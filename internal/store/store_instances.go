package store

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Metadata keys recording the window the derived instances currently cover, so
// a query outside it can warn instead of silently returning nothing. The store
// owns them because it owns both the instances table and the metadata table.
const (
	MetaCoveredStartMs = "expand_covered_start_ms"
	MetaCoveredEndMs   = "expand_covered_end_ms"
)

// Metadata keys recording an explicitly pinned expansion window. `expand` with
// --since/--until sets them; `sync` honors them so a widened (or narrowed)
// window survives the automatic rebuild at the end of every sync instead of
// silently reverting to the rolling default. `expand` with no flags clears them.
const (
	MetaPinnedStartMs = "expand_pinned_start_ms"
	MetaPinnedEndMs   = "expand_pinned_end_ms"
)

// escapeLike escapes the LIKE metacharacters in a literal so it matches
// verbatim under an `ESCAPE '\'` clause.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

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
	SourceCalendarIDs string // JSON array text as stored
	SourceCount       int
	Summary           string
	Start             time.Time
	End               time.Time
	AllDay            bool
	Status            string
	LocalDate         string
}

const insertInstanceSQL = `INSERT INTO event_instances(uid, occurrence_key, primary_calendar_id, source_calendar_ids,
	   source_count, summary, start_at, start_at_ms, end_at, end_at_ms, all_day, status, local_date)
	 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
	 ON CONFLICT(uid, occurrence_key) DO UPDATE SET
	   primary_calendar_id=excluded.primary_calendar_id, source_calendar_ids=excluded.source_calendar_ids,
	   source_count=excluded.source_count, summary=excluded.summary,
	   start_at=excluded.start_at, start_at_ms=excluded.start_at_ms,
	   end_at=excluded.end_at, end_at_ms=excluded.end_at_ms, all_day=excluded.all_day,
	   status=excluded.status, local_date=excluded.local_date`

func instanceArgs(r InstanceRow) []any {
	startISO, startMs := optISOMs(r.Start)
	endISO, endMs := optISOMs(r.End)
	return []any{r.UID, r.OccurrenceKey, r.PrimaryCalendarID, r.SourceCalendarIDs, r.SourceCount,
		r.Summary, startISO, startMs, endISO, endMs, boolInt(r.AllDay), r.Status, r.LocalDate}
}

// ClearInstances empties the derived instances table.
func (s *Store) ClearInstances() error {
	_, err := s.db.Exec(`DELETE FROM event_instances`)
	return err
}

// InsertInstance writes one occurrence.
func (s *Store) InsertInstance(r InstanceRow) error {
	_, err := s.db.Exec(insertInstanceSQL, instanceArgs(r)...)
	return err
}

// ReplaceInstances atomically replaces the whole event_instances table with rows
// and records the covered window, all in one transaction. Because `event list`
// reads only this table, a non-atomic rebuild (clear, then insert) would make
// every event momentarily vanish on any mid-rebuild failure; here a failure
// rolls back and leaves the previous instances intact.
func (s *Store) ReplaceInstances(rows []InstanceRow, coveredStartMs, coveredEndMs int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM event_instances`); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := tx.Exec(insertInstanceSQL, instanceArgs(r)...); err != nil {
			return err
		}
	}
	setMeta := `INSERT INTO metadata(key, value) VALUES(?,?)
	            ON CONFLICT(key) DO UPDATE SET value=excluded.value`
	if _, err := tx.Exec(setMeta, MetaCoveredStartMs, strconv.FormatInt(coveredStartMs, 10)); err != nil {
		return err
	}
	if _, err := tx.Exec(setMeta, MetaCoveredEndMs, strconv.FormatInt(coveredEndMs, 10)); err != nil {
		return err
	}
	return tx.Commit()
}

// InstanceCursor is the keyset position after the last returned row, on the
// stable (start_at_ms, uid, occurrence_key) ordering.
type InstanceCursor struct {
	StartMs int64
	UID     string
	Key     string
}

// QueryInstances returns expanded occurrences that overlap the half-open window
// [sinceMs, untilMs) — every occurrence whose interval [start, effective_end)
// intersects the window, not only those that start inside it, so a meeting that
// began before the window but is still running is included. effective_end is the
// stored end, or an instant just after start for a zero-duration occurrence.
//
// Paging is keyset, not offset: pass the previous page's returned cursor as
// `after` to continue from exactly where the last page ended on the stable
// (start_at_ms, uid, occurrence_key) order — stable even if rows are inserted or
// deleted between calls. When limit > 0 and more rows remain, the returned
// cursor is non-nil. statuses, when non-empty, restricts to those event statuses
// (compared case-insensitively) — e.g. drop CANCELLED occurrences.
func (s *Store) QueryInstances(sinceMs, untilMs int64, calID string, statuses []string, after *InstanceCursor, limit int) (out []InstanceOut, next *InstanceCursor, err error) {
	q := `SELECT uid, occurrence_key, primary_calendar_id, COALESCE(source_calendar_ids,'[]'),
	         source_count, COALESCE(summary,''), COALESCE(start_at,''), COALESCE(end_at,''),
	         all_day, COALESCE(status,''), COALESCE(local_date,''), start_at_ms
	      FROM event_instances
	      WHERE start_at_ms < ?
	        AND (CASE WHEN end_at_ms IS NULL OR end_at_ms <= start_at_ms
	                  THEN start_at_ms + 1 ELSE end_at_ms END) > ?`
	args := []any{untilMs, sinceMs}
	if calID != "" {
		// source_calendar_ids is a JSON array of quoted ids, so membership is a
		// substring test on the quoted form. The id is escaped and an ESCAPE
		// clause declared, so a `%` or `_` inside an id matches literally instead
		// of acting as a LIKE wildcard.
		q += ` AND (primary_calendar_id=? OR source_calendar_ids LIKE ? ESCAPE '\')`
		args = append(args, calID, `%"`+escapeLike(calID)+`"%`)
	}
	if len(statuses) > 0 {
		q += " AND UPPER(COALESCE(status,'')) IN (" + placeholders(len(statuses)) + ")"
		for _, st := range statuses {
			args = append(args, strings.ToUpper(strings.TrimSpace(st)))
		}
	}
	if after != nil {
		q += ` AND (start_at_ms > ? OR (start_at_ms = ? AND (uid > ? OR (uid = ? AND occurrence_key > ?))))`
		args = append(args, after.StartMs, after.StartMs, after.UID, after.UID, after.Key)
	}
	q += " ORDER BY start_at_ms, uid, occurrence_key"
	if limit > 0 {
		// Fetch one extra row to detect a following page without a second query.
		q += " LIMIT ?"
		args = append(args, limit+1)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	type scanned struct {
		out     InstanceOut
		startMs int64
	}
	var all []scanned
	for rows.Next() {
		var i InstanceOut
		var allDay int
		var srcJSON string
		var startMs int64
		if err := rows.Scan(&i.UID, &i.OccurrenceKey, &i.PrimaryCalendarID, &srcJSON,
			&i.SourceCount, &i.Summary, &i.Start, &i.End, &allDay, &i.Status, &i.LocalDate, &startMs); err != nil {
			return nil, nil, err
		}
		i.AllDay = allDay != 0
		if err := json.Unmarshal([]byte(srcJSON), &i.SourceCalendarIDs); err != nil || i.SourceCalendarIDs == nil {
			i.SourceCalendarIDs = []string{}
		}
		all = append(all, scanned{i, startMs})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if limit > 0 && len(all) > limit {
		last := all[limit-1]
		next = &InstanceCursor{StartMs: last.startMs, UID: last.out.UID, Key: last.out.OccurrenceKey}
		all = all[:limit]
	}
	out = make([]InstanceOut, len(all))
	for i := range all {
		out[i] = all[i].out
	}
	return out, next, nil
}

// InstanceOut is an occurrence for query output. Metadata is attached by the
// command layer when --include-meta is set.
type InstanceOut struct {
	UID               string    `json:"uid"`
	OccurrenceKey     string    `json:"occurrence_key"`
	PrimaryCalendarID string    `json:"primary_calendar_id"`
	SourceCalendarIDs []string  `json:"source_calendar_ids"`
	SourceCount       int       `json:"source_count"`
	Summary           string    `json:"summary"`
	Start             string    `json:"start,omitempty"`
	End               string    `json:"end,omitempty"`
	AllDay            bool      `json:"all_day"`
	Status            string    `json:"status,omitempty"`
	LocalDate         string    `json:"local_date,omitempty"`
	Metadata          []MetaRow `json:"metadata,omitempty"`
}
