package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

func cleanSetupEnv(t *testing.T) {
	t.Helper()
	for key := range envBindings {
		t.Setenv(key, "")
	}
	t.Setenv("WECOM_CALENDAR_CONTEXT", "")
}

func setupFixture() NamedContext {
	return NamedContext{Name: "team", BaseURL: "https://service.example.test/deploy", Auth: AuthConfig{Scheme: "basic", Username: "member@example.test", CredentialURL: "https://help.example.test/caldav"}}
}

func TestSetupTargetsNamedContextAndExcludesPersonalEnvironment(t *testing.T) {
	cleanSetupEnv(t)
	dir := t.TempDir()
	target := setupFixture()
	file := File{CurrentContext: "other", Contexts: []NamedContext{{Name: "other", BaseURL: "https://other.example.test", Auth: AuthConfig{Scheme: "basic", Username: "other@example.test"}}, target}}
	if err := WriteFile(dir, file); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WECOM_CALENDAR_CONTEXT", "not-defined")
	t.Setenv("WECOM_CALENDAR_FORMAT", "ndjson")
	t.Setenv("WECOM_CALENDAR_USERNAME", "injected@example.test")
	t.Setenv("WECOM_CALENDAR_PASSWORD", "must-not-be-copied")
	dotenv := filepath.Join(dir, "presets.env")
	if err := os.WriteFile(dotenv, []byte("WECOM_CALENDAR_SERVER=https://dotenv.example.test/deploy\nWECOM_CALENDAR_CREDENTIAL_URL=https://help.example.test/new\nWECOM_CALENDAR_PASSWORD=dotenv-secret\nWECOM_CALENDAR_USERNAME=dotenv@example.test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	opt := LoadOptions{ConfigDir: dir, DotenvPath: dotenv, Context: "TEAM", Setup: true}
	got, err := Load(opt)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Defaults.Format != "ndjson" {
		t.Fatal("setup ignored the output format environment variable")
	}
	if got.ActiveContext != "team" || got.Config.BaseURL != "https://dotenv.example.test/deploy" || got.Config.Auth.Username != "member@example.test" || got.Config.Auth.Scheme != "basic" {
		t.Fatalf("wrong target or personal override: %+v", got)
	}
	if !reflect.DeepEqual(got.Secrets, Secrets{}) {
		t.Fatalf("setup retained secrets: %+v", got.Secrets)
	}
	if got.Sources[fieldServer] != "dotenv" || got.Sources[fieldCredentialURL] != "dotenv" {
		t.Fatal(got.Sources)
	}
	// There is one scheme, so its value cannot show whether a password implied
	// it; its source can. The passwords in the environment and in .env were
	// dropped before inference, which leaves the stored context as the source.
	if got.Sources[fieldAuthScheme] != "file" {
		t.Fatalf("a personal secret decided the scheme: %v", got.Sources)
	}
	t.Setenv("WECOM_CALENDAR_SERVER", "https://env.example.test")
	t.Setenv("WECOM_CALENDAR_AUTH_SCHEME", "basic")
	got, err = Load(opt)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.BaseURL != "https://env.example.test" || got.Config.Auth.Scheme != "basic" || got.Sources[fieldServer] != "env" || got.Sources[fieldAuthScheme] != "env" {
		t.Fatal(got)
	}
	opt.Flags = FlagValues{BaseURL: "https://flag.example.test", CredentialURL: "https://help.example.test/flag", AuthScheme: "basic"}
	got, err = Load(opt)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.BaseURL != "https://flag.example.test" || got.Sources[fieldServer] != "flag" || got.Sources[fieldCredentialURL] != "flag" || got.Sources[fieldAuthScheme] != "flag" {
		t.Fatal(got)
	}
	opt.Context = "new"
	opt.Flags = FlagValues{}
	t.Setenv("WECOM_CALENDAR_SERVER", "")
	opt.DotenvPath = filepath.Join(dir, "absent")
	got, err = Load(opt)
	if err != nil {
		t.Fatal(err)
	}
	// A new target starts from the documented default endpoint, where the
	// siblings start from an empty URL. It inherits nothing from the current
	// context or from the personal variables.
	if got.Config.BaseURL != constants.DefaultServerURL || got.Sources[fieldServer] != "default" || got.Config.Auth.Username != "" || got.Config.Auth.CredentialURL != "" {
		t.Fatalf("new target inherited active context: %+v", got)
	}
}

func TestPlanServiceContextConflictIdempotencyAndPreservation(t *testing.T) {
	cleanSetupEnv(t)
	dir := t.TempDir()
	target := setupFixture()
	file := File{CurrentContext: "other", Contexts: []NamedContext{{Name: "other", BaseURL: "https://other.example.test"}, target},
		Defaults: Defaults{ReadOnly: true, Format: "table", PageSize: 50, Timeout: 45 * time.Second, MaxRetries: 5}}
	if err := WriteFile(dir, file); err != nil {
		t.Fatal(err)
	}
	opt := LoadOptions{ConfigDir: dir, Context: "TEAM", Setup: true, DotenvPath: filepath.Join(dir, "absent")}
	resolved, err := Load(opt)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanServiceContext(file, "TEAM", resolved, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Changed {
		t.Fatalf("repeat is not idempotent: %+v", plan.Changes)
	}
	opt.Flags.BaseURL = "https://new.example.test/deploy"
	resolved, err = Load(opt)
	if err != nil {
		t.Fatal(err)
	}
	_, err = PlanServiceContext(file, "team", resolved, false, false)
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "CONFIG_CONTEXT_CONFLICT" || ce.Category != cerrors.CategoryConflict {
		t.Fatalf("missing structured differences: %v", err)
	}
	differences, ok := ce.Details.(map[string]FieldChange)
	if !ok || !reflect.DeepEqual(differences, map[string]FieldChange{fieldServer: {Before: target.BaseURL, After: "https://new.example.test/deploy"}}) {
		t.Fatalf("conflict did not report the non-secret field differences: %#v", ce.Details)
	}
	plan, err = PlanServiceContext(file, "team", resolved, true, false)
	if err != nil {
		t.Fatal(err)
	}
	nc, _ := plan.File.Context("team")
	if nc.Auth != target.Auth || plan.File.CurrentContext != "other" || !reflect.DeepEqual(plan.File.Defaults, file.Defaults) || plan.File.Contexts[0] != file.Contexts[0] {
		t.Fatalf("clobbered unrelated values: %+v", plan.File)
	}
	if err := WriteFile(dir, plan.File); err != nil {
		t.Fatal(err)
	}
	roundtrip, _, err := ReadFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundtrip, plan.File) {
		t.Fatalf("roundtrip lost fields: got %+v want %+v", roundtrip, plan.File)
	}
	plan, err = PlanServiceContext(file, "team", resolved, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CurrentContext != "team" {
		t.Fatal(plan)
	}
	initial := &Resolved{Config: resolved.Config, Sources: resolved.Sources}
	plan, err = PlanServiceContext(File{}, "team", initial, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CurrentContext != "team" {
		t.Fatal("first context must activate")
	}
}

// A preset needs no --base-url: the server defaults to the public WeCom CalDAV
// endpoint. Whatever the environment holds, the WeCom email and the CalDAV
// password never reach the plan.
func TestPresetDefaultsToThePublicEndpointAndCarriesNoIdentity(t *testing.T) {
	cleanSetupEnv(t)
	dir := t.TempDir()
	t.Setenv("WECOM_CALENDAR_USERNAME", "injected@example.test")
	t.Setenv("WECOM_CALENDAR_PASSWORD", "must-not-be-copied")
	resolved, err := Load(LoadOptions{ConfigDir: dir, Context: "team", Setup: true, DotenvPath: filepath.Join(dir, "absent")})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanServiceContext(File{}, "team", resolved, false, false)
	if err != nil {
		t.Fatal(err)
	}
	nc, ok := plan.File.Context("team")
	if !ok || nc.BaseURL != "https://caldav.wecom.work" || nc.Auth != (AuthConfig{Scheme: SchemeBasic}) {
		t.Fatalf("unexpected preset: %+v", nc)
	}
	raw, _ := json.Marshal(plan)
	if strings.Contains(string(raw), "injected") || strings.Contains(string(raw), "must-not-be-copied") {
		t.Fatalf("plan output exposes personal fields: %s", raw)
	}
	if err := WriteFile(dir, plan.File); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(ConfigFilePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written), "injected") || strings.Contains(string(written), "must-not-be-copied") || strings.Contains(string(written), "username") {
		t.Fatalf("preset file carries personal fields:\n%s", written)
	}
}

// WeCom has no web credential page, so the guide carries no URL of its own.
func TestCredentialGuideHasNoURLUnlessATeamConfiguresOne(t *testing.T) {
	nc := setupFixture()
	cfg := Config{BaseURL: nc.BaseURL, Auth: nc.Auth}
	cfg.Auth.CredentialURL = ""
	g, err := Guide(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if g.CredentialURL != "" || g.Source != "builtin" || g.Server != nc.BaseURL || g.Scheme != SchemeBasic {
		t.Fatalf("the built-in guide must name the service and no URL: %+v", g)
	}
	if g.DocumentationURL == "" || len(g.NextSteps) == 0 {
		t.Fatal(g)
	}
	text := strings.Join(g.Instructions, "\n")
	for _, want := range []string{"WeCom mobile app", "Workbench", "Calendar", "Sync to other calendars", "invalidates the previous one", "CREDENTIAL_STORE_INACCESSIBLE", "do not issue a new one"} {
		if !strings.Contains(text, want) {
			t.Fatalf("instructions do not mention %q:\n%s", want, text)
		}
	}
	for _, line := range g.Lines() {
		if strings.HasPrefix(line, "Credential page:") {
			t.Fatalf("printed a credential page that does not exist: %q", line)
		}
	}
	cfg.Auth.CredentialURL = "https://login.example.test/custom?view=credentials"
	g, err = Guide(cfg, map[string]string{fieldCredentialURL: "env"})
	if err != nil {
		t.Fatal(err)
	}
	if g.CredentialURL != cfg.Auth.CredentialURL || g.Server != nc.BaseURL || g.Source != "env" {
		t.Fatal(g)
	}
	if lines := strings.Join(g.Lines(), "\n"); !strings.Contains(lines, "Credential page: "+cfg.Auth.CredentialURL) || !strings.Contains(lines, "must never be sent to it") {
		t.Fatalf("the team page is not shown as display-only:\n%s", lines)
	}
	if g, _ = Guide(cfg, nil); g.Source != "config" {
		t.Fatalf("an unattributed team page must still be marked as configured: %+v", g)
	}
	raw, _ := json.Marshal(g)
	if strings.Contains(string(raw), "member@example.test") {
		t.Fatal("guide should not expose personal identity")
	}
	for _, bad := range []string{"javascript:alert(1)", "https://user:secret@example.test", "/relative", "https://example.test/\npath"} {
		cfg.Auth.CredentialURL = bad
		if _, err := Guide(cfg, nil); err == nil {
			t.Fatalf("accepted unsafe link %q", bad)
		}
	}
}

// basic is the only scheme; the family's plumbing rejects every other value.
func TestOnlyTheBasicSchemeIsAccepted(t *testing.T) {
	cleanSetupEnv(t)
	nc := setupFixture()
	for _, scheme := range []string{"pat", "bearer", "none", ""} {
		cfg := Config{BaseURL: nc.BaseURL, Auth: AuthConfig{Scheme: scheme}}
		if ce := cerrors.AsCLIError(ValidateService(cfg)); ce == nil || ce.Code != "AUTH_BAD_SCHEME" || !strings.Contains(ce.Hint, "basic") {
			t.Fatalf("scheme %q: %+v", scheme, ce)
		}
	}
	dir := t.TempDir()
	resolved, err := Load(LoadOptions{ConfigDir: dir, Context: "team", Setup: true, DotenvPath: filepath.Join(dir, "absent"), Flags: FlagValues{AuthScheme: "pat"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PlanServiceContext(File{}, "team", resolved, false, false); cerrors.AsCLIError(err) == nil || cerrors.AsCLIError(err).Code != "AUTH_BAD_SCHEME" {
		t.Fatalf("a preset accepted an unsupported scheme: %v", err)
	}
}

func TestNormalizeServiceIdentityAndRejectInvalidInput(t *testing.T) {
	normal, err := NormalizeServiceURL("https://SERVICE.example.test:443/deploy/")
	if err != nil || normal != "https://service.example.test/deploy" {
		t.Fatalf("%q %v", normal, err)
	}
	if normal, err = NormalizeServiceURL(constants.DefaultServerURL); err != nil || normal != "https://caldav.wecom.work" {
		t.Fatalf("default endpoint: %q %v", normal, err)
	}
	for _, bad := range []string{"", "file:///tmp/token", "https://user:secret@example.test", "https://example.test/?token=secret", "https://example.test/#fragment"} {
		if _, err := NormalizeServiceURL(bad); err == nil {
			t.Fatalf("accepted invalid service URL %q", bad)
		}
	}
}

func TestSetupRecoveryQuotesContextNames(t *testing.T) {
	nc := setupFixture()
	resolved := &Resolved{Config: Config{BaseURL: nc.BaseURL, Auth: nc.Auth}}
	plan, err := PlanServiceContext(File{}, "ops team's", resolved, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plan.NextSteps {
		if !strings.Contains(step, `--use-context 'ops team'"'"'s'`) {
			t.Fatalf("context was not safely quoted: %s", step)
		}
	}
}

// Acquisition guidance joins an absent credential and never an unreadable
// store: a new CalDAV password invalidates the one the user's clients hold.
func TestCredentialGuideJoinsAbsenceErrorsOnly(t *testing.T) {
	nc := setupFixture()
	cfg := Config{BaseURL: nc.BaseURL, Auth: nc.Auth}
	for _, code := range []string{"AUTH_NO_BASIC", "CREDENTIAL_NOT_VISIBLE_OR_MISSING"} {
		ce := cerrors.AsCLIError(WithCredentialGuide(cerrors.New(cerrors.CategoryConfig, code, "absent").WithNextSteps("first"), cfg))
		want := []string{"first", constants.AppName + " auth guide", "Credential page: " + nc.Auth.CredentialURL}
		if !reflect.DeepEqual(ce.NextSteps, want) {
			t.Fatalf("%s: %v", code, ce.NextSteps)
		}
	}
	cfg.Auth.CredentialURL = ""
	ce := cerrors.AsCLIError(WithCredentialGuide(cerrors.New(cerrors.CategoryConfig, "AUTH_NO_BASIC", "absent").WithNextSteps("first"), cfg))
	if !reflect.DeepEqual(ce.NextSteps, []string{"first", constants.AppName + " auth guide"}) {
		t.Fatalf("a page was invented: %v", ce.NextSteps)
	}
	ce = cerrors.AsCLIError(WithCredentialGuide(cerrors.New(cerrors.CategoryConfig, "CREDENTIAL_STORE_INACCESSIBLE", "unreadable").WithNextSteps("first"), cfg))
	if !reflect.DeepEqual(ce.NextSteps, []string{"first"}) {
		t.Fatalf("an inaccessible store was sent to credential acquisition: %v", ce.NextSteps)
	}
}
