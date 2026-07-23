package app

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/spf13/cobra"
)

// defaultPageSize bounds an event list page when neither --limit nor --all is
// given, so a very wide window returns a first page with a continuation cursor
// instead of an unbounded dump.
const defaultPageSize = 200

func newEventCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "event",
		Short: "Query calendar events",
	}
	cmd.AddCommand(newEventListCmd(s))
	return cmd
}

func newEventListCmd(s *appState) *cobra.Command {
	var since, until, calendarID, cursor string
	var limit int
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List events in a time window from the local store",
		Long: "List events overlapping [--since, --until) from the local store. Dates\n" +
			"are YYYY-MM-DD in the display timezone; --since defaults to 30 days ago\n" +
			"and --until to 30 days ahead. Run `sync` first to populate the store; a\n" +
			"staleness notice on stderr flags out-of-date data.\n\n" +
			"Results are paginated: the JSON envelope carries `has_more` and a `next`\n" +
			"cursor. Pass `--cursor <next>` to fetch the following page, or `--all` to\n" +
			"return every match in one page (fine here since the query is local).",
		Example: "  wecom-calendar-cli event list --since 2026-07-01 --until 2026-07-31\n" +
			"  wecom-calendar-cli event list --calendar 1688853806313356 --format table\n" +
			"  wecom-calendar-cli event list --since 2026-01-01 --until 2026-12-31 --all",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			loc := displayLoc()
			sinceT, untilT, err := parseWindow(since, until, loc)
			if err != nil {
				return err
			}
			offset := 0
			if cursor != "" {
				if offset, err = decodeCursor(cursor); err != nil {
					return err
				}
			}
			// --all disables paging; otherwise --limit sets the page size, or a
			// sensible default caps the first page.
			pageLimit := defaultPageSize
			if limit > 0 {
				pageLimit = limit
			}
			if all {
				pageLimit = 0
			}

			st, err := s.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			s.staleNotice(st)
			s.coverageNotice(st, sinceT, untilT)

			// Query the expanded instances so recurring events appear on every
			// occurrence in the window (deduped across calendars).
			rows, hasMore, err := st.QueryInstances(
				sinceT.UTC().UnixMilli(), untilT.UTC().UnixMilli(), calendarID, offset, pageLimit)
			if err != nil {
				return err
			}
			next := ""
			if hasMore {
				next = encodeCursor(offset + len(rows))
			}
			return s.emitList(rows, pageInfo{Next: next, HasMore: hasMore})
		},
	}
	f := cmd.Flags()
	f.StringVar(&since, "since", "", "start date YYYY-MM-DD (default 30 days ago)")
	f.StringVar(&until, "until", "", "end date YYYY-MM-DD, exclusive (default 30 days ahead)")
	f.StringVar(&calendarID, "calendar", "", "restrict to one calendar id")
	f.IntVar(&limit, "limit", 0, "page size (0 = default page size unless --all)")
	f.StringVar(&cursor, "cursor", "", "continue from a previous page's `next` cursor")
	f.BoolVar(&all, "all", false, "return every match in one page (no pagination)")
	return cmd
}

// encodeCursor/decodeCursor make an opaque, self-describing pagination cursor
// over the stable (start, uid, occurrence) ordering. Offset-based paging is safe
// here because the query is a local, read-only snapshot between calls.
func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("off:" + strconv.Itoa(offset)))
}

func decodeCursor(s string) (int, error) {
	if raw, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		if v, ok := strings.CutPrefix(string(raw), "off:"); ok {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				return n, nil
			}
		}
	}
	return 0, cerrors.Newf(cerrors.CategoryUsage, "BAD_CURSOR",
		"invalid --cursor value %q", s).
		WithHint("Pass the `next` value from a previous page verbatim, or omit --cursor to start over.")
}

// parseWindow resolves the --since/--until flags to a time range, applying
// defaults of now-30d .. now+30d. The CLI absorbs the date math so agents never
// hand-compute timestamps.
func parseWindow(since, until string, loc *time.Location) (time.Time, time.Time, error) {
	now := time.Now().In(loc)
	start := now.AddDate(0, 0, -30)
	end := now.AddDate(0, 0, 30)
	if since != "" {
		t, err := time.ParseInLocation("2006-01-02", since, loc)
		if err != nil {
			return time.Time{}, time.Time{}, badDate("since", since)
		}
		start = t
	}
	if until != "" {
		t, err := time.ParseInLocation("2006-01-02", until, loc)
		if err != nil {
			return time.Time{}, time.Time{}, badDate("until", until)
		}
		end = t
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, cerrors.New(cerrors.CategoryUsage, "BAD_WINDOW",
			"--until must be after --since")
	}
	return start, end, nil
}

func badDate(flag, val string) error {
	return cerrors.Newf(cerrors.CategoryUsage, "BAD_DATE",
		"invalid --%s date %q, expected YYYY-MM-DD", flag, val)
}

// displayLoc is the timezone used to render event times. Fixed to Asia/Shanghai
// for now; a config field will drive it later.
func displayLoc() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}
