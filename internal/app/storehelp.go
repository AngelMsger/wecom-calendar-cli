package app

import (
	"os"
	"time"

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
