package ical

import (
	"strings"
	"testing"
	"time"
)

func icsWith(props string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:u1\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\n" +
		"RRULE:FREQ=WEEKLY;COUNT=3\r\n" + props + "SUMMARY:Standup\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
}

// TestParseRejectsUnparseableExdate is the regression guard for an exclusion
// being silently dropped. A dropped EXDATE is not a missing detail — it
// resurrects a cancelled occurrence as a live meeting — so it must fail the
// resource the same way an unparseable DTSTART does.
func TestParseRejectsUnparseableExdate(t *testing.T) {
	_, err := Parse(icsWith("EXDATE:not-a-date\r\n"), time.UTC)
	if err == nil {
		t.Fatal("an unparseable EXDATE must fail the parse, not be skipped")
	}
	if !strings.Contains(err.Error(), "EXDATE") {
		t.Fatalf("error should name the offending property, got %v", err)
	}
}

// TestParseRejectsUnparseableExdateInList: the same holds for one bad value in
// a comma-separated EXDATE list, where a silent skip is easiest to miss.
func TestParseRejectsUnparseableExdateInList(t *testing.T) {
	_, err := Parse(icsWith("EXDATE:20260108T100000Z,garbage\r\n"), time.UTC)
	if err == nil {
		t.Fatal("one unparseable value in an EXDATE list must fail the parse")
	}
}

// TestParseAcceptsValidExdate keeps the strict path from over-rejecting.
func TestParseAcceptsValidExdate(t *testing.T) {
	evs, err := Parse(icsWith("EXDATE:20260108T100000Z\r\n"), time.UTC)
	if err != nil {
		t.Fatalf("a valid EXDATE must parse: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	want := OccurrenceKey(time.Date(2026, 1, 8, 10, 0, 0, 0, time.UTC), false)
	if len(evs[0].ExDates) != 1 || evs[0].ExDates[0] != want {
		t.Fatalf("want exdate %q, got %v", want, evs[0].ExDates)
	}
}
