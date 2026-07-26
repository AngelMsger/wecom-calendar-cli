package expand

import (
	"testing"
	"time"
)

// An unbounded daily rule over a decade-wide window exceeds the per-event cap.
const unboundedDailyICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:daily\r\nDTSTART:20200101T090000Z\r\nDTEND:20200101T093000Z\r\n" +
	"RRULE:FREQ=DAILY\r\nSUMMARY:Daily\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func wideOpts() Options {
	return Options{
		Since: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		Loc:   time.UTC,
	}
}

// TestRebuildReportsTruncation is the regression guard for the silent cap: an
// event whose expansion is cut short must be named in the result, otherwise the
// series simply stops mid-window with nothing to distinguish that from the
// series genuinely ending.
func TestRebuildReportsTruncation(t *testing.T) {
	st := openStore(t)
	seedMaster(t, st, "daily", unboundedDailyICS)

	res, err := Rebuild(st, wideOpts())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if res.Instances != MaxInstancesPerEvent {
		t.Fatalf("want expansion capped at %d, got %d", MaxInstancesPerEvent, res.Instances)
	}
	if len(res.Truncated) != 1 || res.Truncated[0] != "daily" {
		t.Fatalf("want the capped uid reported, got %v", res.Truncated)
	}
}

// TestRebuildReportsNoTruncationWhenBounded is the negative case: a series that
// fits must not be flagged, so the notice stays meaningful.
func TestRebuildReportsNoTruncationWhenBounded(t *testing.T) {
	st := openStore(t)
	seedMaster(t, st, "u1", recurringICS)

	res, err := Rebuild(st, opts())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if len(res.Truncated) != 0 {
		t.Fatalf("a COUNT=3 series must not be reported as truncated, got %v", res.Truncated)
	}
}

// TestRebuildDefaultsNilLocation guards against a nil Loc panicking in the
// LocalDate conversion; ical.Parse already defaults it, so Rebuild should too.
func TestRebuildDefaultsNilLocation(t *testing.T) {
	st := openStore(t)
	seedMaster(t, st, "u1", recurringICS)

	o := opts()
	o.Loc = nil
	if _, err := Rebuild(st, o); err != nil {
		t.Fatalf("rebuild with nil Loc: %v", err)
	}
}
