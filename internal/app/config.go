package app

import (
	"context"
	"fmt"
	"os"

	"github.com/angelmsger/wecom-calendar-cli/internal/auth"
	"github.com/angelmsger/wecom-calendar-cli/internal/config"
	"github.com/angelmsger/wecom-calendar-cli/pkg/caldav"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/spf13/cobra"
)

// contextRow is the result shape for `config get-contexts`.
type contextRow struct {
	Current    bool   `json:"current"`
	Name       string `json:"name"`
	Server     string `json:"server,omitempty"`
	AuthScheme string `json:"auth_scheme,omitempty"`
}

// configInitOutput is the result shape emitted by `config init`.
type configInitOutput struct {
	ConfigFile string              `json:"config_file"`
	Contexts   []initContextResult `json:"contexts"`
	NextSteps  []string            `json:"next_steps"`
}

type initContextResult struct {
	Name              string `json:"name"`
	CredentialBackend string `json:"credential_backend"`
}

func newConfigCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage wecom-calendar-cli configuration",
	}
	cmd.AddCommand(
		newConfigInitCmd(s), newConfigShowCmd(s), newConfigPathCmd(s),
		newConfigGetContextsCmd(s), newConfigUseContextCmd(s), newConfigDeleteContextCmd(s),
	)
	return cmd
}

func newConfigInitCmd(s *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactively set up the CalDAV server URL and credentials",
		Long: "Run the interactive setup wizard. It collects the CalDAV server URL,\n" +
			"your WeCom email and app-specific password, validates them and stores\n" +
			"the secret in the OS keychain. It can also configure additional named\n" +
			"contexts for working with several accounts.",
		Example: "  wecom-calendar-cli config init --pretty   # interactive TUI (recommended)\n" +
			"  wecom-calendar-cli config init             # plain line-by-line wizard (scripts, non-TTY)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			existing, _, err := config.ReadFile(s.cfgDir)
			if err != nil {
				return cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_READ",
					"failed to read the config file")
			}
			inputs := config.WizardInputs{
				Existing:   &existing,
				LoadSecret: loadExistingSecret(s.store),
			}
			result, err := runWizard(s, wizardHooks(s), inputs)
			if err != nil {
				if _, ok := err.(*cerrors.CLIError); ok {
					return err
				}
				return cerrors.Wrap(err, cerrors.CategoryConfig, "INIT_ABORTED", err.Error())
			}
			out, err := persistInitResult(s, result, existing)
			if err != nil {
				return err
			}
			return s.emit(out)
		},
	}
}

func newConfigShowCmd(s *appState) *cobra.Command {
	var explain bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the resolved configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := s.cfg()
			view := map[string]any{
				"server":      cfg.BaseURL,
				"auth.scheme": cfg.Auth.Scheme,
				"auth.user":   cfg.Auth.Username,
				"format":      cfg.Defaults.Format,
				"timeout":     cfg.Defaults.Timeout.String(),
				"database":    storePath(s.cfgDir),
			}
			if explain {
				src := s.resolved.Sources
				view["server"] = explained(cfg.BaseURL, src, config.FieldServer)
				view["auth.scheme"] = explained(cfg.Auth.Scheme, src, config.FieldAuthScheme)
				view["auth.user"] = explained(cfg.Auth.Username, src, config.FieldAuthUser)
				view["format"] = explained(cfg.Defaults.Format, src, config.FieldFormat)
			}
			if len(s.resolved.ContextNames) > 1 {
				view["context"] = s.resolved.ActiveContext
			}
			return s.emit(view)
		},
	}
	cmd.Flags().BoolVar(&explain, "explain", false, "annotate each value with its source")
	return cmd
}

func explained(value string, sources map[string]string, field string) string {
	return fmt.Sprintf("%s (from %s)", value, config.ExplainField(sources, field))
}

func newConfigPathCmd(s *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return s.emit(map[string]any{
				"path":     config.ConfigFilePath(s.cfgDir),
				"database": storePath(s.cfgDir),
			})
		},
	}
}

// credentialFrom builds a Credential from a config + secrets pair.
func credentialFrom(cfg config.Config, secrets config.Secrets) auth.Credential {
	return credentialOf(cfg.Auth, secrets)
}

// credentialFromContext builds a Credential for a single named context.
func credentialFromContext(nc config.NamedContext, secrets config.Secrets) auth.Credential {
	return credentialOf(nc.Auth, secrets)
}

// credentialOf builds a Credential from auth settings and transient secrets.
// There is a single scheme (basic); the secret is the app-specific password.
func credentialOf(ac config.AuthConfig, secrets config.Secrets) auth.Credential {
	scheme := ac.Scheme
	if scheme == "" {
		scheme = auth.SchemeBasic
	}
	return auth.Credential{Scheme: scheme, Username: ac.Username, Secret: secrets.Password}
}

// readConfigFile loads the config file for the context subcommands, mapping a
// missing file to a clear error.
func readConfigFile(s *appState) (config.File, error) {
	file, exists, err := config.ReadFile(s.cfgDir)
	if err != nil {
		return config.File{}, cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_READ",
			"failed to read the config file")
	}
	if !exists || len(file.Contexts) == 0 {
		return config.File{}, cerrors.New(cerrors.CategoryConfig, "NO_CONFIG",
			"no configured contexts").
			WithHint("Run `wecom-calendar-cli config init` to create one.")
	}
	return file, nil
}

func newConfigGetContextsCmd(s *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "get-contexts",
		Short: "List the configured contexts",
		Long: "List every context in the config file. The current context — the one\n" +
			"used when --use-context is not given — is marked.",
		Example: "  wecom-calendar-cli config get-contexts\n" +
			"  wecom-calendar-cli config get-contexts --format table",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			file, _, err := config.ReadFile(s.cfgDir)
			if err != nil {
				return cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_READ",
					"failed to read the config file")
			}
			rows := make([]contextRow, 0, len(file.Contexts))
			for _, c := range file.Contexts {
				rows = append(rows, contextRow{
					Current:    c.Name == file.CurrentContext,
					Name:       c.Name,
					Server:     c.BaseURL,
					AuthScheme: c.Auth.Scheme,
				})
			}
			return s.emit(rows)
		},
	}
}

// unknownContextHintFor wraps the config-package hint helper for use by
// app-level commands.
func unknownContextHintFor(file config.File, name string) string {
	return config.UnknownContextHint(name, file.ContextNames())
}

func newConfigUseContextCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "use-context <name>",
		Short:   "Switch the current context",
		Long:    "Set the current context — the account used by default. Override it for\na single command with the global --use-context flag instead.",
		Example: "  wecom-calendar-cli config use-context personal",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			file, err := readConfigFile(s)
			if err != nil {
				return err
			}
			target, ok := file.Context(name)
			if !ok {
				return cerrors.Newf(cerrors.CategoryConfig, "UNKNOWN_CONTEXT",
					"context %q is not defined", name).
					WithHint(unknownContextHintFor(file, name))
			}
			file.CurrentContext = target.Name
			if err := config.WriteFile(s.cfgDir, file); err != nil {
				return cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_WRITE",
					"failed to write the config file")
			}
			return s.emit(map[string]any{"context": target.Name, "status": "current"})
		},
	}
	cmd.ValidArgsFunction = completeContextNames(s)
	return cmd
}

func newConfigDeleteContextCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete-context <name>",
		Short:   "Delete a context and its stored credential",
		Example: "  wecom-calendar-cli config delete-context personal",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			file, err := readConfigFile(s)
			if err != nil {
				return err
			}
			target, ok := file.Context(name)
			if !ok {
				return cerrors.Newf(cerrors.CategoryConfig, "UNKNOWN_CONTEXT",
					"context %q is not defined", name).
					WithHint(unknownContextHintFor(file, name))
			}
			if len(file.Contexts) == 1 {
				return cerrors.New(cerrors.CategoryUsage, "LAST_CONTEXT",
					"cannot delete the only context")
			}
			scheme := target.Auth.Scheme
			if scheme == "" {
				scheme = auth.SchemeBasic
			}
			_ = auth.Forget(target.BaseURL, scheme, s.store)

			kept := file.Contexts[:0]
			for _, c := range file.Contexts {
				if c.Name != target.Name {
					kept = append(kept, c)
				}
			}
			file.Contexts = kept
			if file.CurrentContext == target.Name {
				file.CurrentContext = file.Contexts[0].Name
			}
			if err := config.WriteFile(s.cfgDir, file); err != nil {
				return cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_WRITE",
					"failed to write the config file")
			}
			return s.emit(map[string]any{"context": target.Name, "status": "deleted"})
		},
	}
	cmd.ValidArgsFunction = completeContextNames(s)
	return cmd
}

// persistInitResult is the post-wizard persistence pipeline. The ordering — 1
// validate, 2 save credentials, 3 write config, 4 best-effort orphan cleanup —
// ensures a failure in any single step never leaves a previously working config
// unusable; the worst case is a harmless orphan secret in the keychain.
func persistInitResult(s *appState, result *config.WizardResult, existing config.File) (configInitOutput, error) {
	for _, cr := range result.Creds {
		if cr.Context.Name == "" {
			return configInitOutput{}, cerrors.New(cerrors.CategoryConfig, "CTX_NAME_EMPTY",
				"refusing to persist a context with an empty name").
				WithHint("Re-run `wecom-calendar-cli config init` and provide a name when prompted.")
		}
		cred := credentialFromContext(cr.Context, cr.Secrets)
		if cerr := cred.Validate(); cerr != nil {
			return configInitOutput{}, cerrors.Wrap(cerr, cerrors.CategoryConfig, "CRED_INVALID",
				fmt.Sprintf("context %q has no usable credential", cr.Context.Name))
		}
	}

	type orphan struct {
		baseURL string
		scheme  string
	}
	var orphans []orphan
	out := configInitOutput{
		ConfigFile: config.ConfigFilePath(s.cfgDir),
		NextSteps:  config.SuggestedNextSteps(),
	}
	for _, cr := range result.Creds {
		if prev, ok := existing.Context(cr.Context.Name); ok {
			if prev.BaseURL != cr.Context.BaseURL || prev.Auth.Scheme != cr.Context.Auth.Scheme {
				orphans = append(orphans, orphan{baseURL: prev.BaseURL, scheme: prev.Auth.Scheme})
			}
		}
		cred := credentialFromContext(cr.Context, cr.Secrets)
		backend, err := auth.Save(cr.Context.BaseURL, cred, s.store)
		if err != nil {
			return configInitOutput{}, err
		}
		out.Contexts = append(out.Contexts, initContextResult{
			Name:              cr.Context.Name,
			CredentialBackend: fmt.Sprint(backend),
		})
	}

	if err := config.WriteFile(s.cfgDir, result.File); err != nil {
		return configInitOutput{}, cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_WRITE",
			"failed to write the config file")
	}

	for _, o := range orphans {
		_ = auth.Forget(o.baseURL, o.scheme, s.store)
	}
	return out, nil
}

// runWizard dispatches to the right wizard implementation based on --pretty.
// The huh-driven TUI is opt-in and requires an interactive stdin; otherwise the
// plain line-by-line path runs. Both return the same WizardResult shape.
func runWizard(s *appState, hooks config.WizardHooks, inputs config.WizardInputs) (*config.WizardResult, error) {
	if s.gflags.pretty {
		if !stdinIsTTY() {
			return nil, cerrors.New(cerrors.CategoryUsage, "PRETTY_NEEDS_TTY",
				"--pretty requires an interactive terminal for `config init`").
				WithHint("Drop --pretty or run from a terminal.")
		}
		return config.RunWizardHuh(hooks, inputs)
	}
	return config.RunWizard(config.NewPlainDriver(os.Stdin, os.Stderr), hooks, inputs)
}

// loadExistingSecret returns a WizardInputs.LoadSecret hook that reads the
// secret currently stored for nc into Secrets.Password.
func loadExistingSecret(store *auth.Store) func(config.NamedContext) (config.Secrets, bool) {
	return func(nc config.NamedContext) (config.Secrets, bool) {
		if store == nil || nc.BaseURL == "" || nc.Auth.Scheme == "" {
			return config.Secrets{}, false
		}
		secret, err := store.Load(auth.AccountKey(nc.BaseURL, nc.Auth.Scheme))
		if err != nil || secret == "" {
			return config.Secrets{}, false
		}
		return config.Secrets{Password: secret}, true
	}
}

// wizardHooks builds the credential-validation callback for `config init`. It
// builds a CalDAV client and Pings the calendar-home: this server requires auth
// even for a PROPFIND, so a successful Ping proves the credentials work.
func wizardHooks(s *appState) config.WizardHooks {
	return config.WizardHooks{
		Validate: func(cfg config.Config, secrets config.Secrets) error {
			ctx, cancel := context.WithTimeout(context.Background(), s.timeout())
			defer cancel()
			cred := credentialFrom(cfg, secrets)
			if err := cred.Validate(); err != nil {
				return err
			}
			client, err := caldav.Build(caldav.BuildParams{
				BaseURL:       cfg.BaseURL,
				AuthDecorator: cred.Decorator(),
				Timeout:       cfg.Defaults.Timeout,
				MaxRetries:    cfg.Defaults.MaxRetries,
			})
			if err != nil {
				return err
			}
			return client.Ping(ctx)
		},
	}
}
