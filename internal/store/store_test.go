package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func countLiveEvents(t *testing.T, st *Store) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow(`SELECT count(*) FROM calendar_events WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUpsertIdempotent(t *testing.T) {
	st := openTemp(t)
	now := time.Unix(1_700_000_000, 0)
	run, _ := st.BeginSyncRun("full", now)
	ev := EventInput{
		CalendarID: "c1", UID: "u1", RecurrenceKey: "", SourceHref: "/c1/u1.ics",
		Summary: "Meeting", Start: now, End: now.Add(time.Hour), RawICS: "BEGIN:VEVENT",
		Attendees: []AttendeeInput{{Email: "a@x.com", Name: "A"}},
	}
	for i := 0; i < 3; i++ {
		if err := st.UpsertEvent(ev, now.Add(time.Duration(i)*time.Minute), run); err != nil {
			t.Fatal(err)
		}
	}
	if n := countLiveEvents(t, st); n != 1 {
		t.Fatalf("want 1 event after 3 upserts, got %d", n)
	}
	// Attendees must not accumulate across upserts.
	var na int
	st.db.QueryRow(`SELECT count(*) FROM event_attendees WHERE uid='u1'`).Scan(&na)
	if na != 1 {
		t.Fatalf("want 1 attendee, got %d", na)
	}
}

func TestSoftDeleteHrefEventsNotIn(t *testing.T) {
	st := openTemp(t)
	now := time.Unix(1_700_000_000, 0)
	run, _ := st.BeginSyncRun("full", now)
	master := EventInput{CalendarID: "c1", UID: "u1", RecurrenceKey: "", SourceHref: "/c1/u1.ics", RawICS: "x"}
	override := EventInput{CalendarID: "c1", UID: "u1", RecurrenceKey: "1780909200000", SourceHref: "/c1/u1.ics", RawICS: "x"}
	for _, e := range []EventInput{master, override} {
		if err := st.UpsertEvent(e, now, run); err != nil {
			t.Fatal(err)
		}
	}
	if n := countLiveEvents(t, st); n != 2 {
		t.Fatalf("want 2, got %d", n)
	}
	// A re-parse that no longer contains the override soft-deletes just it.
	n, err := st.SoftDeleteHrefEventsNotIn("c1", "/c1/u1.ics", []string{""}, now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 soft-deleted, got %d", n)
	}
	if live := countLiveEvents(t, st); live != 1 {
		t.Fatalf("want 1 live after soft-delete, got %d", live)
	}
}

func TestMetadataSurvivesResync(t *testing.T) {
	st := openTemp(t)
	now := time.Unix(1_700_000_000, 0)
	run, _ := st.BeginSyncRun("full", now)
	ev := EventInput{CalendarID: "c1", UID: "u1", SourceHref: "/c1/u1.ics", RawICS: "x"}
	if err := st.UpsertEvent(ev, now, run); err != nil {
		t.Fatal(err)
	}
	if err := st.MetaSet("u1", "task", "feishu", `"g-123"`, "agent", now); err != nil {
		t.Fatal(err)
	}
	// Simulate a later full re-sync: re-upsert the event, even soft-delete/revive.
	if _, err := st.SoftDeleteEventsByHref("c1", "/c1/u1.ics", now); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEvent(ev, now.Add(time.Hour), run); err != nil {
		t.Fatal(err)
	}
	rows, err := st.MetaList("u1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || string(rows[0].Value) != `"g-123"` {
		t.Fatalf("metadata lost or altered by re-sync: %+v", rows)
	}
}
