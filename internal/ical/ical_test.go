package ical

import (
	"testing"
	"time"
)

// A WeCom-shaped resource: a non-IANA TZ08 defined by an embedded VTIMEZONE, a
// weekly recurring master with EXDATE, and attendees.
const wecomICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Tencent//WeCom//EN
BEGIN:VTIMEZONE
TZID:TZ08
BEGIN:STANDARD
DTSTART:19700101T000000
TZOFFSETFROM:+0800
TZOFFSETTO:+0800
TZNAME:CST
END:STANDARD
END:VTIMEZONE
BEGIN:VEVENT
UID:evt-1
SUMMARY:Weekly sync
LOCATION:Room A
DTSTART;TZID=TZ08:20260611T170000
DTEND;TZID=TZ08:20260611T173000
RRULE:FREQ=WEEKLY;UNTIL=20260702T155959Z;BYDAY=TH
EXDATE;TZID=TZ08:20260618T170000
SEQUENCE:3
STATUS:CONFIRMED
ORGANIZER;CN=Vista:mailto:vista@example.com
ATTENDEE;CN=Bob;PARTSTAT=ACCEPTED:mailto:bob@example.com
END:VEVENT
END:VCALENDAR
`

const allDayICS = `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:holiday-1
SUMMARY:Holiday
DTSTART;VALUE=DATE:20260101
DTEND;VALUE=DATE:20260102
END:VEVENT
END:VCALENDAR
`

func TestParseTZ08(t *testing.T) {
	evs, err := Parse(wecomICS, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	e := evs[0]

	// DTSTART 17:00 in TZ08 (UTC+8) must resolve to 09:00 UTC — the embedded
	// VTIMEZONE, not a hard failure from time.LoadLocation("TZ08").
	wantUTC := time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)
	if !e.Start.UTC().Equal(wantUTC) {
		t.Errorf("start = %v, want %v", e.Start.UTC(), wantUTC)
	}
	if e.Summary != "Weekly sync" || e.Location != "Room A" {
		t.Errorf("summary/location = %q/%q", e.Summary, e.Location)
	}
	if e.RRule == "" {
		t.Error("rrule should be preserved")
	}
	if e.Sequence != 3 || e.Status != "CONFIRMED" {
		t.Errorf("sequence/status = %d/%q", e.Sequence, e.Status)
	}
	if e.Organizer != "vista@example.com" {
		t.Errorf("organizer = %q", e.Organizer)
	}
	if len(e.Attendees) != 1 || e.Attendees[0].Email != "bob@example.com" || e.Attendees[0].Name != "Bob" {
		t.Errorf("attendees = %+v", e.Attendees)
	}
	// EXDATE at 17:00 TZ08 (UTC+8) -> its occurrence key is the UTC-ms of 09:00 UTC.
	wantEx := OccurrenceKey(time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC), false)
	if len(e.ExDates) != 1 || e.ExDates[0] != wantEx {
		t.Errorf("exdates = %v, want [%s]", e.ExDates, wantEx)
	}
}

func TestParseAllDay(t *testing.T) {
	evs, err := Parse(allDayICS, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(evs) != 1 || !evs[0].AllDay {
		t.Fatalf("want 1 all-day event, got %+v", evs)
	}
	if got := OccurrenceKey(evs[0].Start, true); got != "2026-01-01" {
		t.Errorf("all-day occurrence key = %q, want 2026-01-01", got)
	}
}

func TestOccurrenceKey(t *testing.T) {
	dt := time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)
	if k := OccurrenceKey(dt, false); k != "1781168400000" {
		t.Errorf("date-time key = %q", k)
	}
	if k := OccurrenceKey(dt, true); k != "2026-06-11" {
		t.Errorf("all-day key = %q", k)
	}
}

func TestParseUTCOffset(t *testing.T) {
	cases := map[string]int{"+0800": 8 * 3600, "-0530": -(5*3600 + 30*60), "+0000": 0}
	for in, want := range cases {
		if got, ok := parseUTCOffset(in); !ok || got != want {
			t.Errorf("parseUTCOffset(%q) = %d,%v want %d", in, got, ok, want)
		}
	}
	if _, ok := parseUTCOffset("bad"); ok {
		t.Error("parseUTCOffset(bad) should fail")
	}
}
