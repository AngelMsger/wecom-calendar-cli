package app

import (
	"fmt"
	"os"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/output"
	syncpkg "github.com/angelmsger/wecom-calendar-cli/internal/sync"
)

// syncProgress builds a sync progress reporter and a finalizer. mode is
// auto|none|json:
//   - none: no progress.
//   - json: bounded structured notices on stderr, one JSON line per event.
//   - auto: a single, self-overwriting status line when stderr is a terminal;
//     otherwise the json behavior, so agents and pipes get bounded milestones
//     rather than either silence or the --verbose per-request firehose.
//
// Either way stdout is never touched, so the command's data contract stays
// byte-stable. Returns a nil callback for mode=none.
func (s *appState) syncProgress(mode string) (progress func(syncpkg.ProgressEvent), finish func()) {
	if mode == "none" {
		return nil, func() {}
	}
	start := time.Now()
	if mode == "json" || !stderrIsTTY() {
		return func(ev syncpkg.ProgressEvent) {
			output.EmitNotice(os.Stderr, map[string]any{
				"_notice": map[string]any{"progress": progressPayload(ev, start)},
			})
		}, func() {}
	}
	// Terminal: rewrite one line in place; clear it and terminate with a newline
	// at the end so the shell prompt (and the stdout result) start clean.
	var wrote bool
	return func(ev syncpkg.ProgressEvent) {
			wrote = true
			fmt.Fprintf(os.Stderr, "\r\033[K%s", progressLine(ev, start))
		}, func() {
			if wrote {
				fmt.Fprintln(os.Stderr)
			}
		}
}

func progressPayload(ev syncpkg.ProgressEvent, start time.Time) map[string]any {
	m := map[string]any{
		"phase":           ev.Phase,
		"calendars_total": ev.CalendarsTotal,
		"elapsed_s":       int(time.Since(start).Seconds()),
	}
	switch ev.Phase {
	case syncpkg.ProgressCalendarDone:
		m["calendars_scanned"] = ev.CalendarsScanned
		m["calendar"] = ev.CalendarID
		m["resources_fetched"] = ev.Fetched
		m["events_upserted"] = ev.EventsUpserted
	case syncpkg.ProgressFetching:
		m["calendar"] = ev.CalendarID
		m["fetched"] = ev.Fetched
		m["fetch_total"] = ev.Total
	}
	if ev.CalendarName != "" {
		m["calendar_name"] = ev.CalendarName
	}
	return m
}

func progressLine(ev syncpkg.ProgressEvent, start time.Time) string {
	el := int(time.Since(start).Seconds())
	name := ev.CalendarName
	if name == "" {
		name = ev.CalendarID
	}
	switch ev.Phase {
	case syncpkg.ProgressStart:
		return fmt.Sprintf("syncing %d calendars…", ev.CalendarsTotal)
	case syncpkg.ProgressFetching:
		return fmt.Sprintf("fetching %s: %d/%d events · %ds", name, ev.Fetched, ev.Total, el)
	case syncpkg.ProgressCalendarDone:
		return fmt.Sprintf("%d/%d calendars · %d events · %ds",
			ev.CalendarsScanned, ev.CalendarsTotal, ev.EventsUpserted, el)
	default:
		return ev.Phase
	}
}
