// Package store is the local SQLite persistence layer: a faithful raw-fact
// mirror of the CalDAV calendars/resources/events, a sync audit trail, derived
// instances, and a separate agent-owned metadata layer. It is the documented
// domain difference from the stateless sibling CLIs.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite store at path, applies pragmas,
// ensures the schema, and returns a handle.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: serialize writers; WAL still allows readers
	for _, p := range []string{
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=30000",
		"PRAGMA journal_mode=WAL",
	} {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	for _, stmt := range splitSQL(schemaSQL) {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("schema: %w", err)
		}
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// splitSQL breaks a schema script into individual statements. Comments are
// stripped first (line-based; the schema has no "--" inside string literals) so
// a semicolon inside a comment can never split a statement, then the
// comment-free script is split on ";".
func splitSQL(script string) []string {
	var clean strings.Builder
	for _, line := range strings.Split(script, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		clean.WriteString(line)
		clean.WriteString("\n")
	}
	var out []string
	for _, part := range strings.Split(clean.String(), ";") {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}

func isoMs(t time.Time) (string, int64) {
	u := t.UTC()
	return u.Format("2006-01-02T15:04:05.000Z"), u.UnixMilli()
}
