package timeutil

import (
	"strings"
	"testing"
	"time"
)

var reference = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

// shanghai stands in for a display zone that is not UTC.
var shanghai = time.FixedZone("CST", 8*3600)

func TestParseInstantAcceptsTheFormsAnAgentWrites(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want time.Time
	}{
		{"now", reference},
		{"NOW", reference},
		{"now-1h", reference.Add(-time.Hour)},
		{"now + 30m", reference.Add(30 * time.Minute)},
		{"now+14d", reference.Add(14 * 24 * time.Hour)},
		{"2026-09-20T06:00:00Z", time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC)},
		{"2026-09-20T06:00:00.5+08:00", time.Date(2026, 9, 19, 22, 0, 0, 5e8, time.UTC)},
		{"2026-09-20", time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)},
	} {
		got, err := ParseInstant(tc.in, reference, nil)
		if err != nil {
			t.Fatalf("ParseInstant(%q) error: %v", tc.in, err)
		}
		if !got.Equal(tc.want) {
			t.Errorf("ParseInstant(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// A date-only value is midnight in the given zone; an instant with an offset is
// exact whatever the zone.
func TestParseInstantReadsDatesInTheGivenZone(t *testing.T) {
	t.Parallel()
	got, err := ParseInstant("2026-09-20", reference, shanghai)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 19, 16, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("date in +08:00 = %s, want %s", got.UTC(), want)
	}
	got, err = ParseInstant("2026-09-20T00:00:00Z", reference, shanghai)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("an offset-bearing instant moved with the zone: %s", got.UTC())
	}
}

func TestParseInstantRejectsNonsense(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "yesterday", "5 fortnights", "2026-13-40", "now+soon",
		"20260920", "1758326400", "2026-09-20 06:30:00", "2026-09-20T06:00:00"} {
		if _, err := ParseInstant(in, reference, nil); err == nil {
			t.Errorf("ParseInstant(%q) accepted an invalid instant", in)
		}
	}
}

// The siblings read a bare duration as "that long ago". A window here may reach
// forward, so the direction must be explicit.
func TestParseInstantRejectsABareDuration(t *testing.T) {
	t.Parallel()
	_, err := ParseInstant("7d", reference, nil)
	if err == nil || !strings.Contains(err.Error(), "now-7d or now+7d") {
		t.Fatalf("bare duration error = %v", err)
	}
}

func TestParseFlexDuration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"2h", 2 * time.Hour},
		{"3d", 3 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"1h30m", 90 * time.Minute},
	} {
		got, err := ParseFlexDuration(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseFlexDuration(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{"", "7", "1y", "week", "2026-09-20"} {
		if _, err := ParseFlexDuration(in); err == nil {
			t.Errorf("ParseFlexDuration(%q) accepted an invalid duration", in)
		}
	}
}

func TestIsDate(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]bool{
		"2026-09-20": true, " 2026-09-20 ": true,
		"2026-13-40": false, "7d": false, "2026-09-20T00:00:00Z": false, "": false,
	} {
		if got := IsDate(in); got != want {
			t.Errorf("IsDate(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestRangeResolve(t *testing.T) {
	t.Parallel()
	t.Run("since looks back from now", func(t *testing.T) {
		start, end, err := Range{Since: "7d", Now: reference}.Resolve()
		if err != nil {
			t.Fatal(err)
		}
		if !end.Equal(reference) || !start.Equal(reference.Add(-7*24*time.Hour)) {
			t.Fatalf("got %s..%s", start, end)
		}
	})
	t.Run("from defaults the end to now", func(t *testing.T) {
		start, end, err := Range{From: "now-2h", Now: reference}.Resolve()
		if err != nil {
			t.Fatal(err)
		}
		if !start.Equal(reference.Add(-2*time.Hour)) || !end.Equal(reference) {
			t.Fatalf("got %s..%s", start, end)
		}
	})
	t.Run("from defaults the end to DefaultTo when set", func(t *testing.T) {
		ahead := reference.Add(30 * 24 * time.Hour)
		_, end, err := Range{From: "2026-09-01", Now: reference, DefaultTo: ahead}.Resolve()
		if err != nil {
			t.Fatal(err)
		}
		if !end.Equal(ahead) {
			t.Fatalf("end = %s, want %s", end, ahead)
		}
	})
	t.Run("the window may reach into the future", func(t *testing.T) {
		start, end, err := Range{From: "now", To: "now+14d", Now: reference}.Resolve()
		if err != nil {
			t.Fatal(err)
		}
		if !start.Equal(reference) || !end.Equal(reference.Add(14*24*time.Hour)) {
			t.Fatalf("got %s..%s", start, end)
		}
	})
	t.Run("dates are read in Loc", func(t *testing.T) {
		start, end, err := Range{From: "2026-09-03", To: "2026-09-04", Now: reference, Loc: shanghai}.Resolve()
		if err != nil {
			t.Fatal(err)
		}
		if want := time.Date(2026, 9, 2, 16, 0, 0, 0, time.UTC); !start.Equal(want) || end.Sub(start) != 24*time.Hour {
			t.Fatalf("got %s..%s", start.UTC(), end.UTC())
		}
	})
}

func TestRangeResolveRejectsAmbiguousOrUnusableWindows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		r    Range
		want string
	}{
		{"since with from", Range{Since: "24h", From: "2026-09-03"}, "--since cannot be combined"},
		{"since with to", Range{Since: "24h", To: "2026-09-03"}, "--since cannot be combined"},
		{"to without from", Range{To: "2026-09-04"}, "--to requires --from"},
		{"no bound", Range{}, "no time range given"},
		{"reversed", Range{From: "now", To: "now-1h"}, "--to must be later than --from"},
		{"empty", Range{From: "2026-09-03", To: "2026-09-03"}, "--to must be later than --from"},
		{"zero since", Range{Since: "0s"}, "invalid --since"},
		{"negative since", Range{Since: "-1h"}, "invalid --since"},
		{"date as since", Range{Since: "2026-09-03"}, "invalid --since"},
		{"bad from", Range{From: "soon"}, "invalid --from"},
		{"bad to", Range{From: "2026-09-03", To: "later"}, "invalid --to"},
		{"from past the default end", Range{From: "now+1h"}, "not earlier than the default --to"},
	} {
		tc.r.Now = reference
		_, _, err := tc.r.Resolve()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Resolve error = %v, want it to contain %q", tc.name, err, tc.want)
		}
	}
}

func TestRangeOptionalAndRelative(t *testing.T) {
	t.Parallel()
	if (Range{}).Optional() {
		t.Error("an empty Range reports a bound")
	}
	for _, tc := range []struct {
		r    Range
		want bool
	}{
		{Range{Since: "7d"}, true},
		{Range{From: "now-7d", To: "2026-09-20"}, true},
		{Range{From: "2026-09-01", To: "Now + 14d"}, true},
		{Range{From: "2026-09-01", To: "2026-09-20T00:00:00+08:00"}, false},
		{Range{From: "2026-09-01"}, false},
	} {
		if !tc.r.Optional() {
			t.Errorf("%+v reports no bound", tc.r)
		}
		if got := tc.r.Relative(); got != tc.want {
			t.Errorf("%+v Relative() = %v, want %v", tc.r, got, tc.want)
		}
	}
}
