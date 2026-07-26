package expand

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func seedMaster(t *testing.T, st *store.Store, uid, rawICS string) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0)
	if err := st.UpsertCalendar("c1", "/calendar/c1/", "Cal", "ctag", now); err != nil {
		t.Fatal(err)
	}
	run, _ := st.BeginSyncRun("full", now)
	in := store.EventInput{CalendarID: "c1", UID: uid, RecurrenceKey: "", SourceHref: "/calendar/c1/" + uid + ".ics", RawICS: rawICS}
	if err := st.UpsertEvent(in, now, run); err != nil {
		t.Fatal(err)
	}
}

const recurringICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:u1\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\n" +
	"RRULE:FREQ=WEEKLY;COUNT=3\r\nSUMMARY:Standup\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func opts() Options {
	return Options{
		Since: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		Loc:   time.UTC,
	}
}

// TestRebuildExpandsRecurring is the happy path: a weekly COUNT=3 master yields
// three instances and records the covered window.
func TestRebuildExpandsRecurring(t *testing.T) {
	st := openStore(t)
	seedMaster(t, st, "u1", recurringICS)

	res, err := Rebuild(st, opts())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if res.Instances != 3 {
		t.Fatalf("want 3 occurrences from COUNT=3, got %d", res.Instances)
	}
	if v, ok, _ := st.GetSyncMeta(store.MetaCoveredStartMs); !ok || v == "" {
		t.Fatal("covered window not recorded")
	}
}

// TestRebuildAtomicOnParseError guards atomicity: if a stored master fails to
// reparse, Rebuild aborts and leaves the previous instances intact rather than
// clearing the table first and returning a partial/empty result.
func TestRebuildAtomicOnParseError(t *testing.T) {
	st := openStore(t)
	// A good rebuild first, so the table holds real data.
	seedMaster(t, st, "u1", recurringICS)
	if _, err := Rebuild(st, opts()); err != nil {
		t.Fatal(err)
	}
	before, _, err := st.QueryInstances(0, 1<<62, "", nil, nil, 0)
	if err != nil || len(before) == 0 {
		t.Fatalf("expected seeded instances, got %d (err %v)", len(before), err)
	}

	// Now corrupt the stored master so the next rebuild's reparse fails.
	badICS := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:u1\r\n" +
		"DTSTART:not-a-real-date\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	seedMaster(t, st, "u1", badICS)

	if _, err := Rebuild(st, opts()); err == nil {
		t.Fatal("rebuild should fail on an unparseable stored master")
	}
	after, _, err := st.QueryInstances(0, 1<<62, "", nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("failed rebuild must leave prior instances intact: had %d, now %d", len(before), len(after))
	}
}
