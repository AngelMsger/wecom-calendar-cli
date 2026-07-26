package app

import (
	"os"
	"strconv"
	"time"

	expandpkg "github.com/angelmsger/wecom-calendar-cli/internal/expand"
	"github.com/angelmsger/wecom-calendar-cli/internal/output"
	"github.com/angelmsger/wecom-calendar-cli/internal/store"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// openStore opens the local SQLite store, creating the config directory if
// needed. Callers must Close the returned store.
func (s *appState) openStore() (*store.Store, error) {
	if err := os.MkdirAll(s.cfgDir, 0o700); err != nil {
		return nil, cerrors.Wrap(err, cerrors.CategoryConfig, "MKDIR",
			"could not create the config/data directory")
	}
	st, err := store.Open(s.dbPath())
	if err != nil {
		return nil, cerrors.Wrap(err, cerrors.CategoryInternal, "STORE_OPEN",
			"could not open the local store")
	}
	return st, nil
}

// openStoreForRead opens the store read-only for previews (--dry-run), without
// creating or migrating anything on disk. It returns exists=false when no
// database is present yet, so callers preview against empty state instead of
// materializing a file. Callers must Close the returned store when exists.
func (s *appState) openStoreForRead() (st *store.Store, exists bool, err error) {
	if _, statErr := os.Stat(s.dbPath()); statErr != nil {
		return nil, false, nil // absent (or unreadable): treat as empty, write nothing
	}
	st, err = store.OpenReadOnly(s.dbPath())
	if err != nil {
		return nil, false, cerrors.Wrap(err, cerrors.CategoryInternal, "STORE_OPEN",
			"could not open the local store read-only")
	}
	return st, true, nil
}

// staleNotice emits a stderr notice when the local store has never been synced
// or the last successful sync is older than the staleness threshold. It never
// touches stdout, so the data contract stays byte-stable.
func (s *appState) staleNotice(st *store.Store) {
	ms, ok, err := st.LastSuccessfulSyncMs()
	if err != nil {
		return
	}
	lastISO := ""
	stale := true
	if ok {
		last := time.UnixMilli(ms).UTC()
		lastISO = last.Format(time.RFC3339)
		stale = time.Since(last) > constants.StaleAfter
	}
	if !stale {
		return
	}
	output.EmitNotice(os.Stderr, map[string]any{
		"_notice": map[string]any{
			"stale": map[string]any{
				"last_sync_at": lastISO,
				"message":      "local data may be stale; run `wecom-calendar-cli sync`",
			},
		},
	})
}

// msToTime parses a stored epoch-millisecond string back into a time.
func msToTime(v string) (time.Time, error) {
	ms, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(ms).UTC(), nil
}

// msString renders a time as the epoch-millisecond string the metadata table
// stores.
func msString(t time.Time) string {
	return strconv.FormatInt(t.UTC().UnixMilli(), 10)
}

// truncationNotice emits a stderr notice when a rebuild capped some event's
// expansion, so the cap is visible to an agent that only watches `_notice`
// lines. The exact counts also travel on stdout (see addTruncation).
func (s *appState) truncationNotice(res expandpkg.Result) {
	if len(res.Truncated) == 0 {
		return
	}
	output.EmitNotice(os.Stderr, map[string]any{
		"_notice": map[string]any{
			"expansion_truncated": map[string]any{
				"events":       len(res.Truncated),
				"limit":        expandpkg.MaxInstancesPerEvent,
				"sample_uids":  truncatedSample(res.Truncated),
				"message":      "some recurring events hit the per-event occurrence limit; their later occurrences are missing from this window",
				"how_to_avoid": "narrow the expansion window with `wecom-calendar-cli expand --since <date> --until <date>`",
			},
		},
	})
}

// coverageNotice emits a stderr notice when a query window reaches beyond the
// window the derived instances were expanded over. Recurring occurrences past
// the expansion window are absent, so without this an out-of-range query would
// look empty rather than under-covered. It never touches stdout.
func (s *appState) coverageNotice(st *store.Store, since, until time.Time) {
	startStr, ok1, err1 := st.GetSyncMeta(store.MetaCoveredStartMs)
	endStr, ok2, err2 := st.GetSyncMeta(store.MetaCoveredEndMs)
	if err1 != nil || err2 != nil || !ok1 || !ok2 {
		return
	}
	coveredStart, e1 := strconv.ParseInt(startStr, 10, 64)
	coveredEnd, e2 := strconv.ParseInt(endStr, 10, 64)
	if e1 != nil || e2 != nil {
		return
	}
	qStart := since.UTC().UnixMilli()
	qEnd := until.UTC().UnixMilli()
	if qStart >= coveredStart && qEnd <= coveredEnd {
		return // fully within the expanded window
	}
	output.EmitNotice(os.Stderr, map[string]any{
		"_notice": map[string]any{
			"partial_coverage": map[string]any{
				"covered_from": time.UnixMilli(coveredStart).UTC().Format(time.RFC3339),
				"covered_to":   time.UnixMilli(coveredEnd).UTC().Format(time.RFC3339),
				"message": "query window extends beyond expanded coverage; recurring events outside it are omitted. " +
					"Widen it with `wecom-calendar-cli expand --since <date> --until <date>`.",
			},
		},
	})
}
