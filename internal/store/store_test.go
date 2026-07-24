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
	n, err := st.SoftDeleteHrefEventsNotIn("c1", "/c1/u1.ics", [][2]string{{"u1", ""}}, now)
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

// TestSoftDeleteHrefEventsNotInUIDChange guards the ghost-event bug: when a
// resource is rewritten to a different UID, both masters key on the empty
// recurrence id, so matching on the composite (uid, recurrence) key — not the
// recurrence key alone — must tombstone the old UID's master.
func TestSoftDeleteHrefEventsNotInUIDChange(t *testing.T) {
	st := openTemp(t)
	now := time.Unix(1_700_000_000, 0)
	run, _ := st.BeginSyncRun("full", now)
	old := EventInput{CalendarID: "c1", UID: "old", RecurrenceKey: "", SourceHref: "/c1/x.ics", RawICS: "x"}
	if err := st.UpsertEvent(old, now, run); err != nil {
		t.Fatal(err)
	}
	// The resource now parses to a different UID at the same href.
	fresh := EventInput{CalendarID: "c1", UID: "new", RecurrenceKey: "", SourceHref: "/c1/x.ics", RawICS: "y"}
	if err := st.UpsertEvent(fresh, now, run); err != nil {
		t.Fatal(err)
	}
	// keep only the freshly parsed (uid,rec) pair; the old master must go.
	n, err := st.SoftDeleteHrefEventsNotIn("c1", "/c1/x.ics", [][2]string{{"new", ""}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 soft-deleted (the old UID), got %d", n)
	}
	var liveUID string
	if err := st.db.QueryRow(
		`SELECT uid FROM calendar_events WHERE deleted_at IS NULL`).Scan(&liveUID); err != nil {
		t.Fatal(err)
	}
	if liveUID != "new" {
		t.Fatalf("want only the new UID live, got %q", liveUID)
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
	rows, err := st.MetaList("u1", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || string(rows[0].Value) != `"g-123"` {
		t.Fatalf("metadata lost or altered by re-sync: %+v", rows)
	}
}
