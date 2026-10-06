package auth

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/angelmsger/wecom-calendar-cli/internal/config"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/zalando/go-keyring"
)

// fakeKeyring stands in for the OS keychain so these tests never reach it.
type fakeKeyring struct {
	err     error
	secrets map[string]string
}

func (f fakeKeyring) Get(_, account string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if secret, ok := f.secrets[account]; ok {
		return secret, nil
	}
	return "", keyring.ErrNotFound
}

func (f fakeKeyring) Set(_, account, secret string) error {
	if f.err != nil {
		return f.err
	}
	f.secrets[account] = secret
	return nil
}

func (f fakeKeyring) Delete(string, string) error { return f.err }

func resolveConfig() config.Config {
	return config.Config{BaseURL: "https://service.example.test/deploy", Auth: config.AuthConfig{Scheme: SchemeBasic, Username: "member@example.test", CredentialURL: "https://help.example.test/caldav"}}
}

func joined(steps []string) string { return strings.Join(steps, "\n") }

// A credential that is absent may be acquired, so the guide joins the recovery
// — after the host retry, which stays first.
func TestMissingCredentialPointsAtTheGuideAfterTheHostRetry(t *testing.T) {
	store := newStoreWithKeyring(t.TempDir(), fakeKeyring{secrets: map[string]string{}})
	_, err := Resolve(resolveConfig(), config.Secrets{}, store)
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "CREDENTIAL_NOT_VISIBLE_OR_MISSING" || ce.Recovery == nil || ce.Recovery.Scope != "host" {
		t.Fatalf("unexpected error: %+v", ce)
	}
	if len(ce.NextSteps) != 5 || !strings.Contains(ce.NextSteps[0], "host user environment") {
		t.Fatalf("host retry must come first: %v", ce.NextSteps)
	}
	if !strings.Contains(ce.NextSteps[2], "Only if the host retry also reports missing credentials") || !strings.Contains(ce.NextSteps[2], "Do not issue a new CalDAV password before then") {
		t.Fatalf("acquisition must stay behind the host retry: %q", ce.NextSteps[2])
	}
	if ce.NextSteps[3] != "wecom-calendar-cli auth guide" || ce.NextSteps[4] != "Credential page: https://help.example.test/caldav" {
		t.Fatalf("guide was not appended: %v", ce.NextSteps)
	}
}

// A store that cannot be read is recovered on the host. Sending the caller to
// credential acquisition would make them issue a new CalDAV password, which
// invalidates the one every other calendar client still uses.
func TestInaccessibleStoreNeverSuggestsAcquiringACredential(t *testing.T) {
	store := newStoreWithKeyring(t.TempDir(), fakeKeyring{err: errors.New("keychain is locked")})
	_, err := Resolve(resolveConfig(), config.Secrets{}, store)
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "CREDENTIAL_STORE_INACCESSIBLE" || ce.Recovery == nil || ce.Recovery.Scope != "host" {
		t.Fatalf("unexpected error: %+v", ce)
	}
	steps := joined(ce.NextSteps)
	for _, forbidden := range []string{"auth guide", "Credential page"} {
		if strings.Contains(steps, forbidden) {
			t.Fatalf("inaccessible-store recovery mentions %q:\n%s", forbidden, steps)
		}
	}
	if len(ce.NextSteps) != 3 || !strings.Contains(ce.NextSteps[0], "host user environment") || !strings.Contains(ce.NextSteps[2], "do not issue a new CalDAV password") {
		t.Fatalf("recovery must stay on the host: %v", ce.NextSteps)
	}
	// Reuse reads the same store, so it is no way around an unreadable one:
	// the recovery is identical when another context could be reused.
	cfg := resolveConfig()
	cfg.Auth.Username = ""
	cfg.MayReuse = true
	_, err = Resolve(cfg, config.Secrets{}, store)
	if offered := cerrors.AsCLIError(err); offered.Code != "CREDENTIAL_STORE_INACCESSIBLE" || !reflect.DeepEqual(offered.NextSteps, ce.NextSteps) {
		t.Fatalf("an inaccessible store was sent to reuse: %+v", offered)
	}
}

// reusable is a context `auth reuse` could complete: it has no WeCom email,
// and the loader found another stored context on the same service that has.
func reusable() config.Config {
	cfg := resolveConfig()
	cfg.Auth.Username = ""
	cfg.Auth.CredentialURL = ""
	cfg.MayReuse = true
	return cfg
}

// Contexts on one server share one stored password. A team preset beside a
// signed-in personal context therefore finds the password and lacks only the
// email: reuse is the whole fix, and it comes before every step that could
// lead to a new password.
func TestMissingIdentityOffersReuseFirst(t *testing.T) {
	store := newStoreWithKeyring(t.TempDir(), fakeKeyring{secrets: map[string]string{}})
	cfg := reusable()
	if _, err := Save(cfg.BaseURL, Credential{Scheme: SchemeBasic, Username: "member@example.test", Secret: "shared-password"}, store); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(cfg, config.Secrets{}, store)
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "AUTH_NO_BASIC" || ce.Recovery != nil {
		t.Fatalf("unexpected error: %+v", ce)
	}
	if ce.NextSteps[0] != "wecom-calendar-cli auth reuse --dry-run" || ce.NextSteps[len(ce.NextSteps)-1] != "wecom-calendar-cli auth guide" {
		t.Fatalf("reuse must come first and the guide last: %v", ce.NextSteps)
	}
	if !strings.Contains(ce.Hint, "auth reuse") || !strings.Contains(ce.Hint, "none should be issued") {
		t.Fatalf("the hint does not explain the recovery: %q", ce.Hint)
	}
	// Without a source there is nothing to reuse, and the steps are unchanged.
	cfg.MayReuse = false
	_, err = Resolve(cfg, config.Secrets{}, store)
	if plain := cerrors.AsCLIError(err); plain.Code != "AUTH_NO_BASIC" || strings.Contains(joined(plain.NextSteps)+plain.Hint, "auth reuse") {
		t.Fatalf("reuse was offered without a source: %+v", plain)
	}
}

// When no password is visible at all, the host retry still comes first. Reuse
// follows it, ahead of the step that sends the user to `auth login`: another
// spelling of the server may keep its password under a key of its own.
func TestMissingCredentialOffersReuseAfterTheHostRetry(t *testing.T) {
	store := newStoreWithKeyring(t.TempDir(), fakeKeyring{secrets: map[string]string{}})
	_, err := Resolve(reusable(), config.Secrets{}, store)
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "CREDENTIAL_NOT_VISIBLE_OR_MISSING" || ce.Recovery == nil || ce.Recovery.Scope != "host" {
		t.Fatalf("unexpected error: %+v", ce)
	}
	if len(ce.NextSteps) != 5 || !strings.Contains(ce.NextSteps[0], "host user environment") || ce.NextSteps[1] != "wecom-calendar-cli doctor" {
		t.Fatalf("host retry must come first: %v", ce.NextSteps)
	}
	if ce.NextSteps[2] != "wecom-calendar-cli auth reuse --dry-run" || !strings.Contains(ce.NextSteps[3], "auth login") || ce.NextSteps[4] != "wecom-calendar-cli auth guide" {
		t.Fatalf("reuse must sit between the host retry and acquisition: %v", ce.NextSteps)
	}
}

// An override that only re-spells the stored URL keeps the key the password is
// stored under; one that names another service may not use it.
func TestEquivalentOverrideKeepsTheStoredLookupKey(t *testing.T) {
	store := newStoreWithKeyring(t.TempDir(), fakeKeyring{secrets: map[string]string{}})
	stored := "https://SERVICE.example.test:443/deploy/"
	want := Credential{Scheme: SchemeBasic, Username: "member@example.test", Secret: "stored-password"}
	if _, err := Save(stored, want, store); err != nil {
		t.Fatal(err)
	}
	cfg := resolveConfig()
	if _, err := Resolve(cfg, config.Secrets{}, store); cerrors.AsCLIError(err) == nil || cerrors.AsCLIError(err).Code != "CREDENTIAL_NOT_VISIBLE_OR_MISSING" {
		t.Fatalf("the normalized spelling resolved a key it does not own: %v", err)
	}
	cfg.CredentialBaseURL = stored
	if base, err := CredentialLookupURL(cfg); err != nil || base != stored {
		t.Fatalf("lookup URL: %q %v", base, err)
	}
	if got, err := Resolve(cfg, config.Secrets{}, store); err != nil || got != want {
		t.Fatalf("equivalent override lost the stored credential: %+v %v", got, err)
	}
	cfg.BaseURL = "https://service.example.test/other"
	_, err := Resolve(cfg, config.Secrets{}, store)
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "CREDENTIAL_SERVICE_MISMATCH" || strings.Contains(joined(ce.NextSteps), "auth") {
		t.Fatalf("another deployment used the retained key: %+v", ce)
	}
	if err := ForgetForConfig(cfg, SchemeBasic, store); cerrors.AsCLIError(err) == nil || cerrors.AsCLIError(err).Code != "CREDENTIAL_SERVICE_MISMATCH" {
		t.Fatalf("logout removed another service's credential: %v", err)
	}
}

// A password without an email is an absent credential too.
func TestIncompleteCredentialPointsAtTheGuide(t *testing.T) {
	cfg := resolveConfig()
	cfg.Auth.Username = ""
	cfg.Auth.CredentialURL = ""
	_, err := Resolve(cfg, config.Secrets{Password: "transient"}, nil)
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "AUTH_NO_BASIC" {
		t.Fatalf("unexpected error: %+v", ce)
	}
	if last := ce.NextSteps[len(ce.NextSteps)-1]; last != "wecom-calendar-cli auth guide" {
		t.Fatalf("guide was not appended, or a page was invented: %v", ce.NextSteps)
	}
}

// A stored credential resolves without touching the recovery path.
func TestStoredCredentialResolves(t *testing.T) {
	store := newStoreWithKeyring(t.TempDir(), fakeKeyring{secrets: map[string]string{}})
	cfg := resolveConfig()
	want := Credential{Scheme: SchemeBasic, Username: cfg.Auth.Username, Secret: "stored-password"}
	if _, err := Save(cfg.BaseURL, want, store); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(cfg, config.Secrets{}, store)
	if err != nil || got != want {
		t.Fatalf("got %+v, %v", got, err)
	}
}
