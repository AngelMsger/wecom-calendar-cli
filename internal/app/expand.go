package app

import (
	"time"

	expandpkg "github.com/angelmsger/wecom-calendar-cli/internal/expand"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/spf13/cobra"
)

func newExpandCmd(s *appState) *cobra.Command {
	var since, until string
	cmd := &cobra.Command{
		Use:   "expand",
		Short: "Rebuild the expanded event-instances view",
		Long: "Recompute event_instances from the stored events: expand recurring\n" +
			"masters into occurrences (applying EXDATE and overrides) and fold the\n" +
			"same event across calendars into one occurrence. Pure rebuild; runs\n" +
			"automatically at the end of `sync`. Never touches your metadata.\n\n" +
			"Occurrences are expanded over a window (default 2 years back to 1 year\n" +
			"ahead). Pass --since/--until to widen it when you need to query further\n" +
			"into the past or future; a query beyond the window prints a coverage\n" +
			"notice on stderr.",
		Example: "  wecom-calendar-cli expand\n" +
			"  wecom-calendar-cli expand --since 2018-01-01 --until 2030-01-01",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			loc := displayLoc()
			startT, endT := expandWindowStart(), expandWindowEnd()
			if since != "" {
				t, err := time.ParseInLocation("2006-01-02", since, loc)
				if err != nil {
					return badDate("since", since)
				}
				startT = t
			}
			if until != "" {
				t, err := time.ParseInLocation("2006-01-02", until, loc)
				if err != nil {
					return badDate("until", until)
				}
				endT = t
			}
			if !endT.After(startT) {
				return cerrors.New(cerrors.CategoryUsage, "BAD_WINDOW",
					"--until must be after --since")
			}
			st, err := s.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			n, err := expandpkg.Rebuild(st, expandpkg.Options{Since: startT, Until: endT, Loc: loc})
			if err != nil {
				return err
			}
			return s.emit(map[string]any{
				"instances_rebuilt": n,
				"covered_from":      startT.UTC().Format(time.RFC3339),
				"covered_to":        endT.UTC().Format(time.RFC3339),
				"status":            "rebuilt",
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&since, "since", "", "expansion window start YYYY-MM-DD (default 2 years ago)")
	f.StringVar(&until, "until", "", "expansion window end YYYY-MM-DD (default 1 year ahead)")
	return cmd
}

// expandWindowStart/End bound occurrence expansion. Narrower than the sync
// window so an unbounded recurring rule stays manageable while still covering
// the useful past and near future.
func expandWindowStart() time.Time { return time.Now().AddDate(-2, 0, 0) }
func expandWindowEnd() time.Time   { return time.Now().AddDate(1, 0, 0) }
