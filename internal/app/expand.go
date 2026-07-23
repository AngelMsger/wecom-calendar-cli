package app

import (
	"time"

	expandpkg "github.com/angelmsger/wecom-calendar-cli/internal/expand"
	"github.com/spf13/cobra"
)

func newExpandCmd(s *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "expand",
		Short: "Rebuild the expanded event-instances view",
		Long: "Recompute event_instances from the stored events: expand recurring\n" +
			"masters into occurrences (applying EXDATE and overrides) and fold the\n" +
			"same event across calendars into one occurrence. Pure rebuild; runs\n" +
			"automatically at the end of `sync`. Never touches your metadata.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := s.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			n, err := expandpkg.Rebuild(st, expandpkg.Options{
				Since: expandWindowStart(),
				Until: expandWindowEnd(),
				Loc:   displayLoc(),
			})
			if err != nil {
				return err
			}
			return s.emit(map[string]any{"instances_rebuilt": n, "status": "rebuilt"})
		},
	}
}

// expandWindowStart/End bound occurrence expansion. Narrower than the sync
// window so an unbounded recurring rule stays manageable while still covering
// the useful past and near future.
func expandWindowStart() time.Time { return time.Now().AddDate(-2, 0, 0) }
func expandWindowEnd() time.Time   { return time.Now().AddDate(1, 0, 0) }
