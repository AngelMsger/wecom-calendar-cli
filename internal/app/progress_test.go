package app

import (
	"strings"
	"testing"
	"time"

	syncpkg "github.com/angelmsger/wecom-calendar-cli/internal/sync"
)

func TestProgressPayload(t *testing.T) {
	p := progressPayload(syncpkg.ProgressEvent{
		Phase: syncpkg.ProgressCalendarDone, CalendarsTotal: 11, CalendarsScanned: 3,
		CalendarID: "c1", CalendarName: "Work", Fetched: 412, EventsUpserted: 400,
	}, time.Now())
	if p["phase"] != syncpkg.ProgressCalendarDone || p["calendars_scanned"] != 3 ||
		p["events_upserted"] != 400 || p["calendar_name"] != "Work" {
		t.Fatalf("calendar_done payload wrong: %v", p)
	}
	if _, ok := p["elapsed_s"]; !ok {
		t.Error("elapsed_s should always be present")
	}
}

func TestProgressLine(t *testing.T) {
	start := time.Now()
	if got := progressLine(syncpkg.ProgressEvent{Phase: syncpkg.ProgressStart, CalendarsTotal: 11}, start); !strings.Contains(got, "11 calendars") {
		t.Fatalf("start line missing scale: %q", got)
	}
	done := progressLine(syncpkg.ProgressEvent{
		Phase: syncpkg.ProgressCalendarDone, CalendarsTotal: 11, CalendarsScanned: 3, EventsUpserted: 412,
	}, start)
	if !strings.Contains(done, "3/11 calendars") || !strings.Contains(done, "412 events") {
		t.Fatalf("done line wrong: %q", done)
	}
}
