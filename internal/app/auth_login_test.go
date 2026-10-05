package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/auth"
	"github.com/angelmsger/wecom-calendar-cli/internal/config"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/zalando/go-keyring"
)

// cleanCredentialEnv clears every variable the loader reads, so a developer's
// own WECOM_CALENDAR_* settings cannot leak into a test.
func cleanCredentialEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"WECOM_CALENDAR_SERVER", "WECOM_CALENDAR_USERNAME", "WECOM_CALENDAR_PASSWORD",
		"WECOM_CALENDAR_AUTH_SCHEME", "WECOM_CALENDAR_CREDENTIAL_URL", "WECOM_CALENDAR_FORMAT",
		"WECOM_CALENDAR_CLI_READ_ONLY", "WECOM_CALENDAR_CONTEXT",
	} {
		t.Setenv(name, "")
	}
}

// loginStateForTest builds a login target in a scratch directory. The keyring
// is replaced by go-keyring's in-memory mock, so no test reaches the OS keychain.
func loginStateForTest(t *testing.T) (*appState, config.Config, auth.Credential) {
	t.Helper()
	keyring.MockInit()
	cleanCredentialEnv(t)
	dir := t.TempDir()
	cfg := config.Config{BaseURL: "https://service.example.test/deploy", Auth: config.AuthConfig{Scheme: "basic", CredentialURL: "https://help.example.test/credentials"}}
	return &appState{cfgDir: dir, resolved: &config.Resolved{Config: cfg}, store: auth.NewStore(dir)}, cfg, auth.Credential{Scheme: "basic", Username: "member@example.test", Secret: "personal-secret"}
}

// freshProcessCredential resolves the credential the way a later invocation
// does: a new config load from disk and a new store instance.
func freshProcessCredential(t *testing.T, cfgDir, context string) (auth.Credential, *config.Resolved, error) {
	t.Helper()
	resolved, err := config.Load(config.LoadOptions{ConfigDir: cfgDir, Context: context, DotenvPath: filepath.Join(cfgDir, "absent.env")})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := auth.Resolve(resolved.Config, resolved.Secrets, auth.NewStore(cfgDir))
	return cred, resolved, err
}

func TestPersonalLoginSurvivesFreshConfigLoad(t *testing.T) {
	s, cfg, cred := loginStateForTest(t)
	services := s.loginServices()
	services.Verify = func(config.Config, auth.Credential) error { return nil }
	if _, err := completeLogin(s, cfg, cred, services); err != nil {
		t.Fatal(err)
	}
	file, _, err := config.ReadFile(s.cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	nc, ok := file.Context("default")
	if !ok || nc.Auth.Username != cred.Username || nc.Auth.Scheme != cred.Scheme || nc.Auth.CredentialURL != cfg.Auth.CredentialURL || nc.BaseURL != cfg.BaseURL || file.CurrentContext != "default" {
		t.Fatal(file)
	}
	// Reload the persisted identity and resolve the secret from a new store instance.
	got, resolved, err := freshProcessCredential(t, s.cfgDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != cred || resolved.Config.BaseURL != cfg.BaseURL || resolved.ActiveContext != "default" {
		t.Fatalf("fresh process cannot resolve login: %+v %+v", got, resolved)
	}
	raw, _ := os.ReadFile(config.ConfigFilePath(s.cfgDir))
	if strings.Contains(string(raw), cred.Secret) {
		t.Fatal("the secret was written to the config file")
	}
}

func TestLoginRejectsChangedDeploymentPathBeforeAnyIO(t *testing.T) {
	s, cfg, cred := loginStateForTest(t)
	original := config.File{CurrentContext: "team", Contexts: []config.NamedContext{{Name: "team", BaseURL: "https://service.example.test/other", Auth: config.AuthConfig{Scheme: "basic", Username: "old@example.test"}}}}
	if err := config.WriteFile(s.cfgDir, original); err != nil {
		t.Fatal(err)
	}
	s.resolved.ActiveContext = "team"
	called := false
	services := loginServices{Verify: func(config.Config, auth.Credential) error { called = true; return nil }, Save: func(string, auth.Credential) (string, error) { called = true; return "", nil }, Write: func(string, config.File) error { called = true; return nil }}
	_, err := completeLogin(s, cfg, cred, services)
	if err == nil || cerrors.AsCLIError(err).Code != "CONTEXT_BASE_URL_MISMATCH" || called {
		t.Fatalf("unsafe mismatch handling: %v called=%v", err, called)
	}
	after, _, _ := config.ReadFile(s.cfgDir)
	if !reflect.DeepEqual(original, after) {
		t.Fatal("changed old configuration")
	}
}

func TestLoginPersistenceFailuresAreDistinguishable(t *testing.T) {
	for _, stage := range []string{"verify", "save", "write"} {
		t.Run(stage, func(t *testing.T) {
			s, cfg, cred := loginStateForTest(t)
			cause := errors.New("injected failure")
			saved, written := false, false
			services := loginServices{
				Verify: func(config.Config, auth.Credential) error {
					if stage == "verify" {
						return cause
					}
					return nil
				},
				Save: func(string, auth.Credential) (string, error) {
					saved = true
					if stage == "save" {
						return "", cause
					}
					return "test", nil
				},
				Write: func(string, config.File) error { written = true; return cause },
			}
			_, err := completeLogin(s, cfg, cred, services)
			if !errors.Is(err, cause) {
				t.Fatalf("lost cause: %v", err)
			}
			if stage == "verify" && (saved || written) {
				t.Fatal("persisted after failed verification")
			}
			ce := cerrors.AsCLIError(err)
			if stage == "save" {
				if written {
					t.Fatal("wrote config after failed credential storage")
				}
				if ce.Code != "CREDENTIAL_SAVE_FAILED" || ce.Retryable || !strings.Contains(ce.Hint, "was not stored") {
					t.Fatalf("lost the save outcome: %+v", ce)
				}
			}
			if stage == "write" {
				details, _ := ce.Details.(map[string]any)
				if ce.Code != "LOGIN_CONFIG_WRITE_FAILED" || ce.Retryable || details["credential_stored"] != true ||
					details["context"] != "default" || details["server"] != cfg.BaseURL || details["scheme"] != "basic" {
					t.Fatalf("lost partial outcome: %+v", ce)
				}
			}
			// Both partial outcomes are recovered with the password the server
			// just accepted. A new one would invalidate it for every client.
			if stage != "verify" && !strings.Contains(ce.Hint, "do not issue a new one") {
				t.Fatalf("%s recovery could lead to a new CalDAV password: %q", stage, ce.Hint)
			}
			if _, err := os.Stat(config.ConfigFilePath(s.cfgDir)); !os.IsNotExist(err) {
				t.Fatal("failed login created config")
			}
		})
	}
}

func TestSetContextDryRunAndRepeatAvoidWrites(t *testing.T) {
	s, cfg, _ := loginStateForTest(t)
	s.store = nil // The setup path must not need a credential store.
	s.resolved = &config.Resolved{Config: cfg, Sources: map[string]string{config.FieldServer: "flag", config.FieldAuthScheme: "flag", config.FieldCredentialURL: "flag"}}
	s.resolved.Config.Defaults.Format = "json"
	cmd := newConfigSetContextCmd(s)
	_ = cmd.Flags().Set("dry-run", "true")
	if err := cmd.RunE(cmd, []string{"team"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.ConfigFilePath(s.cfgDir)); !os.IsNotExist(err) {
		t.Fatal("dry run wrote config")
	}
	_ = cmd.Flags().Set("dry-run", "false")
	if err := cmd.RunE(cmd, []string{"team"}); err != nil {
		t.Fatal(err)
	}
	path := config.ConfigFilePath(s.cfgDir)
	old := time.Unix(123456789, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, []string{"team"}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if !info.ModTime().Equal(old) {
		t.Fatal("identical setup rewrote config")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("unexpected setup files: %v", entries)
	}
}

// calDAVStub answers like the WeCom calendar-home: an anonymous or wrongly
// authenticated PROPFIND is 401, the right Basic credential gets a 207.
type calDAVStub struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
}

func newCalDAVStub(t *testing.T, username, password string) *calDAVStub {
	t.Helper()
	stub := &calDAVStub{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.requests = append(stub.requests, r.Method+" "+r.URL.Path)
		stub.mu.Unlock()
		user, pass, ok := r.BasicAuth()
		if !ok || user != username || pass != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="caldav"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method != "PROPFIND" || r.URL.Path != "/calendar/" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><D:multistatus xmlns:D="DAV:"></D:multistatus>`))
	}))
	t.Cleanup(stub.Close)
	return stub
}

func (c *calDAVStub) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.requests...)
}

// stubLoginState points a login at the stub. The base URL carries a path on
// purpose: see TestLoginVerifiesAtTheCalendarHomeOfTheOrigin.
func stubLoginState(t *testing.T, stub *calDAVStub) (*appState, config.Config) {
	t.Helper()
	s, cfg, _ := loginStateForTest(t)
	cfg.BaseURL = stub.URL + "/deploy/"
	cfg.Defaults.Timeout = 5 * time.Second
	s.resolved.Config = cfg
	return s, cfg
}

func TestLoginStoresNothingWhenTheServerRejectsThePassword(t *testing.T) {
	stub := newCalDAVStub(t, "member@example.test", "right-password")
	s, cfg := stubLoginState(t, stub)
	_, err := completeLogin(s, cfg, auth.Credential{Scheme: "basic", Username: "member@example.test", Secret: "wrong-password"}, s.loginServices())
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Category != cerrors.CategoryAuth || ce.Code != "CALDAV_AUTH" || ce.HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("rejected password was not an authentication failure: %+v", ce)
	}
	if _, statErr := os.Stat(config.ConfigFilePath(s.cfgDir)); !os.IsNotExist(statErr) {
		t.Fatal("a rejected login wrote the config")
	}
	if _, loadErr := auth.NewStore(s.cfgDir).Load(auth.AccountKey(cfg.BaseURL, "basic")); !errors.Is(loadErr, auth.ErrSecretNotFound) {
		t.Fatalf("a rejected password was stored: %v", loadErr)
	}
}

func TestLoginAgainstTheStubResolvesInAFreshProcess(t *testing.T) {
	stub := newCalDAVStub(t, "member@example.test", "right-password")
	s, cfg := stubLoginState(t, stub)
	cred := auth.Credential{Scheme: "basic", Username: "member@example.test", Secret: "right-password"}
	if _, err := completeLogin(s, cfg, cred, s.loginServices()); err != nil {
		t.Fatal(err)
	}
	got, resolved, err := freshProcessCredential(t, s.cfgDir, "")
	if err != nil || got != cred {
		t.Fatalf("fresh process cannot resolve the verified login: %+v %v", got, err)
	}
	// The stored URL keeps its path, and the reloaded credential still
	// authenticates against the same service.
	if resolved.Config.BaseURL != stub.URL+"/deploy" {
		t.Fatalf("stored service URL lost its path: %q", resolved.Config.BaseURL)
	}
	if err := verifyCredential(s, resolved.Config, got); err != nil {
		t.Fatalf("reloaded credential no longer verifies: %v", err)
	}
}

// Verification is the check doctor and config init run: one authenticated
// PROPFIND on the calendar-home. The client addresses that collection on the
// origin of the configured URL and does not use the URL's path, so the
// complete-URL comparison at login is stricter than request routing.
func TestLoginVerifiesAtTheCalendarHomeOfTheOrigin(t *testing.T) {
	stub := newCalDAVStub(t, "member@example.test", "right-password")
	s, cfg := stubLoginState(t, stub)
	if err := verifyCredential(s, cfg, auth.Credential{Scheme: "basic", Username: "member@example.test", Secret: "right-password"}); err != nil {
		t.Fatal(err)
	}
	if seen := stub.seen(); !reflect.DeepEqual(seen, []string{"PROPFIND /calendar/"}) {
		t.Fatalf("verification sent %v", seen)
	}
}

// There is no identity endpoint to read back, so the evidence is the 207. An
// answer that is not a CalDAV multistatus proves nothing, even with status 200.
func TestVerifyLoginRejectsResponsesThatAreNotCalDAV(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	s, cfg, cred := loginStateForTest(t)
	cfg.BaseURL = server.URL
	cfg.Defaults.Timeout = time.Second
	s.resolved.Config = cfg
	if err := verifyCredential(s, cfg, cred); err == nil {
		t.Fatal("anonymous response was accepted as an authenticated login")
	}
}

func TestEquivalentURLLoginKeepsCredentialKeyAfterReload(t *testing.T) {
	s, cfg, cred := loginStateForTest(t)
	before := config.NamedContext{Name: "team", BaseURL: cfg.BaseURL, Auth: config.AuthConfig{Scheme: "basic"}}
	if err := config.WriteFile(s.cfgDir, config.File{CurrentContext: "team", Contexts: []config.NamedContext{before}}); err != nil {
		t.Fatal(err)
	}
	s.resolved.ActiveContext = "team"
	cfg.BaseURL = "https://SERVICE.example.test:443/deploy/"
	s.resolved.Config = cfg
	services := s.loginServices()
	services.Verify = func(config.Config, auth.Credential) error { return nil }
	if _, err := completeLogin(s, cfg, cred, services); err != nil {
		t.Fatal(err)
	}
	got, _, err := freshProcessCredential(t, s.cfgDir, "team")
	if err != nil || got != cred {
		t.Fatalf("equivalent URL orphaned credential: %+v %v", got, err)
	}
}

// decodeJSON parses one command's stdout.
func decodeJSON(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout)
	}
	return out
}

// The same contract through the real command tree: presets come from flags and
// the environment, personal variables are ignored, nothing touches the
// credential store, and an existing context changes only when asked to.
func TestTeamSetupThroughTheCommandTree(t *testing.T) {
	keyring.MockInit()
	cleanCredentialEnv(t)
	cfgDir := filepath.Join(t.TempDir(), "config")
	t.Setenv("WECOM_CALENDAR_USERNAME", "injected@example.test")
	t.Setenv("WECOM_CALENDAR_PASSWORD", "must-not-be-copied")

	stdout, _, err := captureCLI(t, cfgDir, "config", "set-context", "Team", "--credential-url", "https://help.example.test/caldav")
	if err != nil {
		t.Fatal(err)
	}
	plan := decodeJSON(t, stdout)
	if plan["context"] != "team" || plan["current_context"] != "team" || plan["changed"] != true || plan["dry_run"] != false {
		t.Fatalf("first context was not created and activated: %v", plan)
	}
	raw, err := os.ReadFile(config.ConfigFilePath(cfgDir))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(raw); strings.Contains(text, "injected") || strings.Contains(text, "must-not-be-copied") || strings.Contains(text, "username") {
		t.Fatalf("preset carries personal fields:\n%s", text)
	}
	if entries, _ := os.ReadDir(cfgDir); len(entries) != 1 {
		t.Fatalf("setup created more than the config file: %v", entries)
	}

	// A second context does not become current until asked.
	if _, _, err = captureCLI(t, cfgDir, "config", "set-context", "second", "--base-url", "https://second.example.test/dav"); err != nil {
		t.Fatal(err)
	}
	file, _, _ := config.ReadFile(cfgDir)
	if file.CurrentContext != "team" || len(file.Contexts) != 2 {
		t.Fatalf("adding a context moved the current one: %+v", file)
	}
	// A conflicting service field is refused with its differences, then applied
	// with --overwrite. The stored username of the context survives both.
	file.Contexts[1].Auth.Username = "member@example.test"
	if err := config.WriteFile(cfgDir, file); err != nil {
		t.Fatal(err)
	}
	stdout, _, err = captureCLI(t, cfgDir, "config", "set-context", "second", "--base-url", "https://moved.example.test/dav")
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "CONFIG_CONTEXT_CONFLICT" || cerrors.ExitCode(err) != cerrors.ExitConflict || ce.Details == nil || stdout != "" {
		t.Fatalf("conflict was not refused: %v %q", err, stdout)
	}
	stdout, _, err = captureCLI(t, cfgDir, "config", "set-context", "second", "--base-url", "https://moved.example.test/dav", "--overwrite", "--activate")
	if err != nil {
		t.Fatal(err)
	}
	if plan = decodeJSON(t, stdout); plan["current_context"] != "second" {
		t.Fatalf("--activate did not switch: %v", plan)
	}
	file, _, _ = config.ReadFile(cfgDir)
	second, _ := file.Context("second")
	team, _ := file.Context("team")
	if second.BaseURL != "https://moved.example.test/dav" || second.Auth.Username != "member@example.test" ||
		team.Auth.CredentialURL != "https://help.example.test/caldav" || team.BaseURL != "https://caldav.wecom.work" {
		t.Fatalf("overwrite clobbered identity or another context: %+v", file)
	}

	// The guide follows the selected context and stays offline.
	stdout, _, err = captureCLI(t, cfgDir, "--use-context", "team", "auth", "guide")
	if err != nil {
		t.Fatal(err)
	}
	guide := decodeJSON(t, stdout)
	steps, _ := guide["next_steps"].([]any)
	if guide["credential_url"] != "https://help.example.test/caldav" || guide["source"] != "file" || guide["scheme"] != "basic" ||
		len(steps) != 1 || steps[0] != "wecom-calendar-cli --use-context 'team' auth login" {
		t.Fatalf("unexpected guide: %v", guide)
	}
	stdout, _, err = captureCLI(t, cfgDir, "auth", "guide")
	if err != nil {
		t.Fatal(err)
	}
	if guide = decodeJSON(t, stdout); guide["credential_url"] != "" || guide["source"] != "builtin" {
		t.Fatalf("a context without a team page got a URL: %v", guide)
	}

	// Login refuses a different complete service URL before it prompts.
	_, _, err = captureCLI(t, cfgDir, "auth", "login", "--base-url", "https://moved.example.test/other")
	if ce = cerrors.AsCLIError(err); ce == nil || ce.Code != "CONTEXT_BASE_URL_MISMATCH" {
		t.Fatalf("login accepted another service URL: %v", err)
	}
	if entries, _ := os.ReadDir(cfgDir); len(entries) != 1 {
		t.Fatalf("setup, guide or the refused login touched the credential store: %v", entries)
	}
}
