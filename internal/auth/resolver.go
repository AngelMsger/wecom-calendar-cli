package auth

import (
	"errors"

	"github.com/angelmsger/wecom-calendar-cli/internal/config"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// Resolve produces a Credential from configuration. A secret supplied via
// flags/env/.env (carried in secrets) takes precedence; otherwise the secret
// is loaded from the Store. The returned credential is validated.
//
// An absent credential gains the acquisition guide as a next step. An
// inaccessible store never does: issuing a new CalDAV password invalidates the
// previous one, so a keychain a sandbox cannot read must not lead there.
//
// When another stored context on the same service already has a WeCom email
// (cfg.MayReuse), an absent credential is offered `auth reuse` ahead of every
// step that acquires one. Reuse reads the same store, so it never joins an
// inaccessible-store error either.
func Resolve(cfg config.Config, secrets config.Secrets, store *Store) (credResult Credential, resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = config.WithCredentialGuide(resultErr, cfg)
		}
	}()
	scheme := cfg.Auth.Scheme
	if scheme == "" {
		scheme = SchemeBasic
	}
	cred := Credential{Scheme: scheme, Username: cfg.Auth.Username}
	cred.Secret = secrets.Password

	if cred.Secret == "" && store != nil && cfg.BaseURL != "" {
		lookupBase, err := CredentialLookupURL(cfg)
		if err != nil {
			return Credential{}, err
		}
		loaded, err := store.Load(AccountKey(lookupBase, scheme))
		switch {
		case err == nil:
			cred.Secret = loaded
		case errors.Is(err, ErrSecretNotFound):
			return Credential{}, credentialNotVisibleOrMissingError(cfg)
		default:
			return Credential{}, credentialStoreInaccessibleError(err)
		}
	}

	if err := cred.Validate(); err != nil {
		return Credential{}, missingIdentityError(err, cfg)
	}
	return cred, nil
}

// credentialNotVisibleOrMissingError keeps the host retry first. The reuse
// step, when offered, sits between it and the step that leads to a password.
func credentialNotVisibleOrMissingError(cfg config.Config) error {
	steps := []string{
		"Retry the same command with access to the host user environment (home directory and OS keychain).",
		"wecom-calendar-cli doctor",
	}
	if cfg.MayReuse {
		steps = append(steps, config.ReuseStep)
	}
	steps = append(steps, "Only if the host retry also reports missing credentials, run `wecom-calendar-cli auth login` (or `config init`) in the user's terminal, or set WECOM_CALENDAR_* environment variables. Do not issue a new CalDAV password before then: it invalidates the previous one.")
	return cerrors.New(cerrors.CategoryConfig, "CREDENTIAL_NOT_VISIBLE_OR_MISSING",
		"stored WeCom Calendar credentials are missing or not visible in this execution environment").
		WithHint("An agent sandbox may have a different home or keychain view even when the host is already configured.").
		WithNextSteps(steps...).
		WithRecovery(hostCredentialRecovery())
}

// missingIdentityError puts `auth reuse` first when a context lacks only its
// WeCom email and another context on the same service has one. Contexts on one
// server share a stored password, so that is the usual state of a team preset
// beside a personal context, and it needs no password at all.
func missingIdentityError(err error, cfg config.Config) error {
	ce := cerrors.AsCLIError(err)
	if ce.Code != "AUTH_NO_BASIC" || !cfg.MayReuse {
		return err
	}
	return ce.WithHint("This context has no WeCom email, and another context on the same server has one. `auth reuse` verifies that stored login and records its email here; no CalDAV password is needed, and none should be issued.").
		WithNextSteps(append([]string{config.ReuseStep}, ce.NextSteps...)...)
}

func credentialStoreInaccessibleError(err error) error {
	return cerrors.Wrap(err, cerrors.CategoryConfig, "CREDENTIAL_STORE_INACCESSIBLE",
		"stored WeCom Calendar credentials cannot be read in this execution environment").
		WithHint("The configured credential store is inaccessible; this commonly happens when an agent sandbox cannot access the host keychain or credential file.").
		WithNextSteps(
			"Retry the same command with access to the host user environment (home directory and OS keychain).",
			"wecom-calendar-cli doctor",
			"Do not run `config init` or `auth login`, and do not issue a new CalDAV password, unless the same check also fails in the host environment.").
		WithRecovery(hostCredentialRecovery())
}

func hostCredentialRecovery() cerrors.Recovery {
	return cerrors.Recovery{
		Action:   "retry_current_command",
		Scope:    "host",
		Requires: []string{"user_home", "os_keychain"},
	}
}

// Save stores a credential's secret for later resolution and returns the
// backend ("keychain" or "file") that accepted it.
func Save(baseURL string, cred Credential, store *Store) (string, error) {
	if err := cred.Validate(); err != nil {
		return "", err
	}
	return store.Save(AccountKey(baseURL, cred.Scheme), cred.Secret)
}

// Forget removes any stored secret for the base URL and scheme.
func Forget(baseURL, scheme string, store *Store) error {
	return store.Delete(AccountKey(baseURL, scheme))
}

// CredentialLookupURL retains legacy store addressing only for the same complete service.
func CredentialLookupURL(cfg config.Config) (string, error) {
	if cfg.CredentialBaseURL == "" {
		return cfg.BaseURL, nil
	}
	target, e1 := config.NormalizeServiceURL(cfg.BaseURL)
	stored, e2 := config.NormalizeServiceURL(cfg.CredentialBaseURL)
	if e1 != nil || e2 != nil || target != stored {
		return "", cerrors.New(cerrors.CategoryConfig, "CREDENTIAL_SERVICE_MISMATCH", "stored credential lookup does not match the complete service URL").
			WithHint("The service URL in effect is not the one the stored credential belongs to. Select the context for the intended service; this is not a password problem.").
			WithNextSteps(constants.AppName+" config get-contexts", constants.AppName+" config show --explain")
	}
	return cfg.CredentialBaseURL, nil
}

// ForgetForConfig removes the same entry that configured requests resolve.
func ForgetForConfig(cfg config.Config, scheme string, store *Store) error {
	base, err := CredentialLookupURL(cfg)
	if err != nil {
		return err
	}
	return Forget(base, scheme, store)
}
