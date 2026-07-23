package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

// MetaRow is one custom-metadata entry.
type MetaRow struct {
	UID       string          `json:"uid"`
	Namespace string          `json:"namespace"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	Source    string          `json:"source"`
	UpdatedAt string          `json:"updated_at,omitempty"`
}

// MetaSet upserts a metadata entry, preserving created_at on update. valueJSON
// must be a valid JSON document.
func (s *Store) MetaSet(uid, ns, key, valueJSON, source string, now time.Time) error {
	iso, ms := isoMs(now)
	_, err := s.db.Exec(
		`INSERT INTO event_metadata(uid, namespace, key, value_json, source,
		   created_at, created_at_ms, updated_at, updated_at_ms)
		 VALUES(?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(uid, namespace, key) DO UPDATE SET
		   value_json=excluded.value_json, source=excluded.source,
		   updated_at=excluded.updated_at, updated_at_ms=excluded.updated_at_ms`,
		uid, ns, key, valueJSON, source, iso, ms, iso, ms)
	return err
}

// MetaList returns entries filtered by any non-empty of uid/ns/key.
func (s *Store) MetaList(uid, ns, key string) ([]MetaRow, error) {
	q := `SELECT uid, namespace, key, value_json, source, COALESCE(updated_at,'')
	      FROM event_metadata WHERE 1=1`
	var args []any
	for _, f := range []struct {
		col, val string
	}{{"uid", uid}, {"namespace", ns}, {"key", key}} {
		if f.val != "" {
			q += " AND " + f.col + "=?"
			args = append(args, f.val)
		}
	}
	q += " ORDER BY uid, namespace, key"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetaRow
	for rows.Next() {
		var m MetaRow
		var val string
		if err := rows.Scan(&m.UID, &m.Namespace, &m.Key, &val, &m.Source, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.Value = json.RawMessage(val)
		out = append(out, m)
	}
	return out, rows.Err()
}

// MetaDelete removes one entry, returning how many rows were affected.
func (s *Store) MetaDelete(uid, ns, key string) (int, error) {
	res, err := s.db.Exec(
		`DELETE FROM event_metadata WHERE uid=? AND namespace=? AND key=?`, uid, ns, key)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// SetSyncMeta upserts a key into the internal metadata table. This is sync/
// expand bookkeeping (e.g. the expanded coverage window); it is distinct from
// the agent-owned event_metadata layer that MetaSet writes.
func (s *Store) SetSyncMeta(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO metadata(key, value) VALUES(?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// GetSyncMeta returns an internal metadata value and whether it was present.
func (s *Store) GetSyncMeta(key string) (string, bool, error) {
	row := s.db.QueryRow(`SELECT value FROM metadata WHERE key=?`, key)
	var v string
	switch err := row.Scan(&v); err {
	case nil:
		return v, true, nil
	case sql.ErrNoRows:
		return "", false, nil
	default:
		return "", false, err
	}
}

// EventExists reports whether a live master event with the given uid exists, so
// meta writes can warn about attaching to an unknown event.
func (s *Store) EventExists(uid string) (bool, error) {
	row := s.db.QueryRow(
		`SELECT 1 FROM calendar_events WHERE uid=? AND deleted_at IS NULL LIMIT 1`, uid)
	var one int
	switch err := row.Scan(&one); err {
	case nil:
		return true, nil
	case sql.ErrNoRows:
		return false, nil
	default:
		return false, err
	}
}
