package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/store"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// windowNow is mid-afternoon in the display timezone, so "today" is unambiguous.
var windowNow = time.Date(2026, 10, 5, 15, 30, 45, 123e6, displayLoc())

func mustEventWindow(t *testing.T, f timeFlags) timeWindow {
	t.Helper()
	w, _, err := f.eventWindow(windowNow, displayLoc())
	if err != nil {
		t.Fatalf("eventWindow(%+v): %v", f, err)
	}
	return w
}

func wantWindow(t *testing.T, w timeWindow, from, to time.Time) {
	t.Helper()
	if !w.from.Equal(from) || !w.to.Equal(to) {
		t.Fatalf("window = [%s, %s), want [%s, %s)",
			w.from.Format(time.RFC3339Nano), w.to.Format(time.RFC3339Nano),
			from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano))
	}
}

// Family rules: --since looks back from now, --from/--to bound [from, to).
func TestEventWindowFamilyVocabulary(t *testing.T) {
	loc := displayLoc()
	w := mustEventWindow(t, timeFlags{since: "7d"})
	wantWindow(t, w, windowNow.Add(-7*24*time.Hour), windowNow)
	if !w.relative {
		t.Fatal("a --since window must be marked relative")
	}

	w = mustEventWindow(t, timeFlags{from: "2026-07-01", to: "2026-08-01"})
	wantWindow(t, w, time.Date(2026, 7, 1, 0, 0, 0, 0, loc), time.Date(2026, 8, 1, 0, 0, 0, 0, loc))
	if w.relative {
		t.Fatal("absolute bounds must not be marked relative")
	}
}

func TestEventWindowRejectsAmbiguousFlags(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags timeFlags
		want  string
	}{
		{"since with from", timeFlags{since: "24h", from: "2026-09-03"}, "--since cannot be combined"},
		{"since with to", timeFlags{since: "24h", to: "2026-09-04"}, "--since cannot be combined"},
		{"to without from", timeFlags{to: "2026-09-04"}, "--to requires --from"},
		{"reversed", timeFlags{from: "2026-09-04", to: "2026-09-03"}, "--to must be later than --from"},
		{"bad since", timeFlags{since: "soon"}, "invalid --since"},
		{"malformed date as since", timeFlags{since: "2026-7-1"}, "a date goes to --from"},
		{"zero since", timeFlags{since: "0h"}, "invalid --since"},
		{"bare duration as from", timeFlags{from: "7d"}, "now-7d or now+7d"},
		{"from past the default end", timeFlags{from: "2027-06-01"}, "pass --to"},
		{"alias and canonical upper bound", timeFlags{from: "2026-09-01", to: "2026-09-04", until: "2026-09-05"}, "pass only --to"},
		{"alias and canonical lower bound", timeFlags{since: "2026-09-01", from: "2026-09-02"}, "pass only --from"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.flags.eventWindow(windowNow, displayLoc())
			ce := cerrors.AsCLIError(err)
			if ce == nil || ce.Code != "BAD_TIME_RANGE" || ce.Category != cerrors.CategoryUsage {
				t.Fatalf("unexpected error: %+v", ce)
			}
			if !strings.Contains(ce.Message, tc.want) {
				t.Fatalf("message %q does not contain %q", ce.Message, tc.want)
			}
			if !strings.Contains(ce.Hint, "--to requires --from") || len(ce.NextSteps) != 3 ||
				ce.NextSteps[0] != "wecom-calendar-cli event list --since 7d" {
				t.Fatalf("missing hint or runnable next steps: %+v", ce)
			}
		})
	}
}

// Intentional difference (a): with no window flag the window is 30 days either
// side of today on local day boundaries, so every call made on the same day
// resolves the same bounds and a cursor from one page matches the next. The
// window used to be relative to the current millisecond, which made every
// default-window cursor fail with CURSOR_MISMATCH.
func TestDefaultEventWindowIsStableForTheDay(t *testing.T) {
	loc := displayLoc()
	morning := time.Date(2026, 10, 5, 0, 0, 1, 0, loc)
	night := time.Date(2026, 10, 5, 23, 59, 59, 0, loc)

	first, second := mustWindowAt(t, morning), mustWindowAt(t, night)
	wantWindow(t, first, time.Date(2026, 9, 5, 0, 0, 0, 0, loc), time.Date(2026, 11, 5, 0, 0, 0, 0, loc))
	wantWindow(t, second, first.from, first.to)
	if first.relative {
		t.Fatal("the default window must stay pageable")
	}
	// It still covers 30 days either side of the current instant.
	if first.from.After(night.AddDate(0, 0, -30)) || first.to.Before(night.AddDate(0, 0, 30)) {
		t.Fatalf("default window [%s, %s) does not cover now±30d", first.from, first.to)
	}

	digest := func(w timeWindow) string {
		return filterDigest(w.from.UTC().UnixMilli(), w.to.UTC().UnixMilli(), "")
	}
	cursor := encodeCursor(store.InstanceCursor{StartMs: 1, UID: "u", Key: "k"}, digest(first))
	if _, err := decodeCursor(cursor, digest(second)); err != nil {
		t.Fatalf("a default-window cursor must resume later the same day: %v", err)
	}
	// The next local day is a different window, and says so.
	_, err := decodeCursor(cursor, digest(mustWindowAt(t, night.Add(2*time.Second))))
	if ce := cerrors.AsCLIError(err); ce == nil || ce.Code != "CURSOR_MISMATCH" ||
		!strings.Contains(ce.Hint, "absolute --from/--to") {
		t.Fatalf("crossing midnight should be a CURSOR_MISMATCH naming absolute bounds: %+v", ce)
	}
}

func mustWindowAt(t *testing.T, now time.Time) timeWindow {
	t.Helper()
	w, _, err := timeFlags{}.eventWindow(now, displayLoc())
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// Intentional difference (b): a calendar looks ahead. --to may lie in the
// future, now+duration is a valid instant, and --from without --to ends 30
// days ahead instead of now.
func TestEventWindowReachesIntoTheFuture(t *testing.T) {
	loc := displayLoc()
	w := mustEventWindow(t, timeFlags{from: "now", to: "now+14d"})
	wantWindow(t, w, windowNow, windowNow.Add(14*24*time.Hour))
	if !w.relative {
		t.Fatal("now-based bounds must be marked relative")
	}

	w = mustEventWindow(t, timeFlags{from: "2026-10-01", to: "2027-01-01"})
	if !w.to.After(windowNow) {
		t.Fatal("--to later than now was not kept")
	}

	w = mustEventWindow(t, timeFlags{from: "2026-10-01"})
	wantWindow(t, w, time.Date(2026, 10, 1, 0, 0, 0, 0, loc), time.Date(2026, 11, 5, 0, 0, 0, 0, loc))
	if !w.to.After(windowNow.AddDate(0, 0, 30)) || w.relative {
		t.Fatalf("--from alone should end 30 days ahead on a day boundary, got %s (relative=%v)", w.to, w.relative)
	}
}

// Intentional difference (c): a date-only value is midnight in the display
// timezone, not UTC, because calendar days are local. An RFC 3339 instant with
// an offset is exact.
func TestEventWindowDateIsALocalDay(t *testing.T) {
	w := mustEventWindow(t, timeFlags{from: "2026-07-01", to: "2026-07-02"})
	wantWindow(t, w,
		time.Date(2026, 6, 30, 16, 0, 0, 0, time.UTC), // 2026-07-01T00:00:00+08:00
		time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC))

	w = mustEventWindow(t, timeFlags{from: "2026-07-01T00:00:00Z", to: "2026-07-01T09:30:00+08:00"})
	wantWindow(t, w, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 1, 1, 30, 0, 0, time.UTC))
}

// Intentional difference (d): there is no actor filter; whoami and is_self
// cover identity. Adding --actor means revisiting that decision in AGENTS.md.
func TestEventListHasNoActorFlag(t *testing.T) {
	for _, cmd := range []string{"list", "get"} {
		c, _, err := newEventCmd(&appState{}).Find([]string{cmd})
		if err != nil {
			t.Fatal(err)
		}
		if c.Flags().Lookup("actor") != nil {
			t.Fatalf("event %s grew an --actor flag", cmd)
		}
	}
}

// The spellings this CLI used before the family vocabulary keep working and
// resolve to the same window as their replacements.
func TestEventWindowDeprecatedAliases(t *testing.T) {
	loc := displayLoc()
	canonical := mustEventWindow(t, timeFlags{from: "2026-07-01", to: "2026-07-31"})

	w, used, err := timeFlags{since: "2026-07-01", until: "2026-07-31"}.eventWindow(windowNow, loc)
	if err != nil {
		t.Fatal(err)
	}
	wantWindow(t, w, canonical.from, canonical.to)
	if len(used) != 2 || used[0].flag != "--since <date>" || used[0].replacement != "--from <date>" ||
		used[1].flag != "--until" || used[1].replacement != "--to" {
		t.Fatalf("deprecations = %+v", used)
	}

	// Mixed with a canonical flag, only the alias is reported.
	_, used, err = timeFlags{from: "2026-07-01", until: "2026-07-31"}.eventWindow(windowNow, loc)
	if err != nil || len(used) != 1 || used[0].flag != "--until" {
		t.Fatalf("mixed alias: used=%+v err=%v", used, err)
	}
	if _, used, _ := (timeFlags{since: "7d"}).eventWindow(windowNow, loc); len(used) != 0 {
		t.Fatalf("a duration --since is not deprecated: %+v", used)
	}

	// --until alone predates "--to requires --from" and keeps its default
	// lower bound; the canonical --to alone is rejected (see the table above).
	w, used, err = timeFlags{until: "2026-12-01"}.eventWindow(windowNow, loc)
	if err != nil || len(used) != 1 {
		t.Fatalf("--until alone: used=%+v err=%v", used, err)
	}
	wantWindow(t, w, time.Date(2026, 9, 5, 0, 0, 0, 0, loc), time.Date(2026, 12, 1, 0, 0, 0, 0, loc))
	if _, _, err := (timeFlags{until: "2026-01-01"}).eventWindow(windowNow, loc); err == nil {
		t.Fatal("--until before the default lower bound must be rejected")
	}
}

func TestDeprecationNoticeShape(t *testing.T) {
	_, used, err := timeFlags{since: "2026-07-01", until: "2026-07-31"}.eventWindow(windowNow, displayLoc())
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct{ flag, replacement string }{
		{"--since <date>", "--from <date>"},
		{"--until", "--to"},
	} {
		raw, err := json.Marshal(deprecationNotice("wecom-calendar-cli event list", used[i]))
		if err != nil {
			t.Fatal(err)
		}
		var notice struct {
			Notice struct {
				Deprecated struct {
					Command, Flag, Replacement, Message, Silence string
				} `json:"deprecated_flag"`
			} `json:"_notice"`
		}
		if err := json.Unmarshal(raw, &notice); err != nil {
			t.Fatal(err)
		}
		d := notice.Notice.Deprecated
		if d.Command != "wecom-calendar-cli event list" || d.Flag != want.flag || d.Replacement != want.replacement ||
			!strings.Contains(d.Message, "deprecated") ||
			!strings.Contains(d.Silence, want.replacement) || !strings.Contains(d.Silence, envNoDeprecationNotice+"=1") {
			t.Fatalf("notice = %s", raw)
		}
	}
}

// expand pins the expansion window. It takes --from/--to, keeps the old
// spellings as aliases, lets either bound default, and refuses a look-back.
func TestExpandWindow(t *testing.T) {
	loc := displayLoc()
	wantStart := time.Date(2018, 1, 1, 0, 0, 0, 0, loc)
	wantEnd := time.Date(2030, 1, 1, 0, 0, 0, 0, loc)

	start, end, pinned, used, err := timeFlags{from: "2018-01-01", to: "2030-01-01"}.expandWindow(windowNow, loc)
	if err != nil || !pinned || len(used) != 0 || !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("--from/--to: %s..%s pinned=%v used=%+v err=%v", start, end, pinned, used, err)
	}

	start, end, pinned, used, err = timeFlags{since: "2018-01-01", until: "2030-01-01"}.expandWindow(windowNow, loc)
	if err != nil || !pinned || !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("aliases: %s..%s pinned=%v err=%v", start, end, pinned, err)
	}
	if len(used) != 2 || used[0].replacement != "--from <date>" || used[1].replacement != "--to" ||
		strings.Contains(used[0].message, "look-back") {
		t.Fatalf("alias deprecations = %+v", used)
	}

	// Either bound alone keeps the other's rolling default.
	start, end, pinned, _, err = timeFlags{to: "2030-01-01"}.expandWindow(windowNow, loc)
	if err != nil || !pinned || !end.Equal(wantEnd) || start.Sub(expandWindowStart()).Abs() > time.Minute {
		t.Fatalf("--to alone: %s..%s pinned=%v err=%v", start, end, pinned, err)
	}
	if _, _, pinned, _, err = (timeFlags{}).expandWindow(windowNow, loc); err != nil || pinned {
		t.Fatalf("no flags: pinned=%v err=%v", pinned, err)
	}
}

func TestExpandRejectsLookBackSince(t *testing.T) {
	for _, since := range []string{"7d", "24h", "1h30m"} {
		_, _, _, _, err := timeFlags{since: since}.expandWindow(windowNow, displayLoc())
		ce := cerrors.AsCLIError(err)
		if ce == nil || ce.Code != "BAD_TIME_RANGE" || ce.Category != cerrors.CategoryUsage ||
			!strings.Contains(ce.Message, "future occurrence") || !strings.Contains(ce.Hint, "--from/--to") ||
			len(ce.NextSteps) != 1 || !strings.Contains(ce.NextSteps[0], "expand --from") {
			t.Fatalf("expand --since %s: %+v", since, ce)
		}
	}
	for _, flags := range []timeFlags{
		{from: "2030-01-01", to: "2018-01-01"},
		{since: "2018-1-1"},
		{since: "2018-01-01", from: "2019-01-01"},
	} {
		_, _, _, _, err := flags.expandWindow(windowNow, displayLoc())
		if ce := cerrors.AsCLIError(err); ce == nil || ce.Code != "BAD_TIME_RANGE" ||
			strings.Contains(ce.Message, "future occurrence") {
			t.Fatalf("expand %+v: %+v", flags, ce)
		}
	}
}

// runCLI drives the real command tree against a throwaway config directory and
// returns what it wrote to stdout and stderr. The structured error itself is
// printed by Execute, so stderr here holds notices only.
func runCLI(t *testing.T, cfgDir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv(envNoDeprecationNotice, "")
	return captureCLI(t, cfgDir, args...)
}

// captureCLI is runCLI without a say on the deprecation-notice variable.
func captureCLI(t *testing.T, cfgDir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv(envNoSkillHint, "1")
	t.Setenv("WECOM_CALENDAR_CONTEXT", "") // a developer's context does not exist in cfgDir
	dir := t.TempDir()
	outFile, outErr := os.Create(filepath.Join(dir, "stdout"))
	errFile, errErr := os.Create(filepath.Join(dir, "stderr"))
	if outErr != nil || errErr != nil {
		t.Fatal(outErr, errErr)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, errFile
	root, _ := newRootCmdWithState()
	root.SetArgs(append([]string{"--config", cfgDir}, args...))
	err = root.Execute()
	os.Stdout, os.Stderr = origOut, origErr
	outFile.Close()
	errFile.Close()
	outBytes, _ := os.ReadFile(filepath.Join(dir, "stdout"))
	errBytes, _ := os.ReadFile(filepath.Join(dir, "stderr"))
	return string(outBytes), string(errBytes), err
}

// seedInstances stores one occurrence per uid, an hour apart starting at first.
func seedInstances(t *testing.T, cfgDir string, first time.Time, uids ...string) {
	t.Helper()
	st, err := store.Open(storePath(cfgDir))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i, uid := range uids {
		start := first.Add(time.Duration(i) * time.Hour)
		if err := st.InsertInstance(store.InstanceRow{
			UID: uid, OccurrenceKey: uid + "-k", PrimaryCalendarID: "c1", SourceCalendarIDs: `["c1"]`,
			SourceCount: 1, Summary: uid, Start: start, End: start.Add(30 * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

type eventPage struct {
	Items []struct {
		UID string `json:"uid"`
	} `json:"items"`
	Next    string `json:"next"`
	HasMore bool   `json:"has_more"`
}

func decodePage(t *testing.T, stdout string) eventPage {
	t.Helper()
	var page eventPage
	if err := json.Unmarshal([]byte(stdout), &page); err != nil {
		t.Fatalf("stdout is not a list envelope: %v\n%s", err, stdout)
	}
	return page
}

// The regression the day-aligned default fixes, through the real command: the
// `next` cursor of a default-window page resumes the next page.
func TestEventListDefaultWindowCursorResumes(t *testing.T) {
	cfgDir := t.TempDir()
	seedInstances(t, cfgDir, time.Now().Add(-90*time.Minute), "past", "soon", "later")

	stdout, _, err := runCLI(t, cfgDir, "event", "list", "--format", "json", "--limit", "2")
	if err != nil {
		t.Fatal(err)
	}
	first := decodePage(t, stdout)
	if len(first.Items) != 2 || !first.HasMore || first.Next == "" {
		t.Fatalf("first page = %+v", first)
	}
	stdout, _, err = runCLI(t, cfgDir, "event", "list", "--format", "json", "--limit", "2", "--cursor", first.Next)
	if err != nil {
		t.Fatalf("default-window cursor was rejected: %v", err)
	}
	second := decodePage(t, stdout)
	if len(second.Items) != 1 || second.Items[0].UID != "later" || second.HasMore {
		t.Fatalf("second page = %+v", second)
	}
}

// Paging needs fixed bounds. A window relative to the current time is rejected
// with --cursor and the error names absolute --from/--to; --all reads it once.
func TestEventListCursorNeedsFixedWindow(t *testing.T) {
	cfgDir := t.TempDir()
	seedInstances(t, cfgDir, time.Now().Add(-3*time.Hour), "a", "b")

	stdout, _, err := runCLI(t, cfgDir, "event", "list", "--format", "json", "--since", "7d", "--limit", "1")
	if err != nil {
		t.Fatal(err)
	}
	first := decodePage(t, stdout)
	if len(first.Items) != 1 || !first.HasMore {
		t.Fatalf("first page = %+v", first)
	}
	for _, window := range [][]string{
		{"--since", "7d"},
		{"--from", "now-7d", "--to", "now+7d"},
		{"--from", "2020-01-01", "--to", "now"},
	} {
		args := append([]string{"event", "list", "--format", "json", "--cursor", first.Next}, window...)
		_, _, err := runCLI(t, cfgDir, args...)
		ce := cerrors.AsCLIError(err)
		if ce == nil || ce.Code != "CURSOR_RELATIVE_WINDOW" || ce.Category != cerrors.CategoryUsage {
			t.Fatalf("%v: unexpected error %+v", window, ce)
		}
		if !strings.Contains(ce.Hint, "absolute --from/--to") || len(ce.NextSteps) == 0 ||
			!strings.Contains(ce.NextSteps[0], "event list --from 20") || !strings.Contains(ce.NextSteps[0], " --to 20") {
			t.Fatalf("%v: the error must name absolute bounds: %+v", window, ce)
		}
	}

	// --all resolves the relative window once and returns every match.
	stdout, _, err = runCLI(t, cfgDir, "event", "list", "--format", "json", "--since", "7d", "--all")
	if err != nil {
		t.Fatal(err)
	}
	if all := decodePage(t, stdout); len(all.Items) != 2 || all.HasMore || all.Next != "" {
		t.Fatalf("--all page = %+v", all)
	}

	// Fixed bounds page normally, and a cursor from another window is still a mismatch.
	today := time.Now().In(displayLoc())
	fixed := []string{"event", "list", "--format", "json", "--limit", "1",
		"--from", today.AddDate(0, 0, -1).Format("2006-01-02"),
		"--to", today.AddDate(0, 0, 2).Format("2006-01-02")}
	stdout, _, err = runCLI(t, cfgDir, fixed...)
	if err != nil {
		t.Fatal(err)
	}
	page := decodePage(t, stdout)
	stdout, _, err = runCLI(t, cfgDir, append(fixed, "--cursor", page.Next)...)
	if err != nil {
		t.Fatalf("fixed-window cursor was rejected: %v", err)
	}
	if next := decodePage(t, stdout); len(next.Items) != 1 || next.Items[0].UID != "b" || next.HasMore {
		t.Fatalf("fixed second page = %+v", next)
	}
	_, _, err = runCLI(t, cfgDir, "event", "list", "--format", "json", "--cursor", first.Next)
	if ce := cerrors.AsCLIError(err); ce == nil || ce.Code != "CURSOR_MISMATCH" {
		t.Fatalf("cursor from another window: %+v", ce)
	}
}

// paginationNotices returns the `_notice.pagination` records on stderr.
func paginationNotices(t *testing.T, stderr string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		if line == "" {
			continue
		}
		var record struct {
			Notice map[string]any `json:"_notice"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("stderr line is not a JSON notice: %q", line)
		}
		if _, ok := record.Notice["pagination"]; ok {
			out = append(out, record.Notice)
		}
	}
	return out
}

// NDJSON keeps rows on stdout and carries the page descriptor on stderr: the
// event list adapter must hand `next` to the renderer, projection must not
// drop it, and the token must resume the next page.
func TestEventListNDJSONContinuation(t *testing.T) {
	cfgDir := t.TempDir()
	seedInstances(t, cfgDir, time.Now().Add(-90*time.Minute), "a", "b")
	list := []string{"event", "list", "--format", "ndjson", "--fields", "uid", "--limit", "1"}

	stdout, stderr, err := runCLI(t, cfgDir, list...)
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "{\"uid\":\"a\"}\n" {
		t.Fatalf("stdout must hold item records only, got %q", stdout)
	}
	notices := paginationNotices(t, stderr)
	if len(notices) != 1 {
		t.Fatalf("want one pagination notice, stderr: %s", stderr)
	}
	pagination, _ := notices[0]["pagination"].(map[string]any)
	next, _ := pagination["next"].(string)
	steps, _ := notices[0]["next_steps"].([]any)
	if next == "" || pagination["has_more"] != true || len(steps) != 1 ||
		steps[0] != "Pass next as --cursor to retrieve the next page." {
		t.Fatalf("notice = %v", notices[0])
	}

	stdout, stderr, err = runCLI(t, cfgDir, append(list, "--cursor", next)...)
	if err != nil {
		t.Fatalf("the emitted token did not resume: %v", err)
	}
	if stdout != "{\"uid\":\"b\"}\n" || len(paginationNotices(t, stderr)) != 0 {
		t.Fatalf("last page: stdout %q stderr %s", stdout, stderr)
	}

	// The table footer names the same continuation flag.
	stdout, _, err = runCLI(t, cfgDir, "event", "list", "--format", "table", "--fields", "uid", "--limit", "1")
	if err != nil || !strings.Contains(stdout, "re-run with --cursor "+next) {
		t.Fatalf("table footer: %q err=%v", stdout, err)
	}

	// Complete and unpaginated results advertise nothing.
	for _, args := range [][]string{
		{"event", "list", "--format", "ndjson", "--all"},
		{"calendar", "list", "--format", "ndjson"},
		{"meta", "list", "--format", "ndjson"},
	} {
		_, stderr, err := runCLI(t, cfgDir, args...)
		if err != nil || len(paginationNotices(t, stderr)) != 0 {
			t.Fatalf("%v: err=%v stderr=%s", args, err, stderr)
		}
	}
}

// A deprecated spelling emits one structured stderr notice each and leaves
// stdout and the result alone; the documented variable silences them.
func TestDeprecatedFlagNoticesOnStderr(t *testing.T) {
	cfgDir := t.TempDir()
	args := []string{"event", "list", "--format", "json", "--since", "2026-07-01", "--until", "2026-07-31"}

	stdout, stderr, err := runCLI(t, cfgDir, args...)
	if err != nil {
		t.Fatal(err)
	}
	if page := decodePage(t, stdout); page.HasMore || len(page.Items) != 0 {
		t.Fatalf("stdout changed: %s", stdout)
	}
	if got := strings.Count(stderr, `"deprecated_flag"`); got != 2 {
		t.Fatalf("want two deprecation notices, got %d: %s", got, stderr)
	}
	if !strings.Contains(stderr, `"flag":"--until"`) || !strings.Contains(stderr, `"replacement":"--to"`) ||
		!strings.Contains(stderr, `"flag":"--since <date>"`) || !strings.Contains(stderr, `"replacement":"--from <date>"`) {
		t.Fatalf("stderr = %s", stderr)
	}

	canonical, stderr, err := runCLI(t, cfgDir, "event", "list", "--format", "json", "--from", "2026-07-01", "--to", "2026-07-31")
	if err != nil || canonical != stdout || strings.Contains(stderr, "deprecated_flag") {
		t.Fatalf("canonical flags: err=%v stderr=%s", err, stderr)
	}

	_, stderr, err = runCLI(t, cfgDir, "expand", "--since", "2018-01-01", "--until", "2030-01-01")
	if err != nil || strings.Count(stderr, `"deprecated_flag"`) != 2 ||
		!strings.Contains(stderr, `"command":"wecom-calendar-cli expand"`) {
		t.Fatalf("expand aliases: err=%v stderr=%s", err, stderr)
	}
}

// A failing window is described in the canonical flags, so the notice that
// maps the typed spelling onto them is printed before the error.
func TestDeprecatedFlagNoticePrecedesAWindowError(t *testing.T) {
	_, stderr, err := runCLI(t, t.TempDir(), "event", "list", "--since", "2026-08-01", "--until", "2026-07-01")
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "BAD_TIME_RANGE" || ce.Message != "--to must be later than --from" {
		t.Fatalf("unexpected error: %+v", ce)
	}
	if strings.Count(stderr, `"deprecated_flag"`) != 2 {
		t.Fatalf("the aliases behind the error were not named: %s", stderr)
	}
}

func TestDeprecatedFlagNoticeCanBeSilenced(t *testing.T) {
	t.Setenv(envNoDeprecationNotice, "1")
	stdout, stderr, err := captureCLI(t, t.TempDir(), "event", "list", "--format", "json", "--until", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr, "deprecated_flag") {
		t.Fatalf("silenced notice was still emitted: %s", stderr)
	}
	if page := decodePage(t, stdout); page.HasMore {
		t.Fatalf("stdout changed: %s", stdout)
	}
}
