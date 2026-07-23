// Package sync orchestrates a CalDAV -> local store pull. It is incremental
// (per-calendar ctag skip), idempotent (upsert by business key), and safe
// (soft-delete only after a full per-calendar re-listing). It never touches the
// agent-owned event_metadata layer.
package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	stdsync "sync"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/ical"
	"github.com/angelmsger/wecom-calendar-cli/internal/store"
	"github.com/angelmsger/wecom-calendar-cli/pkg/caldav"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// fetchWorkers bounds concurrent event GETs. This server serves one .ics per
// request (no multiget), so a full history means many small requests; a modest
// pool keeps sync brisk without hammering the backend.
const fetchWorkers = 8

// Options configures a sync run.
type Options struct {
	Full       bool           // ignore ctag; rescan every calendar
	CalendarID string         // restrict to one calendar id (empty = all)
	Since      time.Time      // full-listing window start
	Until      time.Time      // full-listing window end
	Loc        *time.Location // display timezone for parsing
}

// Result summarizes a sync run.
type Result struct {
	Mode              string `json:"mode"`
	CalendarsTotal    int    `json:"calendars_total"`
	CalendarsScanned  int    `json:"calendars_scanned"`
	ResourcesFetched  int    `json:"resources_fetched"`
	EventsUpserted    int    `json:"events_upserted"`
	EventsSoftDeleted int    `json:"events_soft_deleted"`
}

// Run performs the sync and returns a summary.
func Run(ctx context.Context, client caldav.Client, st *store.Store, opts Options) (Result, error) {
	now := time.Now()
	_, runStartMs := utcMs(now)
	mode := "incremental"
	if opts.Full {
		mode = "full"
	}
	res := Result{Mode: mode}

	runID, err := st.BeginSyncRun(mode, now)
	if err != nil {
		return res, err
	}
	// finalize records the run outcome regardless of how we return.
	finalize := func(stats store.SyncStats, runErr error) (Result, error) {
		res.CalendarsScanned = stats.CalendarsScanned
		res.ResourcesFetched = stats.ResourcesFetched
		res.EventsUpserted = stats.EventsUpserted
		res.EventsSoftDeleted = stats.EventsSoftDeleted
		_ = st.FinishSyncRun(runID, stats, time.Now(), runErr)
		return res, runErr
	}

	cals, err := client.ListCalendars(ctx)
	if err != nil {
		return finalize(store.SyncStats{}, err)
	}
	res.CalendarsTotal = len(cals)
	keep := make([]string, 0, len(cals))
	for _, c := range cals {
		keep = append(keep, c.ID)
	}
	gone, err := st.SoftDeleteCalendarsNotIn(keep, now)
	if err != nil {
		return finalize(store.SyncStats{}, err)
	}
	// Cascade each vanished calendar's deletion to its resources and events, so
	// a removed calendar's occurrences stop appearing in queries.
	for _, id := range gone {
		if err := st.SoftDeleteCalendarContents(id, now); err != nil {
			return finalize(store.SyncStats{}, err)
		}
	}

	var stats store.SyncStats
	for _, cal := range cals {
		if opts.CalendarID != "" && cal.ID != opts.CalendarID {
			continue
		}
		if err := st.UpsertCalendar(cal.ID, cal.Href, cal.DisplayName, cal.Ctag, now); err != nil {
			return finalize(stats, err)
		}
		if !opts.Full {
			if stored, ok, err := st.CalendarState(cal.ID); err != nil {
				return finalize(stats, err)
			} else if ok && stored != "" && stored == cal.Ctag {
				continue // ctag unchanged: nothing to do for this calendar
			}
		}
		if err := scanCalendar(ctx, client, st, cal, opts, now, runStartMs, runID, &stats); err != nil {
			return finalize(stats, err)
		}
	}
	return finalize(stats, nil)
}

func scanCalendar(ctx context.Context, client caldav.Client, st *store.Store, cal caldav.Calendar,
	opts Options, now time.Time, runStartMs int64, runID int64, stats *store.SyncStats) error {
	stats.CalendarsScanned++

	refs, err := client.ListEvents(ctx, cal.Href, opts.Since, opts.Until)
	if err != nil {
		return err
	}
	stored, err := st.ResourceEtags(cal.ID)
	if err != nil {
		return err
	}

	// Partition: unchanged resources are just touched; the rest are fetched.
	var toFetch []caldav.EventRef
	for _, ref := range refs {
		if e, ok := stored[ref.Href]; ok && ref.Etag != "" && e == ref.Etag {
			if err := st.TouchResource(cal.ID, ref.Href, now); err != nil {
				return err
			}
			continue
		}
		toFetch = append(toFetch, ref)
	}

	// Fetch concurrently (network-bound); collect results for a serial writer.
	type fetched struct {
		ref  caldav.EventRef
		ics  string
		etag string
		err  error
	}
	results := make([]fetched, len(toFetch))
	sem := make(chan struct{}, fetchWorkers)
	var wg stdsync.WaitGroup
	for i, ref := range toFetch {
		wg.Add(1)
		go func(i int, ref caldav.EventRef) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			raw, err := client.GetEvent(ctx, ref.Href)
			etag := raw.Etag
			if etag == "" {
				etag = ref.Etag
			}
			results[i] = fetched{ref: ref, ics: raw.ICS, etag: etag, err: err}
		}(i, ref)
	}
	wg.Wait()

	// Serial write phase (SQLite is single-writer). clean tracks whether every
	// listed resource was fetched AND parsed; if not, we withhold the ctag below
	// so the next sync re-lists and retries instead of skipping the calendar.
	clean := true
	for _, r := range results {
		if r.err != nil {
			// A resource the server listed but cannot serve (a 404 from its
			// inconsistent backend) must not abort the calendar; keep the row and
			// force a re-list next time. Once the server stops listing it, the
			// not-seen sweep below tombstones it.
			if cerrors.AsCLIError(r.err).Category == cerrors.CategoryNotFound {
				_ = st.AddWarning(runID, "resource_unfetchable", r.ref.Href, now)
				_ = st.TouchResource(cal.ID, r.ref.Href, now)
				clean = false
				continue
			}
			return r.err
		}
		// Parse BEFORE recording the resource's etag/content. If parsing fails we
		// deliberately do not advance the etag (so a --full sync re-fetches it)
		// and set clean=false (so an incremental sync re-lists the calendar);
		// otherwise the event would be dropped now and then permanently skipped.
		evs, err := ical.Parse(r.ics, opts.Loc)
		if err != nil {
			_ = st.AddWarning(runID, "resource_unparseable", r.ref.Href, now)
			_ = st.TouchResource(cal.ID, r.ref.Href, now)
			clean = false
			continue
		}
		stats.ResourcesFetched++
		sum := sha256.Sum256([]byte(r.ics))
		if err := st.UpsertResourceContent(cal.ID, r.ref.Href, r.etag, hex.EncodeToString(sum[:]), len(r.ics), now, runID); err != nil {
			return err
		}
		keep := make([][2]string, 0, len(evs))
		for _, ev := range evs {
			keep = append(keep, [2]string{ev.UID, ev.RecurrenceIDKey})
			if err := st.UpsertEvent(toInput(cal.ID, r.ref.Href, ev, r.ics), now, runID); err != nil {
				return err
			}
			stats.EventsUpserted++
		}
		n, err := st.SoftDeleteHrefEventsNotIn(cal.ID, r.ref.Href, keep, now)
		if err != nil {
			return err
		}
		stats.EventsSoftDeleted += n
	}

	goneRes, err := st.SoftDeleteResourcesNotSeen(cal.ID, runStartMs, now)
	if err != nil {
		return err
	}
	for _, h := range goneRes {
		n, err := st.SoftDeleteEventsByHref(cal.ID, h, now)
		if err != nil {
			return err
		}
		stats.EventsSoftDeleted += n
	}

	// Commit the ctag only after every resource for this calendar landed AND
	// parsed, so an interruption or a bad resource re-scans next time rather than
	// being skipped by an advanced change-tag.
	if clean {
		return st.SetCalendarState(cal.ID, cal.Ctag, now)
	}
	return nil
}

// toInput maps a parsed event plus its full resource ICS into a store input.
// The whole VCALENDAR body (with its VTIMEZONE, RRULE and EXDATE) is stored on
// each event row so expansion and re-labeling can reparse it self-contained.
func toInput(calID, href string, ev ical.Event, resourceICS string) store.EventInput {
	in := store.EventInput{
		CalendarID: calID, UID: ev.UID, RecurrenceKey: ev.RecurrenceIDKey, SourceHref: href,
		Summary: ev.Summary, Description: ev.Description, Location: ev.Location,
		Start: ev.Start, End: ev.End, AllDay: ev.AllDay, Status: ev.Status,
		Sequence: ev.Sequence, RRule: ev.RRule, RecurrenceRaw: ev.RecurrenceIDRaw,
		Organizer: ev.Organizer, LastModified: ev.LastModified, RawICS: resourceICS,
	}
	for _, a := range ev.Attendees {
		in.Attendees = append(in.Attendees, store.AttendeeInput{Email: a.Email, Name: a.Name, ResponseStatus: a.ResponseStatus})
	}
	return in
}

func utcMs(t time.Time) (string, int64) {
	u := t.UTC()
	return u.Format(time.RFC3339), u.UnixMilli()
}
