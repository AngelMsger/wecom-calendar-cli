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
func (s *Store) MastersForExpansion() ([]MasterRow, error) {
	rows, err := s.db.Query(
		`SELECT uid, calendar_id, raw_ics, COALESCE(sequence,0), COALESCE(last_modified_at_ms,0)
		 FROM calendar_events
		 WHERE deleted_at IS NULL AND recurrence_id_key='' AND raw_ics<>''
		 ORDER BY uid, calendar_id`)
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

// QueryInstances returns expanded occurrences overlapping [sinceMs, untilMs),
// optionally restricted to a calendar (matched against primary/source).
func (s *Store) QueryInstances(sinceMs, untilMs int64, calID string) ([]InstanceOut, error) {
	q := `SELECT uid, occurrence_key, primary_calendar_id, COALESCE(source_calendar_ids,'[]'),
	         source_count, COALESCE(summary,''), COALESCE(start_at,''), COALESCE(end_at,''),
	         all_day, COALESCE(status,''), COALESCE(local_date,'')
	      FROM event_instances
	      WHERE start_at_ms >= ? AND start_at_ms < ?`
	args := []any{sinceMs, untilMs}
	if calID != "" {
		q += " AND (primary_calendar_id=? OR source_calendar_ids LIKE ?)"
		args = append(args, calID, "%\""+calID+"\"%")
	}
	q += " ORDER BY start_at_ms"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InstanceOut
	for rows.Next() {
		var i InstanceOut
		var allDay int
		if err := rows.Scan(&i.UID, &i.OccurrenceKey, &i.PrimaryCalendarID, &i.SourceCalendarIDs,
			&i.SourceCount, &i.Summary, &i.Start, &i.End, &allDay, &i.Status, &i.LocalDate); err != nil {
			return nil, err
		}
		i.AllDay = allDay != 0
		out = append(out, i)
	}
	return out, rows.Err()
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
