// Package timeutil turns the family's time-window flags into a half-open
// [from, to) interval.
//
// This is deliberately the CLI's job, not the agent's: an agent should be able
// to write `--since 7d` or `--from 2026-07-01 --to now+14d` and never compute a
// timestamp. The vocabulary is the shared contract of the agent-facing CLI
// family: --since is a look-back ending now, --from is an inclusive lower bound
// and --to an exclusive upper bound, --since excludes --from/--to, and --to
// requires --from.
//
// It is the lean form of the package prometheus-cli and openobserve-cli carry,
// with two additions a calendar needs and the siblings do not: the zone a
// date-only value is read in (Range.Loc) and an upper bound that can lie in
// the future when --to is omitted (Range.DefaultTo). Both default to the
// family behaviour, so a zero Range resolves exactly as it does there.
//
// This package backs wecom-calendar-cli and is also importable as a library.
// Extend the accepted forms additively.
package timeutil

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Range is an unresolved time window described by flags. Either Since or
// From (optionally with To) is provided. Now, when zero, defaults to
// time.Now() — tests set it for determinism.
type Range struct {
	Since string
	From  string
	To    string
	Now   time.Time
	// Loc is the zone a date-only From/To value is read in. Nil means UTC, the
	// family default; a product whose days are local sets its display zone.
	Loc *time.Location
	// DefaultTo is the upper bound used when From is given without To. Zero
	// means Now, the family default; a product whose records extend into the
	// future sets its look-ahead bound instead.
	DefaultTo time.Time
}

// Optional reports whether the Range carries any bound at all, so a caller
// whose window is optional can distinguish "no window requested" from "an
// invalid window".
func (r Range) Optional() bool {
	return r.Since != "" || r.From != "" || r.To != ""
}

// Relative reports whether a bound is written relative to the current instant
// (--since, now, now±duration). Such a window moves between invocations, so it
// cannot be continued with a pagination cursor.
func (r Range) Relative() bool {
	return r.Since != "" || isNowExpr(r.From) || isNowExpr(r.To)
}

// Resolve turns the Range into start/end instants. It enforces the flag
// exclusivity rules and rejects an empty or reversed window.
func (r Range) Resolve() (start, end time.Time, err error) {
	if r.Since != "" && (r.From != "" || r.To != "") {
		return time.Time{}, time.Time{}, fmt.Errorf("--since cannot be combined with --from or --to")
	}
	if r.To != "" && r.From == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("--to requires --from")
	}
	now := r.Now
	if now.IsZero() {
		now = time.Now()
	}

	switch {
	case r.Since != "":
		d, derr := ParseFlexDuration(r.Since)
		if derr != nil || d <= 0 {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --since %q: use a positive duration such as 24h or 7d", r.Since)
		}
		return now.Add(-d), now, nil
	case r.From != "":
		start, err = ParseInstant(r.From, now, r.Loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --from %q: %w", r.From, err)
		}
		if r.To == "" {
			end = r.DefaultTo
			if end.IsZero() {
				end = now
			}
			if !end.After(start) {
				return time.Time{}, time.Time{}, fmt.Errorf(
					"--from %q is not earlier than the default --to (%s); pass --to", r.From, end.Format(time.RFC3339))
			}
			return start, end, nil
		}
		end, err = ParseInstant(r.To, now, r.Loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --to %q: %w", r.To, err)
		}
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("no time range given: pass --since (e.g. 7d) or --from/--to")
	}

	if !end.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("--to must be later than --from")
	}
	return start, end, nil
}

var flexDurationRe = regexp.MustCompile(`^(\d+)\s*([smhdw])$`)

// ParseFlexDuration parses a duration string. In addition to Go's native units
// (s, m, h) it understands d (days) and w (weeks), e.g. "15m", "24h", "7d".
func ParseFlexDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if m := flexDurationRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "d":
			return time.Duration(n) * 24 * time.Hour, nil
		case "w":
			return time.Duration(n) * 7 * 24 * time.Hour, nil
		}
	}
	return time.ParseDuration(s)
}

// DateLayout is the date-only form From and To accept.
const DateLayout = "2006-01-02"

// IsDate reports whether s is a well-formed date-only value.
func IsDate(s string) bool {
	_, err := time.Parse(DateLayout, strings.TrimSpace(s))
	return err == nil
}

var nowExprRe = regexp.MustCompile(`^(?i:now)\s*([+-])\s*(.+)$`)

func isNowExpr(s string) bool {
	s = strings.TrimSpace(s)
	return strings.EqualFold(s, "now") || nowExprRe.MatchString(s)
}

// ParseInstant parses a single point in time. It accepts:
//   - "now"                 → now
//   - "now-1h" / "now+14d"  → now ± duration (supports d/w units)
//   - RFC 3339 with an offset (with or without sub-second precision) → exact
//   - "2006-01-02"          → midnight in loc (UTC when loc is nil)
//
// A bare duration is rejected rather than read as "that long ago": a window
// may reach into the future, so the direction has to be written out.
func ParseInstant(s string, now time.Time, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if loc == nil {
		loc = time.UTC
	}
	if strings.EqualFold(s, "now") {
		return now, nil
	}
	if m := nowExprRe.FindStringSubmatch(s); m != nil {
		d, err := ParseFlexDuration(m[2])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid duration %q (use a form such as 24h or 7d)", m[2])
		}
		if m[1] == "-" {
			return now.Add(-d), nil
		}
		return now.Add(d), nil
	}
	if _, err := ParseFlexDuration(s); err == nil {
		return time.Time{}, fmt.Errorf("a bare duration is ambiguous here; write now-%s or now+%s", s, s)
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	if t, err := time.ParseInLocation(DateLayout, s, loc); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("use RFC 3339 with an offset, a date in YYYY-MM-DD form, or now±duration such as now+14d")
}
