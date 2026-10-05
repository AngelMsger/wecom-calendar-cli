package auth

import (
	"errors"
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
