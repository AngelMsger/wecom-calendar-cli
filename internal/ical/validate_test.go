package ical

import (
	"strings"
	"testing"
	"time"
)

func wrap(vevent string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
		"BEGIN:VEVENT\r\n" + vevent + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

// TestParseRejectsSemanticErrors guards the incremental-sync data-loss fix: a
// VEVENT that decodes but is semantically broken (no UID, unparseable DTSTART,
// unparseable RECURRENCE-ID) must surface an error so sync treats the resource
// as unparseable instead of committing a degenerate event.
func TestParseRejectsSemanticErrors(t *testing.T) {
	cases := map[string]string{
		"missing UID":         "DTSTART:20260101T100000Z\r\nSUMMARY:x",
		"unparseable DTSTART": "UID:u1\r\nDTSTART:not-a-real-date",
		"unparseable RECURRENCE-ID": "UID:u1\r\nDTSTART:20260101T100000Z\r\n" +
			"RECURRENCE-ID:garbagevalue",
	}
	for name, vevent := range cases {
		if _, err := Parse(wrap(vevent), time.UTC); err == nil {
			t.Errorf("%s: expected a parse error, got nil", name)
		}
	}
}

// TestParseAcceptsValidEvent is the paired positive case so the validation is
// not simply rejecting everything.
func TestParseAcceptsValidEvent(t *testing.T) {
	evs, err := Parse(wrap("UID:u1\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nSUMMARY:ok"), time.UTC)
	if err != nil {
		t.Fatalf("valid event should parse: %v", err)
	}
	if len(evs) != 1 || evs[0].UID != "u1" {
		t.Fatalf("unexpected parse result: %+v", evs)
	}
	if !strings.EqualFold(evs[0].Summary, "ok") {
		t.Fatalf("summary not parsed: %q", evs[0].Summary)
	}
}
