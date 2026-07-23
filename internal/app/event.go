package app

import (
	"time"

	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/spf13/cobra"
)

func newEventCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "event",
		Short: "Query calendar events",
	}
	cmd.AddCommand(newEventListCmd(s))
	return cmd
}

func newEventListCmd(s *appState) *cobra.Command {
	var since, until, calendarID string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List events in a time window from the local store",
		Long: "List events overlapping [--since, --until) from the local store. Dates\n" +
			"are YYYY-MM-DD in the display timezone; --since defaults to 30 days ago\n" +
			"and --until to 30 days ahead. Run `sync` first to populate the store; a\n" +
			"staleness notice on stderr flags out-of-date data.",
		Example: "  wecom-calendar-cli event list --since 2026-07-01 --until 2026-07-31\n" +
			"  wecom-calendar-cli event list --calendar 1688853806313356 --format table",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			loc := displayLoc()
			sinceT, untilT, err := parseWindow(since, until, loc)
			if err != nil {
				return err
			}
			st, err := s.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			s.staleNotice(st)

			// Query the expanded instances so recurring events appear on every
			// occurrence in the window (deduped across calendars).
			rows, err := st.QueryInstances(sinceT.UTC().UnixMilli(), untilT.UTC().UnixMilli(), calendarID)
			if err != nil {
				return err
			}
			hasMore := false
			if limit > 0 && len(rows) > limit {
				rows = rows[:limit]
				hasMore = true
			}
			return s.emitList(rows, pageInfo{HasMore: hasMore})
		},
	}
	f := cmd.Flags()
	f.StringVar(&since, "since", "", "start date YYYY-MM-DD (default 30 days ago)")
	f.StringVar(&until, "until", "", "end date YYYY-MM-DD, exclusive (default 30 days ahead)")
	f.StringVar(&calendarID, "calendar", "", "restrict to one calendar id")
	f.IntVar(&limit, "limit", 0, "cap the number of events returned (0 = no cap)")
	return cmd
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
