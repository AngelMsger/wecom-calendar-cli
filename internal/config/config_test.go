package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// TestFileFormat pins the bytes of config.yaml. The engine is shared; the
// shape is wecom-calendar-cli's, and existing files must keep reading and
// writing the same way.
func TestFileFormat(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	in := File{
		CurrentContext: "team",
		Contexts: []NamedContext{
			{Name: "default", BaseURL: constants.DefaultServerURL, Auth: AuthConfig{Scheme: SchemeBasic, Username: "member@example.test"}},
			{Name: "team", BaseURL: "https://caldav.wecom.work", Auth: AuthConfig{Scheme: SchemeBasic,
				CredentialURL: "https://wiki.example.test/wecom-caldav"}},
			{Name: "bare"},
		},
		Defaults: Defaults{Format: "table", PageSize: 50, Timeout: 90 * time.Second, MaxRetries: 5, ReadOnly: true},
	}
	if err := WriteFile(dir, in); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ConfigFilePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	const want = `current_context: team
contexts:
    - name: default
      server: https://caldav.wecom.work/
      auth:
        scheme: basic
        username: member@example.test
    - name: team
      server: https://caldav.wecom.work
      auth:
        credential_url: https://wiki.example.test/wecom-caldav
        scheme: basic
    - name: bare
defaults:
    format: table
    page_size: 50
    timeout: 1m30s
    max_retries: 5
    read_only: true
`
	if string(raw) != want {
		t.Errorf("config.yaml changed:\n%s", raw)
	}
	got, ok, err := ReadFile(dir)
	if err != nil || !ok || !reflect.DeepEqual(got, in) {
		t.Errorf("read back %+v, %v, %v", got, ok, err)
	}

	// Every block is optional: an empty file writes as {}.
	if err := WriteFile(dir, File{}); err != nil {
		t.Fatal(err)
	}
	if raw, _ = os.ReadFile(ConfigFilePath(dir)); string(raw) != "{}\n" {
		t.Errorf("empty config.yaml = %q", raw)
	}

	// A legacy flat file is read as one context named "default" and is
	// rewritten in the contexts form.
	flat := "server: https://caldav.wecom.work/\nauth:\n  scheme: basic\n  username: member@example.test\ndefaults:\n  page_size: 20\n"
	if err := os.WriteFile(ConfigFilePath(dir), []byte(flat), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy, _, err := ReadFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(dir, legacy); err != nil {
		t.Fatal(err)
	}
	const rewritten = `current_context: default
contexts:
    - name: default
      server: https://caldav.wecom.work/
      auth:
        scheme: basic
        username: member@example.test
defaults:
    page_size: 20
`
	if raw, _ = os.ReadFile(ConfigFilePath(dir)); string(raw) != rewritten {
		t.Errorf("legacy file rewritten as:\n%s", raw)
	}
}

// TestSchema pins wecom-calendar-cli's environment variables, built-in
// defaults (the public server among them), scheme inference and URL rules.
func TestSchema(t *testing.T) {
	cleanSetupEnv(t)
	dir := t.TempDir()
	load := func() *Resolved {
		t.Helper()
		r, err := Load(LoadOptions{ConfigDir: dir, DotenvPath: filepath.Join(dir, "absent.env")})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := load()
	want := Config{BaseURL: constants.DefaultServerURL, Auth: AuthConfig{Scheme: SchemeBasic},
		Defaults: Defaults{Format: "json", PageSize: 100, Timeout: 30 * time.Second, MaxRetries: 3}}
	if !reflect.DeepEqual(r.Config, want) || r.Sources[FieldServer] != "default" || r.Sources[FieldAuthScheme] != "default" {
		t.Fatalf("defaults = %+v (%v)", r.Config, r.Sources)
	}
	if base, err := NormalizeServiceURL(r.Config.BaseURL); err != nil || base != "https://caldav.wecom.work" {
		t.Errorf("the default endpoint normalizes to %q %v", base, err)
	}

	t.Setenv("WECOM_CALENDAR_SERVER", "https://caldav.example.test/dav")
	t.Setenv("WECOM_CALENDAR_USERNAME", "member@example.test")
	t.Setenv("WECOM_CALENDAR_CREDENTIAL_URL", "https://wiki.example.test/caldav")
	t.Setenv("WECOM_CALENDAR_FORMAT", "table")
	t.Setenv("WECOM_CALENDAR_CLI_READ_ONLY", "yes")
	t.Setenv("WECOM_CALENDAR_PASSWORD", "pw")
	r = load()
	if r.Config.BaseURL != "https://caldav.example.test/dav" || r.Config.Auth.Username != "member@example.test" ||
		r.Config.Auth.CredentialURL != "https://wiki.example.test/caldav" || r.Config.Defaults.Format != "table" ||
		!r.Config.Defaults.ReadOnly || r.Secrets != (Secrets{Password: "pw"}) {
		t.Fatalf("environment = %+v %+v", r.Config, r.Secrets)
	}
	// A password implies the only scheme; an explicit scheme still wins.
	if r.Config.Auth.Scheme != SchemeBasic || r.Sources[FieldAuthScheme] != "env" {
		t.Errorf("a password did not imply basic: %q (%s)", r.Config.Auth.Scheme, r.Sources[FieldAuthScheme])
	}
	t.Setenv("WECOM_CALENDAR_AUTH_SCHEME", "bearer")
	if r = load(); r.Config.Auth.Scheme != "bearer" {
		t.Errorf("an explicit scheme wins: %q", r.Config.Auth.Scheme)
	}
	// No http:// is assumed: a bare host is not a service URL.
	if _, err := NormalizeServiceURL("caldav.example.test"); cerrors.AsCLIError(err) == nil || cerrors.AsCLIError(err).Code != "BAD_BASE_URL" {
		t.Errorf("a bare host was accepted: %v", err)
	}
}
