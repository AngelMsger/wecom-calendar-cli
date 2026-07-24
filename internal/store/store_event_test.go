package store

import (
	"testing"
	"time"
)

// TestEventDetailPrimaryAndAttendees: when a uid lives under several calendars,
// EventDetail returns the deterministically-chosen primary (most recently
// modified) with its rich fields and attendees.
func TestEventDetailPrimaryAndAttendees(t *testing.T) {
	st := openTemp(t)
	now := time.Unix(1_700_000_000, 0)
	if err := st.UpsertCalendar("c1", "/c1/", "Cal1", "t1", now); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertCalendar("c2", "/c2/", "Cal2", "t2", now); err != nil {
		t.Fatal(err)
	}
	run, _ := st.BeginSyncRun("full", now)
	older, newer := now.Add(-time.Hour), now
	mustUpsert(t, st, EventInput{CalendarID: "c1", UID: "u1", Summary: "old copy",
		Description: "d1", RawICS: "x", LastModified: older,
		Attendees: []AttendeeInput{{Email: "a@x.com"}}}, now, run)
	mustUpsert(t, st, EventInput{CalendarID: "c2", UID: "u1", Summary: "new copy",
		Description: "d2", Location: "Room B", Organizer: "boss@x.com", RawICS: "x", LastModified: newer,
		Attendees: []AttendeeInput{{Email: "me@x.com", Name: "Me"}, {Email: "bob@x.com"}}}, now, run)

	d, err := st.EventDetail("u1", "")
	if err != nil || d == nil {
		t.Fatalf("want detail, got %v (err %v)", d, err)
	}
	if d.CalendarID != "c2" {
		t.Fatalf("primary should be the newer copy c2, got %s", d.CalendarID)
	}
	if d.Summary != "new copy" || d.Description != "d2" || d.Location != "Room B" || d.Organizer != "boss@x.com" {
		t.Fatalf("rich fields wrong: %+v", d)
	}
	if len(d.Attendees) != 2 {
		t.Fatalf("want 2 attendees from the primary, got %d", len(d.Attendees))
	}

	if got, _ := st.EventDetail("missing", ""); got != nil {
		t.Fatal("EventDetail for an unknown uid should be nil")
	}
}

// TestEventDetailOccurrenceOverride: --occurrence merges a RECURRENCE-ID
// override's non-empty fields onto the master, leaving unset fields alone.
func TestEventDetailOccurrenceOverride(t *testing.T) {
	st := openTemp(t)
	now := time.Unix(1_700_000_000, 0)
	if err := st.UpsertCalendar("c1", "/c1/", "Cal", "t", now); err != nil {
		t.Fatal(err)
	}
	run, _ := st.BeginSyncRun("full", now)
	mustUpsert(t, st, EventInput{CalendarID: "c1", UID: "u1", Summary: "master", Description: "md", RawICS: "x"}, now, run)
	mustUpsert(t, st, EventInput{CalendarID: "c1", UID: "u1", RecurrenceKey: "K1",
		Summary: "override title", RecurrenceRaw: "20260701T100000Z", RawICS: "x"}, now, run)

	d, err := st.EventDetail("u1", "K1")
	if err != nil || d == nil {
		t.Fatalf("want detail: %v", err)
	}
	if d.Summary != "override title" {
		t.Fatalf("occurrence summary should override master, got %q", d.Summary)
	}
	if d.Description != "md" {
		t.Fatalf("an empty override field must keep the master's, got %q", d.Description)
	}
	if d.RecurrenceID != "20260701T100000Z" {
		t.Fatalf("recurrence_id not surfaced, got %q", d.RecurrenceID)
	}
}

func mustUpsert(t *testing.T, st *Store, e EventInput, now time.Time, run int64) {
	t.Helper()
	if err := st.UpsertEvent(e, now, run); err != nil {
		t.Fatal(err)
	}
}
