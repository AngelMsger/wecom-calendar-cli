package store

import (
	"database/sql"
	"strings"
	"time"
)

// SyncStats accumulates counters for a sync run.
type SyncStats struct {
	CalendarsScanned  int
	ResourcesFetched  int
	EventsUpserted    int
	EventsSoftDeleted int
}

// AttendeeInput is one attendee to persist with an event.
type AttendeeInput struct {
	Email, Name, ResponseStatus string
}

// EventInput is a parsed event ready to upsert.
type EventInput struct {
	CalendarID, UID, RecurrenceKey, SourceHref string
	Summary, Description, Location             string
	Start, End                                 time.Time // zero when absent
	AllDay                                     bool
	Status                                     string
	Sequence                                   int
	RRule, RecurrenceRaw, Organizer            string
	LastModified                               time.Time
	RawICS                                     string
	Attendees                                  []AttendeeInput
}

// StoredCalendar is a calendar row for query output.
type StoredCalendar struct {
	ID          string `json:"id"`
	Href        string `json:"href"`
	DisplayName string `json:"display_name"`
	Ctag        string `json:"ctag,omitempty"`
}

// Calendars returns the live (non-deleted) calendars.
func (s *Store) Calendars() ([]StoredCalendar, error) {
	rows, err := s.db.Query(
		`SELECT calendar_id, href, COALESCE(display_name,''), COALESCE(ctag,'')
		 FROM calendars WHERE deleted_at IS NULL ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredCalendar
	for rows.Next() {
		var c StoredCalendar
		if err := rows.Scan(&c.ID, &c.Href, &c.DisplayName, &c.Ctag); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// StoredEvent is an event row for query output.
type StoredEvent struct {
	CalendarID   string `json:"calendar_id"`
	CalendarName string `json:"calendar_name,omitempty"`
	UID          string `json:"uid"`
	Summary      string `json:"summary"`
	Start        string `json:"start,omitempty"`
	End          string `json:"end,omitempty"`
	AllDay       bool   `json:"all_day"`
	Status       string `json:"status,omitempty"`
	Recurring    bool   `json:"recurring"`
}

// AddWarning records a non-fatal sync warning.
func (s *Store) AddWarning(runID int64, category, message string, now time.Time) error {
	iso, ms := isoMs(now)
	_, err := s.db.Exec(
		`INSERT INTO sync_warnings(sync_run_id, category, message, created_at, created_at_ms)
		 VALUES(?,?,?,?,?)`, runID, category, message, iso, ms)
	return err
}

func (s *Store) BeginSyncRun(mode string, now time.Time) (int64, error) {
	iso, ms := isoMs(now)
	res, err := s.db.Exec(
		`INSERT INTO sync_runs(mode, started_at, started_at_ms, ok) VALUES(?,?,?,0)`,
		mode, iso, ms)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FinishSyncRun(id int64, st SyncStats, now time.Time, runErr error) error {
	iso, ms := isoMs(now)
	ok, errStr := 1, ""
	if runErr != nil {
		ok, errStr = 0, runErr.Error()
	}
	_, err := s.db.Exec(
		`UPDATE sync_runs SET finished_at=?, finished_at_ms=?, calendars_scanned=?,
		 resources_fetched=?, events_upserted=?, events_soft_deleted=?, ok=?, error=?
		 WHERE id=?`,
		iso, ms, st.CalendarsScanned, st.ResourcesFetched, st.EventsUpserted,
		st.EventsSoftDeleted, ok, nullStr(errStr), id)
	return err
}

func (s *Store) UpsertCalendar(id, href, name, ctag string, now time.Time) error {
	iso, ms := isoMs(now)
	_, err := s.db.Exec(
		`INSERT INTO calendars(calendar_id, href, display_name, ctag,
		   first_seen_at, first_seen_at_ms, last_seen_at, last_seen_at_ms)
		 VALUES(?,?,?,?,?,?,?,?)
		 ON CONFLICT(calendar_id) DO UPDATE SET
		   href=excluded.href, display_name=excluded.display_name, ctag=excluded.ctag,
		   last_seen_at=excluded.last_seen_at, last_seen_at_ms=excluded.last_seen_at_ms,
		   deleted_at=NULL, deleted_at_ms=NULL`,
		id, href, name, ctag, iso, ms, iso, ms)
	return err
}

// SoftDeleteCalendarsNotIn soft-deletes calendars absent from keep (the current
// server listing) and returns their ids so the caller can cascade the deletion
// to their resources and events.
func (s *Store) SoftDeleteCalendarsNotIn(keep []string, now time.Time) ([]string, error) {
	sel := `SELECT calendar_id FROM calendars WHERE deleted_at IS NULL`
	var selArgs []any
	if len(keep) > 0 {
		sel += " AND calendar_id NOT IN (" + placeholders(len(keep)) + ")"
		for _, k := range keep {
			selArgs = append(selArgs, k)
		}
	}
	rows, err := s.db.Query(sel, selArgs...)
	if err != nil {
		return nil, err
	}
	var gone []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		gone = append(gone, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	iso, ms := isoMs(now)
	for _, id := range gone {
		if _, err := s.db.Exec(
			`UPDATE calendars SET deleted_at=?, deleted_at_ms=? WHERE calendar_id=?`,
			iso, ms, id); err != nil {
			return nil, err
		}
	}
	return gone, nil
}

// SoftDeleteCalendarContents cascades a calendar's disappearance to its live
// resources and events, and drops its sync state so that if the calendar later
// reappears (even with an unchanged ctag) it is fully re-scanned rather than
// skipped with its rows left tombstoned.
func (s *Store) SoftDeleteCalendarContents(calID string, now time.Time) error {
	iso, ms := isoMs(now)
	if _, err := s.db.Exec(
		`UPDATE calendar_resources SET deleted_at=?, deleted_at_ms=?
		 WHERE calendar_id=? AND deleted_at IS NULL`, iso, ms, calID); err != nil {
		return err
	}
	if _, err := s.db.Exec(
		`UPDATE calendar_events SET deleted_at=?, deleted_at_ms=?
		 WHERE calendar_id=? AND deleted_at IS NULL`, iso, ms, calID); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM caldav_sync_state WHERE calendar_id=?`, calID)
	return err
}

func (s *Store) CalendarState(id string) (ctag string, exists bool, err error) {
	row := s.db.QueryRow(`SELECT ctag FROM caldav_sync_state WHERE calendar_id=?`, id)
	var c sql.NullString
	switch err = row.Scan(&c); err {
	case nil:
		return c.String, true, nil
	case sql.ErrNoRows:
		return "", false, nil
	default:
		return "", false, err
	}
}

func (s *Store) SetCalendarState(id, ctag string, now time.Time) error {
	iso, ms := isoMs(now)
	_, err := s.db.Exec(
		`INSERT INTO caldav_sync_state(calendar_id, ctag, last_full_scan_at, last_full_scan_at_ms)
		 VALUES(?,?,?,?)
		 ON CONFLICT(calendar_id) DO UPDATE SET
		   ctag=excluded.ctag, last_full_scan_at=excluded.last_full_scan_at,
		   last_full_scan_at_ms=excluded.last_full_scan_at_ms`,
		id, ctag, iso, ms)
	return err
}

// ResourceEtags returns href->etag for a calendar's live (non-deleted) resources.
func (s *Store) ResourceEtags(calID string) (map[string]string, error) {
	rows, err := s.db.Query(
		`SELECT href, COALESCE(etag,'') FROM calendar_resources
		 WHERE calendar_id=? AND deleted_at IS NULL`, calID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var h, e string
		if err := rows.Scan(&h, &e); err != nil {
			return nil, err
		}
		out[h] = e
	}
	return out, rows.Err()
}

// ResourceFailures returns href->getetag for a calendar's recorded fetch/parse
// failures, so the partition can skip an unchanged bad resource.
func (s *Store) ResourceFailures(calID string) (map[string]string, error) {
	rows, err := s.db.Query(
		`SELECT href, COALESCE(etag,'') FROM calendar_resource_failures WHERE calendar_id=?`, calID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var h, e string
		if err := rows.Scan(&h, &e); err != nil {
			return nil, err
		}
		out[h] = e
	}
	return out, rows.Err()
}

// RecordResourceFailure remembers a resource the server lists but cannot serve
// (404) or whose body will not parse, keyed by the getetag seen, so a later
// sync recognizes it as unchanged-and-bad and skips it instead of re-fetching.
func (s *Store) RecordResourceFailure(calID, href, etag, reason string, now time.Time) error {
	iso, ms := isoMs(now)
	_, err := s.db.Exec(
		`INSERT INTO calendar_resource_failures(calendar_id, href, etag, reason, failed_at, failed_at_ms)
		 VALUES(?,?,?,?,?,?)
		 ON CONFLICT(calendar_id, href) DO UPDATE SET
		   etag=excluded.etag, reason=excluded.reason,
		   failed_at=excluded.failed_at, failed_at_ms=excluded.failed_at_ms`,
		calID, href, etag, reason, iso, ms)
	return err
}

// ClearResourceFailure drops any failure record for a resource that now fetched
// and parsed cleanly.
func (s *Store) ClearResourceFailure(calID, href string) error {
	_, err := s.db.Exec(
		`DELETE FROM calendar_resource_failures WHERE calendar_id=? AND href=?`, calID, href)
	return err
}

// PruneResourceFailuresNotIn drops failure records for resources the server no
// longer lists, so the table does not grow unbounded.
func (s *Store) PruneResourceFailuresNotIn(calID string, keepHrefs []string) error {
	q := `DELETE FROM calendar_resource_failures WHERE calendar_id=?`
	args := []any{calID}
	if len(keepHrefs) > 0 {
		q += " AND href NOT IN (" + placeholders(len(keepHrefs)) + ")"
		for _, h := range keepHrefs {
			args = append(args, h)
		}
	}
	_, err := s.db.Exec(q, args...)
	return err
}

func (s *Store) TouchResource(calID, href string, now time.Time) error {
	iso, ms := isoMs(now)
	_, err := s.db.Exec(
		`UPDATE calendar_resources SET last_seen_at=?, last_seen_at_ms=?, deleted_at=NULL, deleted_at_ms=NULL
		 WHERE calendar_id=? AND href=?`, iso, ms, calID, href)
	return err
}

func (s *Store) UpsertResourceContent(calID, href, etag, sha string, size int, now time.Time, runID int64) error {
	iso, ms := isoMs(now)
	_, err := s.db.Exec(
		`INSERT INTO calendar_resources(calendar_id, href, etag, content_sha256, byte_size,
		   first_seen_at, first_seen_at_ms, last_seen_at, last_seen_at_ms,
		   last_changed_at, last_changed_at_ms, last_sync_run_id)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(calendar_id, href) DO UPDATE SET
		   etag=excluded.etag, content_sha256=excluded.content_sha256, byte_size=excluded.byte_size,
		   last_seen_at=excluded.last_seen_at, last_seen_at_ms=excluded.last_seen_at_ms,
		   last_changed_at=CASE WHEN calendar_resources.content_sha256 IS excluded.content_sha256
		       THEN calendar_resources.last_changed_at ELSE excluded.last_changed_at END,
		   last_changed_at_ms=CASE WHEN calendar_resources.content_sha256 IS excluded.content_sha256
		       THEN calendar_resources.last_changed_at_ms ELSE excluded.last_changed_at_ms END,
		   last_sync_run_id=excluded.last_sync_run_id, deleted_at=NULL, deleted_at_ms=NULL`,
		calID, href, etag, sha, size, iso, ms, iso, ms, iso, ms, runID)
	return err
}

func (s *Store) UpsertEvent(e EventInput, now time.Time, runID int64) error {
	iso, ms := isoMs(now)
	startISO, startMs := optISOMs(e.Start)
	endISO, endMs := optISOMs(e.End)
	lmISO, lmMs := optISOMs(e.LastModified)
	_, err := s.db.Exec(
		`INSERT INTO calendar_events(calendar_id, uid, recurrence_id_key, source_href,
		   summary, description, location, dtstart_at, dtstart_at_ms, dtend_at, dtend_at_ms,
		   all_day, status, sequence, rrule, recurrence_id_raw, organizer,
		   last_modified_at, last_modified_at_ms, raw_ics,
		   first_seen_at, first_seen_at_ms, last_seen_at, last_seen_at_ms, last_sync_run_id)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(calendar_id, uid, recurrence_id_key) DO UPDATE SET
		   source_href=excluded.source_href, summary=excluded.summary, description=excluded.description,
		   location=excluded.location, dtstart_at=excluded.dtstart_at, dtstart_at_ms=excluded.dtstart_at_ms,
		   dtend_at=excluded.dtend_at, dtend_at_ms=excluded.dtend_at_ms, all_day=excluded.all_day,
		   status=excluded.status, sequence=excluded.sequence, rrule=excluded.rrule,
		   recurrence_id_raw=excluded.recurrence_id_raw, organizer=excluded.organizer,
		   last_modified_at=excluded.last_modified_at, last_modified_at_ms=excluded.last_modified_at_ms,
		   raw_ics=excluded.raw_ics, last_seen_at=excluded.last_seen_at,
		   last_seen_at_ms=excluded.last_seen_at_ms, last_sync_run_id=excluded.last_sync_run_id,
		   deleted_at=NULL, deleted_at_ms=NULL`,
		e.CalendarID, e.UID, e.RecurrenceKey, e.SourceHref,
		e.Summary, e.Description, e.Location, startISO, startMs, endISO, endMs,
		boolInt(e.AllDay), e.Status, e.Sequence, e.RRule, e.RecurrenceRaw, e.Organizer,
		lmISO, lmMs, e.RawICS, iso, ms, iso, ms, runID)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(
		`DELETE FROM event_attendees WHERE calendar_id=? AND uid=? AND recurrence_id_key=?`,
		e.CalendarID, e.UID, e.RecurrenceKey); err != nil {
		return err
	}
	for _, a := range e.Attendees {
		if a.Email == "" {
			continue
		}
		if _, err := s.db.Exec(
			`INSERT OR IGNORE INTO event_attendees(calendar_id, uid, recurrence_id_key, email, name, response_status)
			 VALUES(?,?,?,?,?,?)`,
			e.CalendarID, e.UID, e.RecurrenceKey, a.Email, a.Name, a.ResponseStatus); err != nil {
			return err
		}
	}
	return nil
}

// SoftDeleteHrefEventsNotIn soft-deletes events under one resource whose
// (uid, recurrence_id_key) pair is not in keep — an override removed from the
// .ics, or the whole prior event when a resource is rewritten to a different
// UID. Matching on the composite key (not recurrence_id_key alone) is essential:
// a master and a new master both key on the empty recurrence id, so comparing
// only recurrence keys would leave the old UID's master live as a ghost.
func (s *Store) SoftDeleteHrefEventsNotIn(calID, href string, keep [][2]string, now time.Time) (int, error) {
	iso, ms := isoMs(now)
	q := `UPDATE calendar_events SET deleted_at=?, deleted_at_ms=?
	      WHERE calendar_id=? AND source_href=? AND deleted_at IS NULL`
	args := []any{iso, ms, calID, href}
	if len(keep) > 0 {
		q += " AND (uid || char(31) || recurrence_id_key) NOT IN (" + placeholders(len(keep)) + ")"
		for _, k := range keep {
			args = append(args, k[0]+"\x1f"+k[1])
		}
	}
	res, err := s.db.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// SoftDeleteResourcesNotSeen soft-deletes (and returns) resources not touched
// since runStartMs — i.e. absent from this run's full listing.
func (s *Store) SoftDeleteResourcesNotSeen(calID string, runStartMs int64, now time.Time) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT href FROM calendar_resources
		 WHERE calendar_id=? AND deleted_at IS NULL
		   AND (last_seen_at_ms IS NULL OR last_seen_at_ms < ?)`, calID, runStartMs)
	if err != nil {
		return nil, err
	}
	var gone []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			rows.Close()
			return nil, err
		}
		gone = append(gone, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	iso, ms := isoMs(now)
	for _, h := range gone {
		if _, err := s.db.Exec(
			`UPDATE calendar_resources SET deleted_at=?, deleted_at_ms=? WHERE calendar_id=? AND href=?`,
			iso, ms, calID, h); err != nil {
			return nil, err
		}
	}
	return gone, nil
}

// SoftDeleteEventsByHref soft-deletes every live event of a resource.
func (s *Store) SoftDeleteEventsByHref(calID, href string, now time.Time) (int, error) {
	iso, ms := isoMs(now)
	res, err := s.db.Exec(
		`UPDATE calendar_events SET deleted_at=?, deleted_at_ms=?
		 WHERE calendar_id=? AND source_href=? AND deleted_at IS NULL`, iso, ms, calID, href)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// LastSuccessfulSyncMs returns the finish time (UTC ms) of the most recent
// successful sync, if any.
func (s *Store) LastSuccessfulSyncMs() (int64, bool, error) {
	row := s.db.QueryRow(`SELECT finished_at_ms FROM sync_runs WHERE ok=1 AND finished_at_ms IS NOT NULL ORDER BY finished_at_ms DESC LIMIT 1`)
	var ms sql.NullInt64
	switch err := row.Scan(&ms); err {
	case nil:
		return ms.Int64, ms.Valid, nil
	case sql.ErrNoRows:
		return 0, false, nil
	default:
		return 0, false, err
	}
}

// QueryEvents returns live master events whose start falls in [sinceMs, untilMs).
// Recurring masters are returned as-is; per-occurrence expansion is the job of
// event_instances (a later phase).
func (s *Store) QueryEvents(sinceMs, untilMs int64, calID string) ([]StoredEvent, error) {
	q := `SELECT e.calendar_id, COALESCE(c.display_name,''), e.uid, COALESCE(e.summary,''),
	         COALESCE(e.dtstart_at,''), COALESCE(e.dtend_at,''), e.all_day,
	         COALESCE(e.status,''), CASE WHEN COALESCE(e.rrule,'')<>'' THEN 1 ELSE 0 END
	      FROM calendar_events e
	      LEFT JOIN calendars c ON c.calendar_id=e.calendar_id
	      WHERE e.deleted_at IS NULL AND e.recurrence_id_key=''
	        AND e.dtstart_at_ms >= ? AND e.dtstart_at_ms < ?`
	args := []any{sinceMs, untilMs}
	if calID != "" {
		q += " AND e.calendar_id=?"
		args = append(args, calID)
	}
	q += " ORDER BY e.dtstart_at_ms"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredEvent
	for rows.Next() {
		var ev StoredEvent
		var allDay, recurring int
		if err := rows.Scan(&ev.CalendarID, &ev.CalendarName, &ev.UID, &ev.Summary,
			&ev.Start, &ev.End, &allDay, &ev.Status, &recurring); err != nil {
			return nil, err
		}
		ev.AllDay = allDay != 0
		ev.Recurring = recurring != 0
		out = append(out, ev)
	}
	return out, rows.Err()
}

func optISOMs(t time.Time) (any, any) {
	if t.IsZero() {
		return nil, nil
	}
	iso, ms := isoMs(t)
	return iso, ms
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
