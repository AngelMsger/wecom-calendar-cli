// Reuse a verified login by recording its identity on another context of the
// same service. Secrets stay in their original store; public setup stays offline.
//
// The credential store is keyed by server host and scheme, so contexts on one
// server already share one stored CalDAV password. What reuse adds is the
// identity: it finds the WeCom email that stored password still authenticates
// and records it on a context that has none, so nobody issues a new password —
// which would invalidate the previous one — to complete a team preset.
package app

import (
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/angelmsger/wecom-calendar-cli/internal/auth"
	"github.com/angelmsger/wecom-calendar-cli/internal/config"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/spf13/cobra"
)

type reuseServices struct {
	Resolve func(config.Config) (auth.Credential, error)
	Verify  func(config.Config, auth.Credential) error
	Read    func(string) (config.File, bool, error)
	Write   func(string, config.File) error
}

type reuseResult struct {
	Context       string `json:"context"`
	SourceContext string `json:"source_context,omitempty"`
	State         string `json:"state"`
	Changed       bool   `json:"changed"`
	Verified      bool   `json:"verified"`
	DryRun        bool   `json:"dry_run"`
	Reason        string `json:"reason,omitempty"`
}

// noPasswordHint closes the reuse errors. None of them is a password problem,
// and a new CalDAV password would invalidate the one every client still uses.
const noPasswordHint = " Nothing was changed, and this is not a password problem: do not issue a new CalDAV password because of it."

func newAuthReuseCmd(s *appState) *cobra.Command {
	var dryRun bool
	var from string
	cmd := &cobra.Command{
		Use:   "reuse",
		Short: "Reuse an existing login in the selected context without signing in again",
		Long: "Fill in the selected context's missing WeCom email from another context on the\n" +
			"same service whose stored login still works, instead of asking for a CalDAV\n" +
			"password. Stored passwords are keyed by server host and scheme, so contexts on\n" +
			"one server already share one, and nothing is copied. Reuse matches stored\n" +
			"contexts by complete service URL and scheme, verifies the stored password with\n" +
			"a candidate's email through the calendar-home check auth login runs, and\n" +
			"records that email on the selected context. It never replaces an existing\n" +
			"email, reads credentials from environment variables, switches the scheme or\n" +
			"activates a context. --dry-run performs the same read-only verification and\n" +
			"reports the proposed association. An unchanged or unavailable result is not\n" +
			"proof of authentication; use auth status to check.",
		Example: "  wecom-calendar-cli --use-context team auth reuse --dry-run\n" +
			"  wecom-calendar-cli --use-context team auth reuse\n" +
			"  wecom-calendar-cli --use-context team auth reuse --from-context personal",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			services := reuseServices{
				Resolve: func(cfg config.Config) (auth.Credential, error) { return auth.Resolve(cfg, config.Secrets{}, s.store) },
				Verify:  func(cfg config.Config, cred auth.Credential) error { return verifyCredential(s, cfg, cred) },
				Read:    config.ReadFile, Write: config.WriteFile,
			}
			result, err := reuseAuthentication(s, from, dryRun, services)
			if err != nil {
				return err
			}
			return s.emit(result)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "verify and preview the identity association without writing")
	cmd.Flags().StringVar(&from, "from-context", "", "choose a matching source from config get-contexts when multiple identities are available")
	return cmd
}

func reuseAuthentication(s *appState, from string, dryRun bool, services reuseServices) (reuseResult, error) {
	result := reuseResult{Context: s.resolved.ActiveContext, State: "unavailable", DryRun: dryRun}
	file, _, err := services.Read(s.cfgDir)
	if err != nil {
		return result, cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_READ", "failed to read authentication contexts")
	}
	// The server URL has a default here, so a destination needs no stored URL;
	// it only has to exist. `config set-context <name>` is enough to create it.
	target, exists := file.Context(result.Context)
	if !exists {
		return result, cerrors.New(cerrors.CategoryConfig, "AUTH_REUSE_TARGET_MISSING", "prepare the destination context before reusing a login").
			WithHint("Reuse records an identity on a context stored in the config file. Create the context, select it with --use-context, then run auth reuse again." + noPasswordHint).
			WithNextSteps(constants.AppName + " config set-context <name>")
	}
	result.Context = target.Name
	selected := constants.AppName + " --use-context " + config.QuoteContextArg(target.Name)
	targetCfg := reuseContextConfig(target, file.Defaults)
	// basic is the only scheme. Refuse anything else here, before a source is
	// matched or a credential is read.
	if err := config.ValidateService(targetCfg); err != nil {
		return result, err
	}
	if !config.SameService(targetCfg, s.cfg()) {
		return result, cerrors.New(cerrors.CategoryConflict, "AUTH_REUSE_TARGET_MISMATCH", "service overrides differ from the stored destination; no identity was changed").
			WithHint("--base-url, --auth-scheme or their WECOM_CALENDAR_* variables select another service than the context stores. Drop the override, or select the context for that service." + noPasswordHint).
			WithNextSteps(selected + " config show --explain")
	}

	if from != "" {
		source, ok := file.Context(from)
		if !ok {
			return result, cerrors.New(cerrors.CategoryNotFound, "AUTH_REUSE_SOURCE_NOT_FOUND", "the selected source context does not exist").
				WithHint(config.UnknownContextHint(from, file.ContextNames()) + noPasswordHint).
				WithNextSteps(constants.AppName + " config get-contexts")
		}
		if !config.SameService(targetCfg, reuseContextConfig(source, file.Defaults)) {
			return result, cerrors.New(cerrors.CategoryConflict, "AUTH_REUSE_SOURCE_MISMATCH", "the source context belongs to a different service or authentication scope").
				WithHint("A login is reused only between contexts with the same complete server URL and scheme. Choose a source on the destination's server." + noPasswordHint).
				WithNextSteps(constants.AppName + " config get-contexts")
		}
	}
	// A populated destination belongs to the user. Reuse fills missing identity,
	// never silently changes which account a named context represents.
	if target.Auth.Username != "" {
		result.State = "unchanged"
		result.Reason = "destination identity is already configured"
		return result, nil
	}
	// Basic authentication needs the email, so only a context that has one can
	// be a source. The rest are dropped before any credential is read.
	candidates := []config.NamedContext{}
	for _, source := range config.ReuseSources(file, target.Name) {
		if from == "" || strings.EqualFold(source.Name, from) {
			candidates = append(candidates, source)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
	type match struct {
		source     config.NamedContext
		cfg        config.Config
		credential auth.Credential
	}
	matches := []match{}
	seen := map[string]bool{}
	for _, source := range candidates {
		cfg := reuseContextConfig(source, file.Defaults)
		credential, err := services.Resolve(cfg)
		if err != nil {
			if unavailableReuseCredential(err) {
				continue
			}
			return result, err
		}
		if err = services.Verify(cfg, credential); err != nil {
			if failure := cerrors.AsCLIError(err); failure.Category == cerrors.CategoryAuth && failure.HTTPStatus == 401 {
				continue
			}
			return result, err
		}
		// One stored password normally verifies one email. WeCom treats an
		// email case-insensitively, so two spellings are one identity.
		key := reuseCredentialKey(cfg) + "\x00" + config.NormalizeUsername(credential.Username)
		if seen[key] {
			continue
		}
		seen[key] = true
		matches = append(matches, match{source, cfg, credential})
	}
	if len(matches) == 0 {
		result.Reason = "no matching stored identity can be reused"
		return result, nil
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.source.Name
		}
		return result, cerrors.New(cerrors.CategoryConflict, "AUTH_REUSE_AMBIGUOUS", "multiple matching login identities are available; choose a source context").
			WithDetails(map[string]any{"contexts": names}).
			WithHint("More than one stored identity verified for this service. Name the context whose WeCom account the destination should use."+noPasswordHint).
			WithNextSteps(constants.AppName+" config get-contexts", selected+" auth reuse --from-context <name> --dry-run")
	}
	chosen := matches[0]
	updated := target
	updated.Auth.Username = chosen.credential.Username
	updated.Auth.Scheme = chosen.credential.Scheme
	// Preserve the native store key for equivalent URL spellings without copying
	// a secret. The normalized complete service URL has already been matched.
	if reuseCredentialKey(targetCfg) != reuseCredentialKey(chosen.cfg) {
		updated.BaseURL = chosen.cfg.BaseURL
	}
	result.SourceContext = chosen.source.Name
	result.Verified = true
	result.Changed = true
	result.State = "available"
	if dryRun {
		return result, nil
	}

	// A retry keeps an explicit source, so a resolved ambiguity stays resolved.
	again := selected + " auth reuse"
	if from != "" {
		again += " --from-context " + config.QuoteContextArg(chosen.source.Name)
	}
	current, _, err := services.Read(s.cfgDir)
	if err != nil {
		return result, cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_READ", "failed to recheck authentication contexts")
	}
	if !reflect.DeepEqual(file, current) {
		return result, cerrors.New(cerrors.CategoryConflict, "AUTH_REUSE_CONFIG_CHANGED", "configuration changed during verification; no identity was saved").
			WithHint("The config file was edited while the stored login was being verified. Preview again against the current file." + noPasswordHint).
			WithNextSteps(again + " --dry-run")
	}
	currentCredential, err := services.Resolve(chosen.cfg)
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(currentCredential, chosen.credential) {
		return result, cerrors.New(cerrors.CategoryConflict, "AUTH_REUSE_CREDENTIAL_CHANGED", "stored credential changed during verification; no identity was saved").
			WithHint("The stored password was replaced while it was being verified, for example by a login in another context on the same server. Preview again against the current one." + noPasswordHint).
			WithNextSteps(again + " --dry-run")
	}
	next := file
	next.Contexts = append([]config.NamedContext(nil), file.Contexts...)
	for i, c := range next.Contexts {
		if c.Name == target.Name {
			next.Contexts[i] = updated
			break
		}
	}
	if err = services.Write(s.cfgDir, next); err != nil {
		return result, cerrors.Wrap(err, cerrors.CategoryConfig, "AUTH_REUSE_WRITE_FAILED", "verified identity could not be saved; credentials were not changed").
			WithHint("The stored login verified, but the config file could not be written. Fix access to the config directory, then run auth reuse again; the stored password is untouched, so do not issue a new one.").
			WithNextSteps(again)
	}
	result.State = "reused"
	return result, nil
}

func reuseContextConfig(c config.NamedContext, defaults config.Defaults) config.Config {
	return config.StoredContext(c, defaults)
}

// reuseCredentialKey is the store account a context resolves in a fresh
// process: the host of its stored URL, or of the default endpoint, and the scheme.
func reuseCredentialKey(cfg config.Config) string {
	return auth.AccountKey(cfg.BaseURL, cfg.Auth.Scheme)
}

func unavailableReuseCredential(err error) bool {
	var ce *cerrors.CLIError
	if !errors.As(err, &ce) {
		return false
	}
	// Store visibility failures require host access, not a new login. Preserve
	// their structured recovery; only incomplete local identity is skippable.
	return ce.Code == "AUTH_NO_BASIC"
}
