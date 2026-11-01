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
	if _, err := st.SoftDeleteHrefEventsNotIn("c1", "/c1/u1.ics", nil, now); err != nil {
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

// TestSaveResourceIsAtomic: the stored etag is what lets an incremental sync
// skip a resource, so a write that fails partway must leave the previous etag
// and events whole. Otherwise the next sync would skip the resource and keep
// half-written events until a --full rescan.
func TestSaveResourceIsAtomic(t *testing.T) {
	st := openTemp(t)
	now := time.Unix(1_700_000_000, 0)
	run, _ := st.BeginSyncRun("incremental", now)
	first := ResourceInput{CalendarID: "c1", Href: "/c1/x.ics", Etag: "e1",
		Events: []EventInput{{UID: "a", Summary: "v1", RawICS: "x"}}}
	if _, err := st.SaveResource(first, now, run); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`CREATE TRIGGER fail_b BEFORE INSERT ON calendar_events
		WHEN NEW.uid = 'b' BEGIN SELECT RAISE(ABORT, 'disk full'); END`); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordResourceFailure("c1", "/c1/x.ics", "e0", "unparseable", now); err != nil {
		t.Fatal(err)
	}
	second := ResourceInput{CalendarID: "c1", Href: "/c1/x.ics", Etag: "e2", Events: []EventInput{
		{UID: "a", Summary: "v2", RawICS: "y"},
		{UID: "b", Summary: "new", RawICS: "y"},
	}}
	if _, err := st.SaveResource(second, now.Add(time.Hour), run); err == nil {
		t.Fatal("want the failed event write to fail the save")
	}
	etags, err := st.ResourceEtags("c1")
	if err != nil {
		t.Fatal(err)
	}
	if etags["/c1/x.ics"] != "e1" {
		t.Fatalf("etag advanced past a failed write: %q", etags["/c1/x.ics"])
	}
	var summary string
	if err := st.db.QueryRow(`SELECT summary FROM calendar_events WHERE uid='a'`).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != "v1" {
		t.Fatalf("want the earlier event untouched, got %q", summary)
	}
	if failed, _ := st.ResourceFailures("c1"); failed["/c1/x.ics"] != "e0" {
		t.Fatalf("want the failure record kept, got %v", failed)
	}

	if _, err := st.db.Exec(`DROP TRIGGER fail_b`); err != nil {
		t.Fatal(err)
	}
	n, err := st.SaveResource(ResourceInput{CalendarID: "c1", Href: "/c1/x.ics", Etag: "e3",
		Events: []EventInput{{UID: "b", RawICS: "z"}}}, now.Add(2*time.Hour), run)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || countLiveEvents(t, st) != 1 {
		t.Fatalf("want the dropped event soft-deleted and one live, got %d deleted, %d live", n, countLiveEvents(t, st))
	}
	if failed, _ := st.ResourceFailures("c1"); len(failed) != 0 {
		t.Fatalf("want the failure record cleared, got %v", failed)
	}
	var calID, href string
	if err := st.db.QueryRow(`SELECT calendar_id, source_href FROM calendar_events WHERE uid='b'`).Scan(&calID, &href); err != nil {
		t.Fatal(err)
	}
	if calID != "c1" || href != "/c1/x.ics" {
		t.Fatalf("want events filed under their resource, got %q %q", calID, href)
	}
}

// liveEvent reports whether the event with uid is live (not tombstoned).
func liveEvent(t *testing.T, st *Store, uid string) bool {
	t.Helper()
	var n int
	if err := st.db.QueryRow(`SELECT count(*) FROM calendar_events WHERE uid=? AND deleted_at IS NULL`, uid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// liveResource reports whether the resource at href is live (not tombstoned).
func liveResource(t *testing.T, st *Store, href string) bool {
	t.Helper()
	var n int
	if err := st.db.QueryRow(`SELECT count(*) FROM calendar_resources WHERE href=? AND deleted_at IS NULL`, href).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func TestRemoveUnlistedResourcesIsAtomic(t *testing.T) {
	st := openTemp(t)
	t0 := time.Unix(1_700_000_000, 0)
	run, _ := st.BeginSyncRun("full", t0)
	for _, r := range []ResourceInput{
		{CalendarID: "c1", Href: "/c1/kept.ics", Etag: "e1", Events: []EventInput{{UID: "kept", RawICS: "x"}}},
		{CalendarID: "c1", Href: "/c1/gone.ics", Etag: "e1", Events: []EventInput{{UID: "gone", RawICS: "x"}}},
		{CalendarID: "c2", Href: "/c2/other.ics", Etag: "e1", Events: []EventInput{{UID: "other", RawICS: "x"}}},
	} {
		if _, err := st.SaveResource(r, t0, run); err != nil {
			t.Fatal(err)
		}
	}
	// The next run lists only kept.ics.
	t1 := t0.Add(time.Hour)
	if _, err := st.SaveResource(ResourceInput{CalendarID: "c1", Href: "/c1/kept.ics", Etag: "e1",
		Events: []EventInput{{UID: "kept", RawICS: "x"}}}, t1, run); err != nil {
		t.Fatal(err)
	}

	// A failure while tombstoning the events must leave the resource live too,
	// so the next sync sweeps it again instead of skipping it forever.
	if _, err := st.db.Exec(`CREATE TRIGGER fail_gone BEFORE UPDATE ON calendar_events
		WHEN OLD.uid = 'gone' BEGIN SELECT RAISE(ABORT, 'disk full'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.RemoveUnlistedResources("c1", t1.UnixMilli(), t1); err == nil {
		t.Fatal("want the failed event update to fail the sweep")
	}
	if !liveResource(t, st, "/c1/gone.ics") || !liveEvent(t, st, "gone") {
		t.Fatal("a failed sweep tombstoned the resource without its events")
	}
	if _, err := st.db.Exec(`DROP TRIGGER fail_gone`); err != nil {
		t.Fatal(err)
	}

	resources, events, err := st.RemoveUnlistedResources("c1", t1.UnixMilli(), t1)
	if err != nil {
		t.Fatal(err)
	}
	if resources != 1 || events != 1 {
		t.Fatalf("want 1 resource and 1 event tombstoned, got %d and %d", resources, events)
	}
	if liveResource(t, st, "/c1/gone.ics") || liveEvent(t, st, "gone") {
		t.Fatal("want the unlisted resource and its event tombstoned")
	}
	if !liveEvent(t, st, "kept") || !liveEvent(t, st, "other") {
		t.Fatal("want listed resources and other calendars untouched")
	}
}

// A sync interrupted between the old two steps left a tombstoned resource with
// live events, and the not-seen sweep skips tombstoned resources. The sweep
// must repair that state instead of leaving deleted events readable as live.
func TestRemoveUnlistedResourcesRepairsLeftoverEvents(t *testing.T) {
	st := openTemp(t)
	now := time.Unix(1_700_000_000, 0)
	run, _ := st.BeginSyncRun("full", now)
	if _, err := st.SaveResource(ResourceInput{CalendarID: "c1", Href: "/c1/gone.ics", Etag: "e1",
		Events: []EventInput{{UID: "gone", RawICS: "x"}}}, now, run); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE calendar_resources SET deleted_at='x', deleted_at_ms=1 WHERE href='/c1/gone.ics'`); err != nil {
		t.Fatal(err)
	}

	later := now.Add(time.Hour)
	resources, events, err := st.RemoveUnlistedResources("c1", later.UnixMilli(), later)
	if err != nil {
		t.Fatal(err)
	}
	if resources != 0 || events != 1 || liveEvent(t, st, "gone") {
		t.Fatalf("want the leftover event tombstoned, got %d resources, %d events, live=%v",
			resources, events, liveEvent(t, st, "gone"))
	}
}
