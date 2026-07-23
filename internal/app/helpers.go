package app

import (
	"context"
	"path/filepath"

	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
)

// pageInfo carries pagination metadata for a list result.
type pageInfo struct {
	Next    string
	HasMore bool
}

// storePath returns the SQLite store path for a config directory. The database
// lives alongside config.yaml so a single --config directory carries both.
func storePath(cfgDir string) string {
	return filepath.Join(cfgDir, constants.DatabaseFileName)
}

// cmdContext returns a context bounded by the resolved request timeout.
func cmdContext(s *appState) (context.Context, context.CancelFunc) {
	d := s.timeout()
	if d <= 0 {
		d = constants.DefaultTimeout
	}
	return context.WithTimeout(context.Background(), d)
}
