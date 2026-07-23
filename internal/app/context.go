package app

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/auth"
	"github.com/angelmsger/wecom-calendar-cli/internal/config"
	"github.com/angelmsger/wecom-calendar-cli/internal/output"
	"github.com/angelmsger/wecom-calendar-cli/pkg/caldav"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// globalFlags holds the persistent flags shared by every command.
type globalFlags struct {
	baseURL    string
	format     string
	fields     string
	timeout    string
	configPath string
	useContext string
	verbose    bool
	// pretty opts a human user into TUI prompts (in `config init`) and
	// ANSI-colored JSON. Off by default so Agent / scripted / pipe usage stays
	// byte-identical.
	pretty bool
	// allowWrites overrides read-only mode for the current invocation. The
	// posture itself is set via config (defaults.read_only) or env
	// (WECOM_CALENDAR_CLI_READ_ONLY); this flag is the per-call escape hatch.
	allowWrites bool
}

// appState is the shared runtime context, built once in the root command's
// PersistentPreRunE and captured by every subcommand handler.
type appState struct {
	gflags   globalFlags
	resolved *config.Resolved
	store    *auth.Store
	cfgDir   string
}

// load resolves configuration from all sources using the current global flags.
func (s *appState) load() error {
	cfgDir := s.gflags.configPath
	if cfgDir == "" {
		d, err := config.ResolveConfigDir()
		if err != nil {
			return cerrors.Wrap(err, cerrors.CategoryConfig, "NO_HOME",
				"could not determine the home directory")
		}
		cfgDir = d
	}
	resolved, err := config.Load(config.LoadOptions{
		ConfigDir: cfgDir,
		Context:   s.gflags.useContext,
		Flags: config.FlagValues{
			BaseURL: s.gflags.baseURL,
			Format:  s.gflags.format,
			Timeout: s.gflags.timeout,
		},
	})
	if err != nil {
		// Pass structured CLI errors (e.g. UNKNOWN_CONTEXT) through untouched so
		// their specific hint survives; only opaque file/dotenv errors get the
		// generic wrapper.
		var ce *cerrors.CLIError
		if errors.As(err, &ce) {
			return ce
		}
		return cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_LOAD",
			"failed to load configuration")
	}
	s.resolved = resolved
	s.cfgDir = cfgDir
	s.store = auth.NewStore(cfgDir)
	return nil
}

// cfg returns the resolved config.
func (s *appState) cfg() config.Config { return s.resolved.Config }

// dbPath returns the SQLite store path, kept alongside the config file.
func (s *appState) dbPath() string { return storePath(s.cfgDir) }

// newClient resolves credentials and builds an authenticated CalDAV client.
func (s *appState) newClient(_ context.Context) (caldav.Client, error) {
	cfg := s.cfg()
	cred, err := auth.Resolve(cfg, s.resolved.Secrets, s.store)
	if err != nil {
		return nil, err
	}
	var verbose io.Writer
	if s.gflags.verbose {
		verbose = os.Stderr
	}
	return caldav.Build(caldav.BuildParams{
		BaseURL:       cfg.BaseURL,
		AuthDecorator: cred.Decorator(),
		Timeout:       cfg.Defaults.Timeout,
		MaxRetries:    cfg.Defaults.MaxRetries,
		Verbose:       verbose,
	})
}

// readOnly reports whether the effective posture for this invocation is
// read-only. Set via config (defaults.read_only) or WECOM_CALENDAR_CLI_READ_ONLY;
// --allow-writes flips it back to read-write for the current call.
func (s *appState) readOnly() bool {
	return s.cfg().Defaults.ReadOnly && !s.gflags.allowWrites
}

// emit writes a successful result to stdout in the configured format.
func (s *appState) emit(v any) error {
	return output.Emit(v, output.Options{
		Format: s.cfg().Defaults.Format,
		Fields: s.fieldList(),
		Writer: os.Stdout,
		Pretty: s.gflags.pretty,
	})
}

// emitList writes a paginated list result to stdout as a {items, next,
// has_more} envelope in the configured format.
func (s *appState) emitList(items any, info pageInfo) error {
	return output.EmitList(items, info.Next, info.HasMore, output.Options{
		Format: s.cfg().Defaults.Format,
		Fields: s.fieldList(),
		Writer: os.Stdout,
		Pretty: s.gflags.pretty,
	})
}

// fieldList splits the --fields flag into dot paths.
func (s *appState) fieldList() []string {
	if s.gflags.fields == "" {
		return nil
	}
	parts := strings.Split(s.gflags.fields, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// timeout returns the resolved request timeout.
func (s *appState) timeout() time.Duration { return s.cfg().Defaults.Timeout }
