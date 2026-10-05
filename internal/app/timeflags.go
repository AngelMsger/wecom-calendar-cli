package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/output"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/angelmsger/wecom-calendar-cli/pkg/timeutil"
	"github.com/spf13/cobra"
)

// timeflags.go wires the family's time-window contract
// (docs/agent-facing-cli-best-practices.md §3.5) to `event list` and `expand`:
// --since <duration> looks back from now, --from is an inclusive lower bound
// and --to an exclusive upper bound, --since excludes --from/--to, and --to
// requires --from. The parsing lives in pkg/timeutil, as in prometheus-cli.
//
// A calendar looks ahead, which the siblings' event and history filters never
// do, so four things differ here on purpose (AGENTS.md records each one):
// the window may end in the future, an omitted bound defaults to 30 days from
// today instead of to now, a date-only value is a local day rather than a UTC
// one, and there is no --actor filter.

// envNoDeprecationNotice opts out of the deprecated-flag notices.
const envNoDeprecationNotice = "WECOM_CALENDAR_CLI_NO_DEPRECATION_NOTICE"

// defaultWindowDays is how far the default event window reaches on either side
// of today, and how far ahead a window with only --from extends.
const defaultWindowDays = 30

// timeFlags holds the window flags of one command. until, and a date-valued
// since, are the spellings this CLI used before it adopted the family
// vocabulary; they stay as deprecated aliases of --to and --from.
type timeFlags struct {
	since string
	from  string
	to    string
	until string
}

// timeWindow is a resolved half-open [from, to) interval.
type timeWindow struct {
	from time.Time
	to   time.Time
	// relative is set when a bound was written relative to the current instant
	// (--since, now, now±duration). Such a window moves between invocations, so
	// a pagination cursor cannot continue it.
	relative bool
}

func addEventTimeFlags(cmd *cobra.Command, t *timeFlags) {
	f := cmd.Flags()
	f.StringVar(&t.since, "since", "", "look back this far from now, e.g. 24h or 7d (a YYYY-MM-DD value is a deprecated alias of --from)")
	f.StringVar(&t.from, "from", "", "window start, inclusive: YYYY-MM-DD, RFC 3339 with an offset, or now-7d (default 30 days ago)")
	f.StringVar(&t.to, "to", "", "window end, exclusive, same forms; may be in the future, e.g. now+14d; requires --from (default 30 days ahead)")
	f.StringVar(&t.until, "until", "", "deprecated alias of --to")
}

func addExpandTimeFlags(cmd *cobra.Command, t *timeFlags) {
	f := cmd.Flags()
	f.StringVar(&t.from, "from", "", "expansion window start: YYYY-MM-DD or RFC 3339 with an offset (default 2 years ago); pins the window for later syncs")
	f.StringVar(&t.to, "to", "", "expansion window end, exclusive, same forms (default 1 year ahead); pins the window for later syncs")
	f.StringVar(&t.since, "since", "", "deprecated alias of --from (YYYY-MM-DD only; a look-back duration is rejected)")
	f.StringVar(&t.until, "until", "", "deprecated alias of --to")
}

// deprecatedFlag names one deprecated spelling a caller used and its
// replacement.
type deprecatedFlag struct {
	flag        string
	replacement string
	message     string
}

// canonical folds the deprecated aliases into the family vocabulary. It
// returns the canonical flag values, whether --to came from --until, and the
// deprecated spellings that were used. sinceNote is appended to the --since
// notice, for a command where --since keeps a meaning of its own.
func (t timeFlags) canonical(sinceNote string) (c timeFlags, untilAlias bool, used []deprecatedFlag, err error) {
	c = timeFlags{
		since: strings.TrimSpace(t.since),
		from:  strings.TrimSpace(t.from),
		to:    strings.TrimSpace(t.to),
	}
	if timeutil.IsDate(c.since) {
		if c.from != "" {
			return timeFlags{}, false, nil, fmt.Errorf("a date-valued --since is a deprecated alias of --from; pass only --from")
		}
		c.from, c.since = c.since, ""
		used = append(used, deprecatedFlag{
			flag:        "--since <date>",
			replacement: "--from <date>",
			message:     "A date-valued --since is deprecated; use --from, which is the same inclusive lower bound." + sinceNote,
		})
	}
	if until := strings.TrimSpace(t.until); until != "" {
		if c.to != "" {
			return timeFlags{}, false, nil, fmt.Errorf("--until is a deprecated alias of --to; pass only --to")
		}
		c.to, untilAlias = until, true
		used = append(used, deprecatedFlag{
			flag:        "--until",
			replacement: "--to",
			message:     "--until is deprecated; use --to, which is the same exclusive upper bound.",
		})
	}
	return c, untilAlias, used, nil
}

// deprecationNotice is the stderr record for one deprecated flag. The family
// has no earlier deprecated-flag notice, so the shape follows the shared
// standard's §4.3: what was used, its replacement, and how to silence it.
func deprecationNotice(command string, d deprecatedFlag) map[string]any {
	return map[string]any{"_notice": map[string]any{
		"deprecated_flag": map[string]any{
			"command":     command,
			"flag":        d.flag,
			"replacement": d.replacement,
			"message":     d.message,
			"silence":     "use " + d.replacement + ", or set " + envNoDeprecationNotice + "=1 to suppress",
		},
	}}
}

// emitDeprecations writes one notice per deprecated flag to stderr. stdout and
// the exit code are untouched.
func emitDeprecations(cmd *cobra.Command, used []deprecatedFlag) {
	if os.Getenv(envNoDeprecationNotice) != "" {
		return
	}
	for _, d := range used {
		output.EmitNotice(os.Stderr, deprecationNotice(cmd.CommandPath(), d))
	}
}

// defaultEventWindow is the window `event list` uses when a bound is omitted:
// from the start of the day 30 days ago to the end of the day 30 days ahead, in
// the display timezone. Anchoring on local midnight instead of the current
// instant keeps the bounds identical for every call made on the same day, which
// is what lets a cursor from one page match the next.
func defaultEventWindow(now time.Time, loc *time.Location) (from, to time.Time) {
	y, m, d := now.In(loc).Date()
	from = time.Date(y, m, d-defaultWindowDays, 0, 0, 0, 0, loc)
	to = time.Date(y, m, d+defaultWindowDays+1, 0, 0, 0, 0, loc)
	return from, to
}

// eventWindow resolves the flags of `event list`. The deprecated spellings
// that were used are returned on failure too: an error is worded in the
// canonical flags, so the notice is what ties it back to what was typed.
func (t timeFlags) eventWindow(now time.Time, loc *time.Location) (timeWindow, []deprecatedFlag, error) {
	var used []deprecatedFlag
	bad := func(err error) (timeWindow, []deprecatedFlag, error) {
		return timeWindow{}, used, badTimeRange(err,
			"--since <duration> looks back from now. --from is inclusive and --to is exclusive, and --to requires --from. "+
				"A date-only value is midnight in the display timezone ("+loc.String()+"); "+
				"an RFC 3339 instant with an offset is exact, and now±duration can reach into the future.",
			constants.AppName+" event list --since 7d",
			constants.AppName+" event list --from 2026-07-01 --to 2026-08-01",
			constants.AppName+" event list --from now --to now+14d")
	}
	c, untilAlias, used, err := t.canonical(" Here --since now takes a look-back duration such as 7d.")
	if err != nil {
		return bad(err)
	}
	defaultFrom, defaultTo := defaultEventWindow(now, loc)
	r := timeutil.Range{Since: c.since, From: c.from, To: c.to, Now: now, Loc: loc, DefaultTo: defaultTo}
	if !r.Optional() {
		return timeWindow{from: defaultFrom, to: defaultTo}, used, nil
	}
	if r.Since != "" && r.From == "" && r.To == "" {
		// A malformed date lands here too, now that --since is a duration; point
		// its author at the flag that takes dates.
		if d, err := timeutil.ParseFlexDuration(r.Since); err != nil || d <= 0 {
			return bad(fmt.Errorf("invalid --since %q: use a positive duration such as 24h or 7d (a date goes to --from)", r.Since))
		}
	}
	if untilAlias && r.Since == "" && r.From == "" {
		// --until on its own predates the rule that an upper bound needs a
		// lower one. It keeps the default lower bound so an old invocation
		// still runs; the canonical --to is held to the family rule.
		to, err := timeutil.ParseInstant(r.To, now, loc)
		if err != nil {
			return bad(fmt.Errorf("invalid --until %q: %w", r.To, err))
		}
		if !to.After(defaultFrom) {
			return bad(fmt.Errorf("--until %q is not later than the default --from (%s); pass --from",
				r.To, defaultFrom.Format(time.RFC3339)))
		}
		return timeWindow{from: defaultFrom, to: to, relative: r.Relative()}, used, nil
	}
	from, to, err := r.Resolve()
	if err != nil {
		return bad(err)
	}
	return timeWindow{from: from, to: to, relative: r.Relative()}, used, nil
}

// expandWindow resolves the flags of `expand`. The command pins the window
// recurring events are expanded over; it is not an event filter, so either
// bound may be given alone and falls back to the rolling default, and a
// look-back --since is refused: a window ending now would silently drop every
// future occurrence from the store.
func (t timeFlags) expandWindow(now time.Time, loc *time.Location) (start, end time.Time, pinned bool, used []deprecatedFlag, err error) {
	bad := func(err error) (time.Time, time.Time, bool, []deprecatedFlag, error) {
		return time.Time{}, time.Time{}, false, used, badTimeRange(err,
			"Give the expansion window as --from/--to. A date-only value is midnight in the display timezone ("+
				loc.String()+"); either bound may be omitted to keep its default (2 years back, 1 year ahead).",
			constants.AppName+" expand --from 2018-01-01 --to 2030-01-01")
	}
	if since := strings.TrimSpace(t.since); since != "" && !timeutil.IsDate(since) {
		if _, err := timeutil.ParseFlexDuration(since); err != nil {
			return bad(fmt.Errorf("invalid --since %q: expected YYYY-MM-DD (--since is a deprecated alias of --from here)", since))
		}
		return bad(fmt.Errorf("expand takes no look-back --since %q: a window ending now would drop every future occurrence", since))
	}
	c, _, used, err := t.canonical("")
	if err != nil {
		return bad(err)
	}
	start, end = expandWindowStart(), expandWindowEnd()
	if c.from != "" {
		if start, err = timeutil.ParseInstant(c.from, now, loc); err != nil {
			return bad(fmt.Errorf("invalid --from %q: %w", c.from, err))
		}
	}
	if c.to != "" {
		if end, err = timeutil.ParseInstant(c.to, now, loc); err != nil {
			return bad(fmt.Errorf("invalid --to %q: %w", c.to, err))
		}
	}
	if !end.After(start) {
		switch {
		case c.to == "":
			return bad(fmt.Errorf("--from %q is not earlier than the default --to (1 year ahead); pass --to", c.from))
		case c.from == "":
			return bad(fmt.Errorf("--to %q is not later than the default --from (2 years ago); pass --from", c.to))
		}
		return bad(fmt.Errorf("--to must be later than --from"))
	}
	return start, end, c.from != "" || c.to != "", used, nil
}

// badTimeRange is the family's error for an unusable window: usage category,
// BAD_TIME_RANGE, a hint that restates the contract, and runnable examples.
func badTimeRange(cause error, hint string, examples ...string) error {
	return cerrors.Wrap(cause, cerrors.CategoryUsage, "BAD_TIME_RANGE", cause.Error()).
		WithHint(hint).
		WithNextSteps(examples...)
}

// relativeWindowCursorError rejects --cursor on a window that moves with the
// clock. Paging needs fixed bounds: each call would resolve a different
// window, so rows could be skipped or repeated, and the cursor — bound to the
// window it was issued for — could never match. The next step restates the
// window as absolute bounds to page from the start.
func relativeWindowCursorError(w timeWindow, loc *time.Location) error {
	stamp := func(t time.Time) string { return t.In(loc).Truncate(time.Second).Format(time.RFC3339) }
	return cerrors.New(cerrors.CategoryUsage, "CURSOR_RELATIVE_WINDOW",
		"--cursor cannot continue a window that is relative to the current time").
		WithHint("--since and now±duration move with the clock, so the window this cursor was issued for is gone. "+
			"Pass absolute --from/--to on every page and start again without --cursor, "+
			"or read the relative window in one call with --all.").
		WithNextSteps(
			constants.AppName+" event list --from "+stamp(w.from)+" --to "+stamp(w.to),
			"Or repeat the relative window with --all instead of --cursor.",
		)
}
