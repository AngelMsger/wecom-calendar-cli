// Package expand rebuilds the derived event_instances table: it expands
// recurring masters into occurrences (applying EXDATE and RECURRENCE-ID
// overrides) and folds the same logical event appearing under several calendars
// into a single occurrence keyed by (uid, occurrence_key). It reads only the
// stored raw facts and is a pure, repeatable rebuild; it never touches the
// agent-owned event_metadata layer.
package expand

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/ical"
	"github.com/angelmsger/wecom-calendar-cli/internal/store"
	"github.com/teambition/rrule-go"
)

// MaxInstancesPerEvent caps expansion of a single unbounded rule so a stray
// FREQ=DAILY without UNTIL cannot explode the table. Hitting it is reported in
// Result.Truncated rather than applied silently.
const MaxInstancesPerEvent = 2000

// Options configures a rebuild.
type Options struct {
	Since            time.Time
	Until            time.Time
	Loc              *time.Location
	CalendarPriority []string // preferred primary calendars, most-preferred first
}

// Result summarizes a rebuild. Truncated names the events whose expansion hit
// MaxInstancesPerEvent, so the cap is reported rather than silently applied: a
// caller can surface "this series is cut short in the window you asked for"
// instead of letting the occurrences simply stop mid-window.
type Result struct {
	Instances int
	Truncated []string // uids capped at MaxInstancesPerEvent
}

// Rebuild recomputes every event_instances row and returns what it wrote.
// It computes every row first and only then replaces the table atomically, so a
// parse or write failure aborts with the previous instances intact rather than
// leaving `event list` reading an empty or half-built table. A stored master
// that fails to reparse — or whose recurrence rule will not build — is a genuine
// inconsistency (sync only stores parseable resources) and aborts the rebuild
// rather than being silently skipped or degraded to a single occurrence.
func Rebuild(st *store.Store, opts Options) (Result, error) {
	var res Result
	if opts.Loc == nil {
		opts.Loc = time.UTC
	}
	masters, err := st.MastersForExpansion()
	if err != nil {
		return res, err
	}
	byUID := map[string][]store.MasterRow{}
	var order []string
	for _, m := range masters {
		if _, ok := byUID[m.UID]; !ok {
			order = append(order, m.UID)
		}
		byUID[m.UID] = append(byUID[m.UID], m)
	}

	var rows []store.InstanceRow
	for _, uid := range order {
		group := byUID[uid]
		primary := pickPrimary(group, opts.CalendarPriority)
		srcIDs := calendarIDs(group)
		srcJSON, _ := json.Marshal(srcIDs)

		evs, err := ical.Parse(primary.RawICS, opts.Loc)
		if err != nil {
			return res, fmt.Errorf("expand: reparsing stored event %q: %w", uid, err)
		}
		master, overrides := split(evs, uid)
		if master == nil {
			continue // only override components stored for this uid; nothing to expand
		}
		occurrences, truncated, err := expandOccurrences(*master, overrides, opts)
		if err != nil {
			return res, fmt.Errorf("expand: event %q: %w", uid, err)
		}
		if truncated {
			res.Truncated = append(res.Truncated, uid)
		}
		for _, o := range occurrences {
			rows = append(rows, store.InstanceRow{
				UID:               uid,
				OccurrenceKey:     o.key,
				PrimaryCalendarID: primary.CalendarID,
				SourceCalendarIDs: string(srcJSON),
				SourceCount:       len(srcIDs),
				Summary:           o.summary,
				Start:             o.start,
				End:               o.end,
				AllDay:            master.AllDay,
				Status:            o.status,
				LocalDate:         o.start.In(opts.Loc).Format("2006-01-02"),
			})
		}
	}
	// One transaction: clear, insert every row, and record the covered window so
	// queries beyond it can flag partial coverage. Nothing is visible until commit.
	if err := st.ReplaceInstances(rows, opts.Since.UTC().UnixMilli(), opts.Until.UTC().UnixMilli()); err != nil {
		return res, err
	}
	res.Instances = len(rows)
	return res, nil
}

type occurrence struct {
	key        string
	summary    string
	status     string
	start, end time.Time
}

// expandOccurrences returns one occurrence per date the master produces in the
// window, plus whether the per-event cap was hit. A recurrence rule that will
// not build is an error rather than a silent collapse to a single occurrence:
// the resource parsed cleanly at sync time, so a failure here means the stored
// rule is genuinely inconsistent and the caller should see it.
func expandOccurrences(master ical.Event, overrides map[string]ical.Event, opts Options) ([]occurrence, bool, error) {
	duration := time.Duration(0)
	if !master.End.IsZero() && !master.Start.IsZero() {
		duration = master.End.Sub(master.Start)
	}
	// RFC 5545: an all-day (DATE) event with no DTEND/DURATION lasts one day.
	// Without this an all-day event would collapse to a zero-length instant and
	// be missed by an overlap query on any day but its start.
	if master.AllDay && duration <= 0 {
		duration = 24 * time.Hour
	}
	exset := map[string]bool{}
	for _, k := range master.ExDates {
		exset[k] = true
	}

	build := func(nominal time.Time) (occurrence, bool) {
		key := ical.OccurrenceKey(nominal, master.AllDay)
		if exset[key] {
			return occurrence{}, false
		}
		if ov, ok := overrides[key]; ok {
			end := ov.End
			if end.IsZero() {
				end = ov.Start.Add(duration)
			}
			return occurrence{key, orDefault(ov.Summary, master.Summary),
				orDefault(ov.Status, master.Status), ov.Start, end}, true
		}
		return occurrence{key, master.Summary, master.Status, nominal, nominal.Add(duration)}, true
	}

	// Non-recurring: a single occurrence at the master's start.
	if master.RRuleOption == nil {
		if o, ok := build(master.Start); ok {
			return []occurrence{o}, false, nil
		}
		return nil, false, nil
	}

	rr, err := rrule.NewRRule(*master.RRuleOption)
	if err != nil {
		return nil, false, fmt.Errorf("building recurrence rule %q: %w", master.RRule, err)
	}
	var out []occurrence
	truncated := false
	seen := map[string]bool{}
	for i, nominal := range rr.Between(opts.Since, opts.Until, true) {
		if i >= MaxInstancesPerEvent {
			truncated = true
			break
		}
		if o, ok := build(nominal); ok {
			out = append(out, o)
			seen[o.key] = true
		}
	}
	// Overrides moved into the window but not produced by the rule (e.g. RANGE
	// or a shifted occurrence) still count as occurrences.
	for key, ov := range overrides {
		if seen[key] || exset[key] {
			continue
		}
		if !ov.Start.IsZero() && !ov.Start.Before(opts.Since) && ov.Start.Before(opts.Until) {
			end := ov.End
			if end.IsZero() {
				end = ov.Start.Add(duration)
			}
			out = append(out, occurrence{key, orDefault(ov.Summary, master.Summary),
				orDefault(ov.Status, master.Status), ov.Start, end})
		}
	}
	return out, truncated, nil
}

// split returns the master event and the RECURRENCE-ID overrides (keyed by
// recurrence-id key) for one uid within a parsed resource.
func split(evs []ical.Event, uid string) (*ical.Event, map[string]ical.Event) {
	var master *ical.Event
	overrides := map[string]ical.Event{}
	for i := range evs {
		e := evs[i]
		if e.UID != uid {
			continue
		}
		if e.RecurrenceIDKey == "" {
			m := e
			master = &m
		} else {
			overrides[e.RecurrenceIDKey] = e
		}
	}
	return master, overrides
}

// pickPrimary chooses the primary calendar deterministically: configured
// priority first, then most-recently-modified / highest sequence, then the
// lexicographically smallest calendar id.
func pickPrimary(group []store.MasterRow, priority []string) store.MasterRow {
	rank := map[string]int{}
	for i, id := range priority {
		rank[id] = len(priority) - i // higher = preferred
	}
	best := group[0]
	for _, m := range group[1:] {
		if better(m, best, rank) {
			best = m
		}
	}
	return best
}

func better(a, b store.MasterRow, rank map[string]int) bool {
	if rank[a.CalendarID] != rank[b.CalendarID] {
		return rank[a.CalendarID] > rank[b.CalendarID]
	}
	if a.LastModifiedMs != b.LastModifiedMs {
		return a.LastModifiedMs > b.LastModifiedMs
	}
	if a.Sequence != b.Sequence {
		return a.Sequence > b.Sequence
	}
	return a.CalendarID < b.CalendarID
}

func calendarIDs(group []store.MasterRow) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range group {
		if !seen[m.CalendarID] {
			seen[m.CalendarID] = true
			out = append(out, m.CalendarID)
		}
	}
	return out
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
