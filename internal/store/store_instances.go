package store

import "time"

// MasterRow is a live master event needed to (re)build instances.
type MasterRow struct {
	UID            string
	CalendarID     string
	RawICS         string
	Sequence       int
	LastModifiedMs int64
}

// MastersForExpansion returns every live master event (recurrence_id_key=”)
// with the data needed to expand it. A uid may appear under several calendars.
// Events under a soft-deleted calendar are excluded so a calendar that vanished
// from the server never contributes occurrences to the derived view.
func (s *Store) MastersForExpansion() ([]MasterRow, error) {
	rows, err := s.db.Query(
		`SELECT e.uid, e.calendar_id, e.raw_ics, COALESCE(e.sequence,0), COALESCE(e.last_modified_at_ms,0)
		 FROM calendar_events e
		 JOIN calendars c ON c.calendar_id = e.calendar_id AND c.deleted_at IS NULL
		 WHERE e.deleted_at IS NULL AND e.recurrence_id_key='' AND e.raw_ics<>''
		 ORDER BY e.uid, e.calendar_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MasterRow
	for rows.Next() {
		var m MasterRow
		if err := rows.Scan(&m.UID, &m.CalendarID, &m.RawICS, &m.Sequence, &m.LastModifiedMs); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// InstanceRow is one expanded occurrence to persist.
type InstanceRow struct {
	UID               string
	OccurrenceKey     string
	PrimaryCalendarID string
	SourceCalendarIDs string // JSON array
	SourceCount       int
	Summary           string
	Start             time.Time
	End               time.Time
	AllDay            bool
	Status            string
	LocalDate         string
}

// ClearInstances empties the derived instances table for a full rebuild.
func (s *Store) ClearInstances() error {
	_, err := s.db.Exec(`DELETE FROM event_instances`)
	return err
}

// InsertInstance writes one occurrence.
func (s *Store) InsertInstance(r InstanceRow) error {
	startISO, startMs := optISOMs(r.Start)
	endISO, endMs := optISOMs(r.End)
	_, err := s.db.Exec(
		`INSERT INTO event_instances(uid, occurrence_key, primary_calendar_id, source_calendar_ids,
		   source_count, summary, start_at, start_at_ms, end_at, end_at_ms, all_day, status, local_date)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(uid, occurrence_key) DO UPDATE SET
		   primary_calendar_id=excluded.primary_calendar_id, source_calendar_ids=excluded.source_calendar_ids,
		   source_count=excluded.source_count, summary=excluded.summary,
		   start_at=excluded.start_at, start_at_ms=excluded.start_at_ms,
		   end_at=excluded.end_at, end_at_ms=excluded.end_at_ms, all_day=excluded.all_day,
		   status=excluded.status, local_date=excluded.local_date`,
		r.UID, r.OccurrenceKey, r.PrimaryCalendarID, r.SourceCalendarIDs, r.SourceCount,
		r.Summary, startISO, startMs, endISO, endMs, boolInt(r.AllDay), r.Status, r.LocalDate)
	return err
}

// QueryInstances returns expanded occurrences that overlap the half-open window
// [sinceMs, untilMs) — every occurrence whose interval [start, effective_end)
// intersects the window, not only those that start inside it, so a meeting that
// began before the window but is still running is included. effective_end is the
// stored end, or an instant just after start for a zero-duration occurrence.
// Results are returned in a stable total order and paginated by offset/limit;
// when limit > 0, hasMore reports whether rows remain past the page.
func (s *Store) QueryInstances(sinceMs, untilMs int64, calID string, offset, limit int) (out []InstanceOut, hasMore bool, err error) {
	q := `SELECT uid, occurrence_key, primary_calendar_id, COALESCE(source_calendar_ids,'[]'),
	         source_count, COALESCE(summary,''), COALESCE(start_at,''), COALESCE(end_at,''),
	         all_day, COALESCE(status,''), COALESCE(local_date,'')
	      FROM event_instances
	      WHERE start_at_ms < ?
	        AND (CASE WHEN end_at_ms IS NULL OR end_at_ms <= start_at_ms
	                  THEN start_at_ms + 1 ELSE end_at_ms END) > ?`
	args := []any{untilMs, sinceMs}
	if calID != "" {
		q += " AND (primary_calendar_id=? OR source_calendar_ids LIKE ?)"
		args = append(args, calID, "%\""+calID+"\"%")
	}
	q += " ORDER BY start_at_ms, uid, occurrence_key"
	if limit > 0 {
		// Fetch one extra row to detect a following page without a second query.
		q += " LIMIT ? OFFSET ?"
		args = append(args, limit+1, offset)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var i InstanceOut
		var allDay int
		if err := rows.Scan(&i.UID, &i.OccurrenceKey, &i.PrimaryCalendarID, &i.SourceCalendarIDs,
			&i.SourceCount, &i.Summary, &i.Start, &i.End, &allDay, &i.Status, &i.LocalDate); err != nil {
			return nil, false, err
		}
		i.AllDay = allDay != 0
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
		hasMore = true
	}
	return out, hasMore, nil
}

// InstanceOut is an occurrence for query output.
type InstanceOut struct {
	UID               string `json:"uid"`
	OccurrenceKey     string `json:"occurrence_key"`
	PrimaryCalendarID string `json:"primary_calendar_id"`
	SourceCalendarIDs string `json:"source_calendar_ids"`
	SourceCount       int    `json:"source_count"`
	Summary           string `json:"summary"`
	Start             string `json:"start,omitempty"`
	End               string `json:"end,omitempty"`
	AllDay            bool   `json:"all_day"`
	Status            string `json:"status,omitempty"`
	LocalDate         string `json:"local_date,omitempty"`
}
