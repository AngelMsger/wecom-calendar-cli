package auth

import (
	kit "github.com/angelmsger/agent-cli-kit/auth"
	"github.com/angelmsger/wecom-calendar-cli/internal/config"
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
func Resolve(cfg config.Config, secrets config.Secrets, store *Store) (Credential, error) {
	// The password is the secret of whichever scheme is configured, so an
	// unknown scheme still reports AUTH_BAD_SCHEME rather than a missing secret.
	supplied := map[string]string{SchemeBasic: secrets.Password, cfg.Auth.Scheme: secrets.Password}
	var missing []string
	if cfg.MayReuse {
		// The host retry stays first; reuse sits between it and the step that
		// leads to a password.
		missing = []string{config.ReuseStep, missingStep}
	}
	cred, err := kit.Resolve(kit.Request{
		Scheme:       cfg.Auth.Scheme,
		Username:     cfg.Auth.Username,
		Secrets:      supplied,
		BaseURL:      cfg.BaseURL,
		LookupURL:    func() (string, error) { return CredentialLookupURL(cfg) },
		MissingSteps: missing,
	}, store)
	if err != nil {
		return Credential{}, config.WithCredentialGuide(missingIdentityError(err, cfg), cfg)
	}
	return cred, nil
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

// Save stores a credential's secret for later resolution and returns the
// backend ("keychain" or "file") that accepted it.
func Save(baseURL string, cred Credential, store *Store) (string, error) {
	return kit.Save(baseURL, cred, store)
}

// Forget removes any stored secret for the base URL and scheme.
func Forget(baseURL, scheme string, store *Store) error {
	return kit.Forget(baseURL, scheme, store)
}

// CredentialLookupURL retains legacy store addressing only for the same complete service.
func CredentialLookupURL(cfg config.Config) (string, error) {
	if cfg.CredentialBaseURL == "" {
		return cfg.BaseURL, nil
	}
	target, e1 := config.NormalizeServiceURL(cfg.BaseURL)
	stored, e2 := config.NormalizeServiceURL(cfg.CredentialBaseURL)
	if e1 != nil || e2 != nil || target != stored {
		return "", kit.ServiceMismatchError()
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
