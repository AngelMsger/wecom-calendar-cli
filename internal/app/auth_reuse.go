// Reuse a verified login by recording its identity on another context of the
// same service. agent-cli-kit/reuse decides; this file describes
// wecom-calendar-cli's contexts to it and supplies resolution, verification and
// persistence. Secrets stay in their original store; public setup stays offline.
//
// The credential store is keyed by server host and scheme, so contexts on one
// server already share one stored CalDAV password. What reuse adds is the
// identity: it finds the WeCom email that stored password still authenticates
// and records it on a context that has none, so nobody issues a new password —
// which would invalidate the previous one — to complete a team preset.
package app

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/angelmsger/agent-cli-kit/credstore"
	"github.com/angelmsger/agent-cli-kit/reuse"
	"github.com/angelmsger/wecom-calendar-cli/internal/auth"
	"github.com/angelmsger/wecom-calendar-cli/internal/config"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/spf13/cobra"
)

type reuseServices struct {
	Resolve func(config.Config) (auth.Credential, credstore.Lookup, error)
	Lookup  func(key string) credstore.Lookup
	Verify  func(context.Context, config.Config, auth.Credential) error
	Read    func(string) (config.File, bool, error)
	Write   func(string, config.File) error
}

type reuseOptions struct {
	From       string
	FromScheme string
	DryRun     bool
}

// noPasswordHint closes the reuse errors. None of them is a password problem,
// and a new CalDAV password would invalidate the one every client still uses.
const noPasswordHint = " Nothing was changed, and this is not a password problem: do not issue a new CalDAV password because of it."

func newAuthReuseCmd(s *appState) *cobra.Command {
	var opts reuseOptions
	cmd := &cobra.Command{
		Use:   "reuse",
		Short: "Reuse an existing login in the selected context without signing in again",
		Long: "Fill in the selected context's missing WeCom email from a login this machine\n" +
			"already has for the same service, instead of asking for a CalDAV password.\n" +
			"Stored passwords are keyed by server host and scheme, so contexts on one\n" +
			"server already share one, and nothing is copied. Reuse matches stored\n" +
			"contexts by complete service URL before it reads any credential, verifies\n" +
			"the stored password with a candidate's email through the calendar-home check\n" +
			"auth login runs, and records that email on the selected context. When no\n" +
			"other context's login verifies, it also tries the password stored for this\n" +
			"server with each email the server's contexts record. It never replaces an\n" +
			"existing email, reads credentials from environment variables or activates a\n" +
			"context. --from-context and --from-scheme select a source explicitly.\n" +
			"--dry-run performs the same read-only verification and reports the proposed\n" +
			"association. An unchanged or unavailable result is not proof of\n" +
			"authentication; run the check next_steps names (doctor).",
		Example: "  wecom-calendar-cli --use-context team auth reuse --dry-run\n" +
			"  wecom-calendar-cli --use-context team auth reuse\n" +
			"  wecom-calendar-cli --use-context team auth reuse --from-context personal",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := reuseAuthentication(s, opts, s.reuseServices())
			if err != nil {
				return err
			}
			return s.emit(result)
		},
	}
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "verify and preview the identity association without writing")
	cmd.Flags().StringVar(&opts.From, "from-context", "", "choose a matching source from config get-contexts when multiple identities are available")
	cmd.Flags().StringVar(&opts.FromScheme, "from-scheme", "", "use the password stored for this server under this scheme (basic), paired with each WeCom email recorded for the server, or with the --from-context one")
	return cmd
}

func (s *appState) reuseServices() reuseServices {
	return reuseServices{
		Resolve: func(cfg config.Config) (auth.Credential, credstore.Lookup, error) {
			return auth.ResolveLookup(cfg, config.Secrets{}, s.store)
		},
		Lookup: func(key string) credstore.Lookup { return s.store.Lookup(key) },
		Verify: func(ctx context.Context, cfg config.Config, cred auth.Credential) error {
			return verifyCredentialWithin(ctx, s, cfg, cred)
		},
		Read: config.ReadFile, Write: config.WriteFile,
	}
}

func reuseAuthentication(s *appState, opts reuseOptions, services reuseServices) (reuse.Result, error) {
	started := time.Now()
	result := reuse.Result{Context: s.resolved.ActiveContext, State: reuse.StateUnavailable, DryRun: opts.DryRun}
	file, _, err := services.Read(s.cfgDir)
	if err != nil {
		return result, cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_READ", "failed to read authentication contexts")
	}
	target, exists := file.Context(s.resolved.ActiveContext)
	targetCfg := config.StoredContext(target, file.Defaults)
	// basic is the only scheme. A destination stored with another one is a
	// configuration error, reported before a source is matched or a secret read.
	if exists {
		if err := config.ValidateService(targetCfg); err != nil {
			return result, err
		}
	}
	// A retry names the source under its stored spelling.
	if source, ok := file.Context(opts.From); ok {
		opts.From = source.Name
	}
	selected := constants.AppName + " --use-context " + config.QuoteContextArg(target.Name)
	again := reuseRetry(selected, opts)
	withCredential := func(spec reuse.Spec) config.Config {
		cfg := targetCfg
		cfg.BaseURL, cfg.Auth.Scheme, cfg.Auth.Username = spec.KeyBase, spec.Scheme, spec.Username
		return cfg
	}
	req := reuse.Request{
		Target:         s.resolved.ActiveContext,
		Contexts:       oneSpellingPerIdentity(config.ReuseContexts(file), target.Name, opts.From),
		Runtime:        reuse.Context{Service: config.ReuseService(s.cfg()), Scheme: s.cfg().Auth.Scheme},
		SchemeSupplied: config.SchemeSupplied(s.resolved.Sources),
		From:           opts.From,
		FromScheme:     opts.FromScheme,
		DryRun:         opts.DryRun,
		Start:          started,
		Timeout:        s.timeout(),
		Check:          "doctor",
		Key:            auth.AccountKey,
		Valid: func(scheme string) bool {
			cfg := targetCfg
			cfg.Auth.Scheme = scheme
			return config.ValidateService(cfg) == nil
		},
		SameUsername: config.SameUsername,
		Resolve: func(spec reuse.Spec) (auth.Credential, credstore.Lookup, error) {
			return services.Resolve(withCredential(spec))
		},
		Lookup: services.Lookup,
		Verify: func(ctx context.Context, spec reuse.Spec, cred auth.Credential) (reuse.Account, error) {
			// Every source matched the destination's complete service, so it is
			// verified against the destination's configuration. The calendar
			// home names no account, so none is reported.
			return reuse.Account{}, services.Verify(ctx, withCredential(spec), cred)
		},
		Persist: func(u reuse.Update) error {
			current, _, err := services.Read(s.cfgDir)
			if err != nil {
				return cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_READ", "failed to recheck authentication contexts")
			}
			if !reflect.DeepEqual(file, current) {
				return reuse.ErrConfigChanged
			}
			next := file
			next.Contexts = append([]config.NamedContext(nil), file.Contexts...)
			for i, c := range next.Contexts {
				if c.Name != target.Name {
					continue
				}
				c.Auth.Scheme = u.Scheme
				if u.SetUsername {
					c.Auth.Username = u.Username
				}
				if u.BaseURL != "" {
					c.BaseURL = u.BaseURL
				}
				next.Contexts[i] = c
			}
			if err := services.Write(s.cfgDir, next); err != nil {
				return cerrors.Wrap(err, cerrors.CategoryConfig, "AUTH_REUSE_WRITE_FAILED", "verified identity could not be saved; credentials were not changed").
					WithHint("The stored login verified, but the config file could not be written. Fix access to the config directory, then run auth reuse again; the stored password is untouched, so do not issue a new one.").
					WithNextSteps(again)
			}
			return nil
		},
	}
	result, err = reuse.Run(req)
	return result, withReuseHints(err, file, opts.From, selected, again)
}

// reuseRetry repeats an invocation, keeping an explicit source so a resolved
// ambiguity stays resolved.
func reuseRetry(selected string, opts reuseOptions) string {
	again := selected + " auth reuse"
	if opts.From != "" {
		again += " --from-context " + config.QuoteContextArg(opts.From)
	}
	if opts.FromScheme != "" {
		again += " --from-scheme " + opts.FromScheme
	}
	return again
}

// withReuseHints gives each reuse error this CLI's hint and, where the generic
// step does not fit, its steps. The category defaults in pkg/errors point at
// config init, auth login and sync, any of which is wrong for a selection or
// concurrency failure, and a new CalDAV password would invalidate the one
// every calendar client still uses.
func withReuseHints(err error, file config.File, from, selected, again string) error {
	var ce *cerrors.CLIError
	if !errors.As(err, &ce) {
		return err
	}
	switch ce.Code {
	case "AUTH_REUSE_TARGET_MISSING":
		// The server URL has a default, so creating the context is enough.
		ce.WithHint("Reuse records an identity on a context stored in the config file. Create the context, select it with --use-context, then run auth reuse again." + noPasswordHint).
			WithNextSteps(constants.AppName + " config set-context <name>")
	case "AUTH_REUSE_TARGET_MISMATCH":
		ce.WithHint("--base-url, --auth-scheme or their WECOM_CALENDAR_* variables select another service than the context stores. Drop the override, or select the context for that service." + noPasswordHint).
			WithNextSteps(selected + " config show --explain")
	case "AUTH_REUSE_SOURCE_NOT_FOUND":
		hint := "No WeCom email recorded for this server pairs with the stored password. Choose a context on this server that has one."
		if _, ok := file.Context(from); from != "" && !ok {
			hint = config.UnknownContextHint(from, file.ContextNames())
		}
		ce.WithHint(hint + noPasswordHint)
	case "AUTH_REUSE_SOURCE_MISMATCH":
		ce.WithHint("A login is reused only on the destination's own server, the same complete URL, and basic is the only scheme. Choose a source on that server." + noPasswordHint)
	case "AUTH_REUSE_AMBIGUOUS":
		ce.WithHint("More than one stored identity verified for this service. Name the context whose WeCom account the destination should use." + noPasswordHint)
	case "AUTH_REUSE_CONFIG_CHANGED":
		ce.WithHint("The config file was edited while the stored login was being verified. Preview again against the current file." + noPasswordHint).
			WithNextSteps(again + " --dry-run")
	case "AUTH_REUSE_CREDENTIAL_CHANGED":
		ce.WithHint("The stored password was replaced while it was being verified, for example by a login in another context on the same server. Preview again against the current one." + noPasswordHint).
			WithNextSteps(again + " --dry-run")
	}
	return err
}

// oneSpellingPerIdentity keeps one context per WeCom email and stored key.
// WeCom treats an email case-insensitively, so two spellings of one address
// under one stored password are one identity, while agent-cli-kit/reuse
// compares usernames exactly when it counts logins and would offer the two as
// a choice. The first context by name stands for the others; the destination
// and an explicitly selected source are always kept.
func oneSpellingPerIdentity(contexts []reuse.Context, keep ...string) []reuse.Context {
	identity := func(c reuse.Context) string {
		if c.Service == "" || c.Username == "" {
			return ""
		}
		return c.Service + "\x00" + auth.AccountKey(c.KeyBase, c.Scheme) + "\x00" + config.NormalizeUsername(c.Username)
	}
	kept := func(name string) bool {
		for _, k := range keep {
			if k != "" && strings.EqualFold(k, name) {
				return true
			}
		}
		return false
	}
	byName := append([]reuse.Context(nil), contexts...)
	sort.SliceStable(byName, func(i, j int) bool { return byName[i].Name < byName[j].Name })
	owner := map[string]string{}
	for _, c := range byName {
		if id := identity(c); id != "" && kept(c.Name) {
			owner[id] = c.Name
		}
	}
	for _, c := range byName {
		if id := identity(c); id != "" && owner[id] == "" {
			owner[id] = c.Name
		}
	}
	out := make([]reuse.Context, 0, len(contexts))
	for _, c := range contexts {
		if id := identity(c); id == "" || kept(c.Name) || owner[id] == c.Name {
			out = append(out, c)
		}
	}
	return out
}

// withReuseRecovery leads a rejected-credential error (HTTP 401) with
// `auth reuse` when the configuration file shows a stored login could replace
// the rejected one. A context that could reuse has no WeCom email of its own,
// so its requests stop at AUTH_NO_BASIC before reaching the server; this keeps
// the family rule for any 401 that does arrive.
func withReuseRecovery(ce *cerrors.CLIError, s *appState, cmd *cobra.Command) {
	if ce == nil || ce.Category != cerrors.CategoryAuth || ce.HTTPStatus != 401 || s == nil || s.resolved == nil || cmd == nil {
		return
	}
	if cmd.Name() == "reuse" || cmd.Name() == "login" {
		return
	}
	if step := s.cfg().ReuseStep; step != "" {
		ce.NextSteps = append([]string{step}, ce.NextSteps...)
	}
}
