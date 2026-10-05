package app

import (
	"errors"
	"testing"

	"github.com/angelmsger/wecom-calendar-cli/internal/auth"
	"github.com/angelmsger/wecom-calendar-cli/internal/config"
)

// A stored secret is keyed by the server's host and the scheme, so a preset
// and a personal context on the public endpoint share one. Losing it is costly
// here: the password cannot be read back from WeCom, and issuing another one
// invalidates it for every calendar client.

func storedSecret(t *testing.T, cfgDir, baseURL string) (string, error) {
	t.Helper()
	return auth.NewStore(cfgDir).Load(auth.AccountKey(baseURL, "basic"))
}

func personalContext(name, baseURL string) config.NamedContext {
	return config.NamedContext{Name: name, BaseURL: baseURL, Auth: config.AuthConfig{Scheme: "basic", Username: "member@example.test"}}
}

// A preset stores the URL without a trailing slash; the wizard default and
// .env.example spell it with one. Re-running the wizard across that difference
// must not delete the credential it has just saved.
func TestConfigInitKeepsTheCredentialWhenOnlyTheURLSpellingChanges(t *testing.T) {
	s, _, _ := loginStateForTest(t)
	existing := config.File{CurrentContext: "default", Contexts: []config.NamedContext{personalContext("default", "https://caldav.wecom.work")}}
	edited := personalContext("default", "https://caldav.wecom.work/")
	result := &config.WizardResult{
		File:  config.File{CurrentContext: "default", Contexts: []config.NamedContext{edited}},
		Creds: []config.ContextResult{{Context: edited, Secrets: config.Secrets{Password: "app-password"}}},
	}
	if _, err := persistInitResult(s, result, existing); err != nil {
		t.Fatal(err)
	}
	if got, err := storedSecret(t, s.cfgDir, edited.BaseURL); err != nil || got != "app-password" {
		t.Fatalf("the wizard deleted the credential it had just saved: %q %v", got, err)
	}
}

// Moving a context to another server still clears the secret nothing uses any
// more, and leaves one that a second context on the old server resolves.
func TestConfigInitForgetsOnlyCredentialsNoContextUses(t *testing.T) {
	for _, shared := range []bool{false, true} {
		s, _, _ := loginStateForTest(t)
		old := personalContext("default", "https://old.example.test/dav")
		if _, err := auth.Save(old.BaseURL, auth.Credential{Scheme: "basic", Username: old.Auth.Username, Secret: "old-password"}, s.store); err != nil {
			t.Fatal(err)
		}
		existing := config.File{CurrentContext: "default", Contexts: []config.NamedContext{old}}
		moved := personalContext("default", "https://new.example.test/dav")
		file := config.File{CurrentContext: "default", Contexts: []config.NamedContext{moved}}
		if shared {
			other := personalContext("other", "https://old.example.test/")
			existing.Contexts = append(existing.Contexts, other)
			file.Contexts = append([]config.NamedContext{other}, moved)
		}
		result := &config.WizardResult{File: file, Creds: []config.ContextResult{{Context: moved, Secrets: config.Secrets{Password: "new-password"}}}}
		if _, err := persistInitResult(s, result, existing); err != nil {
			t.Fatal(err)
		}
		if got, err := storedSecret(t, s.cfgDir, moved.BaseURL); err != nil || got != "new-password" {
			t.Fatalf("shared=%v: new credential missing: %q %v", shared, got, err)
		}
		got, err := storedSecret(t, s.cfgDir, old.BaseURL)
		if shared && (err != nil || got != "old-password") {
			t.Fatalf("a credential another context still uses was deleted: %q %v", got, err)
		}
		if !shared && !errors.Is(err, auth.ErrSecretNotFound) {
			t.Fatalf("an unused credential was left behind: %q %v", got, err)
		}
	}
}

func TestDeleteContextKeepsACredentialAnotherContextUses(t *testing.T) {
	s, _, _ := loginStateForTest(t)
	file := config.File{CurrentContext: "default", Contexts: []config.NamedContext{
		personalContext("default", "https://caldav.wecom.work/"),
		{Name: "team", BaseURL: "https://caldav.wecom.work", Auth: config.AuthConfig{Scheme: "basic"}},
		personalContext("elsewhere", "https://elsewhere.example.test/dav"),
	}}
	if err := config.WriteFile(s.cfgDir, file); err != nil {
		t.Fatal(err)
	}
	for _, c := range []config.NamedContext{file.Contexts[0], file.Contexts[2]} {
		if _, err := auth.Save(c.BaseURL, auth.Credential{Scheme: "basic", Username: c.Auth.Username, Secret: "password-for-" + c.Name}, s.store); err != nil {
			t.Fatal(err)
		}
	}
	// The preset shares the personal context's server, and therefore its secret.
	if _, _, err := captureCLI(t, s.cfgDir, "config", "delete-context", "team"); err != nil {
		t.Fatal(err)
	}
	if got, err := storedSecret(t, s.cfgDir, "https://caldav.wecom.work/"); err != nil || got != "password-for-default" {
		t.Fatalf("deleting the preset removed the personal context's credential: %q %v", got, err)
	}
	// A context that is the only user of its server takes its secret with it.
	if _, _, err := captureCLI(t, s.cfgDir, "config", "delete-context", "elsewhere"); err != nil {
		t.Fatal(err)
	}
	if got, err := storedSecret(t, s.cfgDir, "https://elsewhere.example.test/dav"); !errors.Is(err, auth.ErrSecretNotFound) {
		t.Fatalf("an unused credential was left behind: %q %v", got, err)
	}
	after, _, _ := config.ReadFile(s.cfgDir)
	if len(after.Contexts) != 1 || after.Contexts[0].Name != "default" {
		t.Fatalf("unexpected contexts: %+v", after)
	}
}
