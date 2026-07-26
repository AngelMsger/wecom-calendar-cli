package app

import (
	"context"
	"time"

	expandpkg "github.com/angelmsger/wecom-calendar-cli/internal/expand"
	syncpkg "github.com/angelmsger/wecom-calendar-cli/internal/sync"
	"github.com/spf13/cobra"
)

func newSyncCmd(s *appState) *cobra.Command {
	var full, dryRun bool
	var calendarID, progressMode string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync WeCom calendars into the local store",
		Long: "Pull calendars and events over CalDAV into the local SQLite store.\n" +
			"Incremental by default (calendars whose change-tag is unchanged are\n" +
			"skipped); --full rescans everything. Idempotent: re-running is safe and\n" +
			"leaves the data unchanged. This never touches your event metadata.",
		Example: "  wecom-calendar-cli sync\n" +
			"  wecom-calendar-cli sync --full\n" +
			"  wecom-calendar-cli sync --calendar 1688853806313356 --dry-run",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A full pull makes many small requests; bound the whole run
			// generously (the per-request timeout still applies in transport).
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			client, err := s.newClient(ctx)
			if err != nil {
				return err
			}

			if dryRun {
				cals, err := client.ListCalendars(ctx)
				if err != nil {
					return err
				}
				// Read stored ctags read-only; a dry run must not create or migrate
				// the database. When no store exists yet, everything is "never synced".
				st, exists, err := s.openStoreForRead()
				if err != nil {
					return err
				}
				if exists {
					defer st.Close()
				}
				type dryRow struct {
					ID        string `json:"id"`
					Name      string `json:"name"`
					WouldScan bool   `json:"would_scan"`
					Reason    string `json:"reason"`
				}
				var rows []dryRow
				for _, c := range cals {
					if calendarID != "" && c.ID != calendarID {
						continue
					}
					var stored string
					var stateFound bool
					if exists && !full {
						var err error
						if stored, stateFound, err = st.CalendarState(c.ID); err != nil {
							return err
						}
					}
					would, reason := scanDecision(full, exists, stateFound, stored, c.Ctag)
					rows = append(rows, dryRow{ID: c.ID, Name: c.DisplayName, WouldScan: would, Reason: reason})
				}
				return s.emit(map[string]any{"dry_run": true, "calendars": rows})
			}

			st, err := s.openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			loc := displayLoc()
			progress, finishProgress := s.syncProgress(progressMode)
			res, err := syncpkg.Run(ctx, client, st, syncpkg.Options{
				Full:       full,
				CalendarID: calendarID,
				Since:      syncWindowStart(),
				Until:      syncWindowEnd(),
				Loc:        loc,
				Progress:   progress,
			})
			finishProgress()
			if err != nil {
				return err
			}
			// Rebuild the expanded/deduped instances so queries are ready. The
			// window is whatever `expand --since/--until` pinned, falling back to
			// the rolling default — rebuilding on the default unconditionally would
			// silently discard a window the user widened, which is exactly what the
			// coverage notice tells them to do about a short window.
			expandSince, expandUntil, pinned := resolveExpandWindow(st)
			rebuilt, err := expandpkg.Rebuild(st, expandpkg.Options{
				Since: expandSince, Until: expandUntil, Loc: loc,
			})
			if err != nil {
				return err
			}
			s.truncationNotice(rebuilt)
			out := map[string]any{
				"mode":                res.Mode,
				"calendars_total":     res.CalendarsTotal,
				"calendars_scanned":   res.CalendarsScanned,
				"resources_fetched":   res.ResourcesFetched,
				"events_upserted":     res.EventsUpserted,
				"events_soft_deleted": res.EventsSoftDeleted,
				"instances_rebuilt":   rebuilt.Instances,
				"covered_from":        expandSince.UTC().Format(time.RFC3339),
				"covered_to":          expandUntil.UTC().Format(time.RFC3339),
				"window_pinned":       pinned,
			}
			addTruncation(out, rebuilt)
			return s.emit(out)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&full, "full", false, "ignore change-tags and rescan every calendar")
	f.BoolVar(&dryRun, "dry-run", false, "report which calendars would be scanned, without writing")
	f.StringVar(&calendarID, "calendar", "", "sync only one calendar id")
	f.StringVar(&progressMode, "progress", "auto", "progress on stderr: auto (a live line on a terminal, bounded JSON notices otherwise), none, or json")
	enumComplete(cmd, "progress", "auto", "none", "json")
	return cmd
}

// scanDecision reports whether `sync` would scan a calendar, and why, mirroring
// the skip rule in internal/sync. It is separate from the command so the
// reasons — which are what a user reads when diagnosing a slow sync — can be
// tested directly.
//
// The no-change-tag case is called out explicitly. A server that returns an
// empty getctag gives us nothing to compare, so the calendar must be re-listed
// on every sync to notice changes at all; that is correct, but reporting it as
// "never synced" (which the store's empty stored ctag otherwise looks like) is
// wrong and actively misleading — the calendar may have synced many times.
func scanDecision(full, storeExists, stateFound bool, storedCtag, serverCtag string) (bool, string) {
	switch {
	case full:
		return true, "full rescan"
	case !storeExists:
		return true, "never synced"
	case serverCtag == "":
		return true, "server sends no change-tag, so this calendar is re-listed every sync"
	case !stateFound || storedCtag == "":
		return true, "never synced"
	case storedCtag == serverCtag:
		return false, "ctag unchanged"
	default:
		return true, "ctag changed"
	}
}

// syncWindowStart/End bound the CalDAV calendar-query used to enumerate events.
// Wide enough to capture the full personal history and a couple of years ahead.
func syncWindowStart() time.Time { return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC) }
func syncWindowEnd() time.Time   { return time.Now().AddDate(2, 0, 0) }
