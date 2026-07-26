package app

import (
	"time"

	expandpkg "github.com/angelmsger/wecom-calendar-cli/internal/expand"
	"github.com/angelmsger/wecom-calendar-cli/internal/store"
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
			"notice on stderr. A window set that way is remembered and reused by every\n" +
			"later `sync`, so it survives the next refresh; run `expand` with no flags\n" +
			"to forget it and return to the rolling default.",
		Example: "  wecom-calendar-cli expand\n" +
			"  wecom-calendar-cli expand --since 2018-01-01 --until 2030-01-01",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			loc := displayLoc()
			pinned := since != "" || until != ""
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
			res, err := expandpkg.Rebuild(st, expandpkg.Options{Since: startT, Until: endT, Loc: loc})
			if err != nil {
				return err
			}
			// Record (or clear) the pin only after the rebuild succeeds, so a
			// failed run never changes the window later syncs will use.
			if err := setExpandPin(st, pinned, startT, endT); err != nil {
				return err
			}
			s.truncationNotice(res)
			out := map[string]any{
				"instances_rebuilt": res.Instances,
				"covered_from":      startT.UTC().Format(time.RFC3339),
				"covered_to":        endT.UTC().Format(time.RFC3339),
				"window_pinned":     pinned,
				"status":            "rebuilt",
			}
			addTruncation(out, res)
			return s.emit(out)
		},
	}
	f := cmd.Flags()
	f.StringVar(&since, "since", "", "expansion window start YYYY-MM-DD (default 2 years ago); pins the window for later syncs")
	f.StringVar(&until, "until", "", "expansion window end YYYY-MM-DD (default 1 year ahead); pins the window for later syncs")
	return cmd
}

// expandWindowStart/End bound occurrence expansion by default. Narrower than the
// sync window so an unbounded recurring rule stays manageable while still
// covering the useful past and near future. They roll forward with the clock;
// an explicitly pinned window (see setExpandPin) overrides them.
func expandWindowStart() time.Time { return time.Now().AddDate(-2, 0, 0) }
func expandWindowEnd() time.Time   { return time.Now().AddDate(1, 0, 0) }

// resolveExpandWindow returns the window the next rebuild should cover: the
// window pinned by an earlier `expand --since/--until` if one is recorded, else
// the rolling default. `sync` uses it so its automatic rebuild does not quietly
// discard a window the user deliberately chose — the coverage notice tells them
// to widen with `expand`, and the Skill tells agents to re-sync, so without this
// the two instructions would undo each other on every cycle.
func resolveExpandWindow(st *store.Store) (start, end time.Time, pinned bool) {
	start, end = expandWindowStart(), expandWindowEnd()
	ps, okS, errS := st.GetSyncMeta(store.MetaPinnedStartMs)
	pe, okE, errE := st.GetSyncMeta(store.MetaPinnedEndMs)
	if errS != nil || errE != nil || !okS || !okE {
		return start, end, false
	}
	pinnedStart, err1 := msToTime(ps)
	pinnedEnd, err2 := msToTime(pe)
	if err1 != nil || err2 != nil || !pinnedEnd.After(pinnedStart) {
		return start, end, false
	}
	return pinnedStart, pinnedEnd, true
}

// setExpandPin records the window as pinned, or clears any existing pin when the
// rebuild used the rolling default.
func setExpandPin(st *store.Store, pinned bool, start, end time.Time) error {
	if !pinned {
		return st.DeleteSyncMeta(store.MetaPinnedStartMs, store.MetaPinnedEndMs)
	}
	if err := st.SetSyncMeta(store.MetaPinnedStartMs, msString(start)); err != nil {
		return err
	}
	return st.SetSyncMeta(store.MetaPinnedEndMs, msString(end))
}

// addTruncation records a capped expansion in a command's stdout result. The cap
// is reported rather than applied silently: without it a series simply stops
// part-way through the requested window with nothing to distinguish that from
// the series genuinely ending.
func addTruncation(out map[string]any, res expandpkg.Result) {
	if len(res.Truncated) == 0 {
		return
	}
	out["truncated_events"] = len(res.Truncated)
	out["truncated_uids"] = truncatedSample(res.Truncated)
}

// truncatedSample caps the uid list so a pathological store cannot flood the
// result; the count in truncated_events remains exact.
func truncatedSample(uids []string) []string {
	const maxSample = 20
	if len(uids) <= maxSample {
		return uids
	}
	return uids[:maxSample]
}
