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
	"testing"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/auth"
	"github.com/angelmsger/wecom-calendar-cli/internal/config"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/zalando/go-keyring"
)

// reuseFixture is a personal context beside a team preset on one service. The
// preset has no WeCom email, which is the state `auth reuse` completes. The
// keyring is the in-memory mock, and the default services never reach it.
func reuseFixture(t *testing.T) (*appState, config.File, reuseServices) {
	t.Helper()
	keyring.MockInit()
	cleanCredentialEnv(t)
	dir := t.TempDir()
	source := config.NamedContext{Name: "personal", BaseURL: "https://service.example.test/deploy", Auth: config.AuthConfig{Scheme: "basic", Username: "member@example.test"}}
	target := source
	target.Name = "team"
	target.Auth.Username = ""
	file := config.File{CurrentContext: "personal", Contexts: []config.NamedContext{source, target}}
	if err := config.WriteFile(dir, file); err != nil {
		t.Fatal(err)
	}
	s := &appState{cfgDir: dir, store: auth.NewStore(dir), resolved: &config.Resolved{Config: reuseContextConfig(target, file.Defaults), ActiveContext: "team"}}
	services := reuseServices{Read: config.ReadFile, Write: config.WriteFile,
		Resolve: func(cfg config.Config) (auth.Credential, error) {
			return auth.Credential{Scheme: cfg.Auth.Scheme, Username: cfg.Auth.Username, Secret: "test-secret"}, nil
		},
		Verify: func(config.Config, auth.Credential) error { return nil }}
	return s, file, services
}

// noCredentialAccess fails the test when reuse reads a credential it has no
// reason to read.
func noCredentialAccess(t *testing.T, why string) func(config.Config) (auth.Credential, error) {
	t.Helper()
	return func(config.Config) (auth.Credential, error) {
		t.Fatal(why)
		return auth.Credential{}, nil
	}
}

// assertRunnableStep resolves a recovery step against the command tree: it must
// name a real leaf command and pass flags and arguments that command accepts.
// The context listing is `config get-contexts` here and `config contexts` in
// some siblings, so a borrowed spelling has to fail a test, not a user.
func assertRunnableStep(t *testing.T, step string) {
	t.Helper()
	words := strings.Fields(step)
	if len(words) < 2 || words[0] != constants.AppName {
		t.Errorf("recovery %q is not a %s command", step, constants.AppName)
		return
	}
	root := NewRootCmd()
	cmd, rest, err := root.Find(words[1:])
	if err != nil || cmd == root || cmd.HasSubCommands() {
		t.Errorf("recovery %q does not resolve to a command (reached %q, %v)", step, cmd.CommandPath(), err)
		return
	}
	if err := cmd.ParseFlags(rest); err != nil {
		t.Errorf("recovery %q passes a flag %q does not accept: %v", step, cmd.CommandPath(), err)
		return
	}
	if err := cmd.ValidateArgs(cmd.Flags().Args()); err != nil {
		t.Errorf("recovery %q passes arguments %q does not accept: %v", step, cmd.CommandPath(), err)
	}
}

func TestAuthReusePreviewApplyAndIdempotence(t *testing.T) {
	s, before, services := reuseFixture(t)
	path := config.ConfigFilePath(s.cfgDir)
	bytesBefore, _ := os.ReadFile(path)
	preview, err := reuseAuthentication(s, "", true, services)
	if err != nil || !preview.Changed || preview.State != "available" || !preview.Verified || !preview.DryRun || preview.SourceContext != "personal" || preview.Context != "team" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	afterPreview, _ := os.ReadFile(path)
	if string(afterPreview) != string(bytesBefore) {
		t.Fatal("preview changed config")
	}
	applied, err := reuseAuthentication(s, "", false, services)
	if err != nil || applied.State != "reused" || applied.SourceContext != "personal" || !applied.Changed || !applied.Verified || applied.DryRun {
		t.Fatalf("apply: %+v %v", applied, err)
	}
	after, _, _ := config.ReadFile(s.cfgDir)
	target, _ := after.Context("team")
	if target.Auth.Username != "member@example.test" || after.CurrentContext != before.CurrentContext || !reflect.DeepEqual(after.Contexts[0], before.Contexts[0]) {
		t.Fatal("did not preserve personal source/current context")
	}
	if target.BaseURL != before.Contexts[1].BaseURL || target.Auth.Scheme != "basic" {
		t.Fatalf("reuse changed the destination's service: %+v", target)
	}
	services.Write = func(string, config.File) error { t.Fatal("repeated write"); return nil }
	services.Resolve = noCredentialAccess(t, "populated identity must remain unchanged")
	repeated, err := reuseAuthentication(s, "", false, services)
	if err != nil || repeated.Changed || repeated.Verified || repeated.State != "unchanged" || repeated.Reason == "" {
		t.Fatalf("repeat: %+v %v", repeated, err)
	}
	raw, _ := json.Marshal(applied)
	written, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "test-secret") || strings.Contains(string(written), "test-secret") {
		t.Fatal("secret reached output or the config file")
	}
}

func TestAuthReuseNativeCredentialSurvivesFreshLoad(t *testing.T) {
	s, _, services := reuseFixture(t)
	credential := auth.Credential{Scheme: "basic", Username: "member@example.test", Secret: "retained-secret"}
	if _, err := auth.Save(s.cfg().BaseURL, credential, s.store); err != nil {
		t.Fatal(err)
	}
	services.Resolve = func(cfg config.Config) (auth.Credential, error) {
		return auth.Resolve(cfg, config.Secrets{}, s.store)
	}
	if _, err := reuseAuthentication(s, "", false, services); err != nil {
		t.Fatal(err)
	}
	// A later invocation: a new config load from disk and a new store instance.
	got, resolved, err := freshProcessCredential(t, s.cfgDir, "team")
	if err != nil || got != credential || resolved.ActiveContext != "team" {
		t.Fatalf("fresh identity could not resolve existing credential: %+v %v", got, err)
	}
	if resolved.Config.MayReuse {
		t.Fatal("a completed context is still offered reuse")
	}
}

func TestAuthReuseMatchesCompleteServiceBeforeCredentialAccess(t *testing.T) {
	for _, kind := range []string{"host", "path", "protocol", "port", "scheme"} {
		t.Run(kind, func(t *testing.T) {
			s, file, services := reuseFixture(t)
			switch kind {
			case "host":
				file.Contexts[0].BaseURL = "https://other.example.test/deploy"
			case "path":
				file.Contexts[0].BaseURL = "https://service.example.test/other"
			case "protocol":
				file.Contexts[0].BaseURL = "http://service.example.test/deploy"
			case "port":
				file.Contexts[0].BaseURL = "https://service.example.test:8443/deploy"
			case "scheme":
				file.Contexts[0].Auth.Scheme = "token"
			}
			if err := config.WriteFile(s.cfgDir, file); err != nil {
				t.Fatal(err)
			}
			services.Resolve = noCredentialAccess(t, "read credential before matching service")
			result, err := reuseAuthentication(s, "", true, services)
			if err != nil || result.Changed || result.State != "unavailable" || result.SourceContext != "" {
				t.Fatalf("mismatch: %+v %v", result, err)
			}
			_, err = reuseAuthentication(s, "personal", true, services)
			if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_SOURCE_MISMATCH" || cerrors.ExitCode(err) != cerrors.ExitConflict {
				t.Fatalf("explicit mismatch: %v", err)
			}
		})
	}
}

// basic is the only scheme. A destination stored with another one is a
// configuration error, reported before a source is matched or a secret read.
func TestAuthReuseRefusesAnUnsupportedSchemeBeforeCredentialAccess(t *testing.T) {
	s, file, services := reuseFixture(t)
	for i := range file.Contexts {
		file.Contexts[i].Auth.Scheme = "pat"
	}
	if err := config.WriteFile(s.cfgDir, file); err != nil {
		t.Fatal(err)
	}
	s.resolved.Config = reuseContextConfig(file.Contexts[1], file.Defaults)
	services.Resolve = noCredentialAccess(t, "read a credential for an unsupported scheme")
	_, err := reuseAuthentication(s, "", true, services)
	if err == nil || cerrors.AsCLIError(err).Code != "AUTH_BAD_SCHEME" {
		t.Fatalf("unsupported scheme: %v", err)
	}
}

func TestAuthReusePreservesDestinationIdentity(t *testing.T) {
	s, file, services := reuseFixture(t)
	file.Contexts[1].Auth.Username = "other@example.test"
	if err := config.WriteFile(s.cfgDir, file); err != nil {
		t.Fatal(err)
	}
	services.Resolve = noCredentialAccess(t, "must not replace an existing identity")
	r, err := reuseAuthentication(s, "", false, services)
	if err != nil || r.State != "unchanged" || r.Changed || r.Verified {
		t.Fatalf("existing identity: %+v %v", r, err)
	}
	after, _, _ := config.ReadFile(s.cfgDir)
	if !reflect.DeepEqual(file, after) {
		t.Fatal("an existing identity was rewritten")
	}
}

func TestAuthReuseAmbiguityAndExplicitSelection(t *testing.T) {
	s, file, services := reuseFixture(t)
	other := file.Contexts[0]
	other.Name = "second"
	other.Auth.Username = "second@example.test"
	file.Contexts = append(file.Contexts, other)
	if err := config.WriteFile(s.cfgDir, file); err != nil {
		t.Fatal(err)
	}
	_, err := reuseAuthentication(s, "", true, services)
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "AUTH_REUSE_AMBIGUOUS" || cerrors.ExitCode(err) != cerrors.ExitConflict {
		t.Fatalf("ambiguity: %v", err)
	}
	details, _ := ce.Details.(map[string]any)
	if !reflect.DeepEqual(details["contexts"], []string{"personal", "second"}) {
		t.Fatalf("the conflict does not name its candidates: %#v", ce.Details)
	}
	after, _, _ := config.ReadFile(s.cfgDir)
	if !reflect.DeepEqual(file, after) {
		t.Fatal("an ambiguous reuse changed the config")
	}
	// A retry after a failed write keeps the chosen source, under its stored name.
	write := services.Write
	services.Write = func(string, config.File) error { return errors.New("read-only file system") }
	_, err = reuseAuthentication(s, "SECOND", false, services)
	ce = cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "AUTH_REUSE_WRITE_FAILED" || !reflect.DeepEqual(ce.NextSteps, []string{constants.AppName + " --use-context 'team' auth reuse --from-context 'second'"}) {
		t.Fatalf("the retry lost the chosen source: %+v", ce)
	}
	assertRunnableStep(t, ce.NextSteps[0])
	services.Write = write
	r, err := reuseAuthentication(s, "SECOND", false, services)
	if err != nil || r.SourceContext != "second" || r.State != "reused" {
		t.Fatalf("explicit choice: %+v %v", r, err)
	}
	after, _, _ = config.ReadFile(s.cfgDir)
	if target, _ := after.Context("team"); target.Auth.Username != "second@example.test" {
		t.Fatalf("the chosen identity was not the one saved: %+v", target)
	}
}

// Every command a reuse failure advertises must exist in this CLI. Each error
// is produced through the real function, then each step that starts with the
// binary name is resolved against the command tree.
func TestAuthReuseRecoveryNamesRealCommands(t *testing.T) {
	failures := map[string]error{}

	s, file, services := reuseFixture(t)
	_, failures["AUTH_REUSE_SOURCE_NOT_FOUND"] = reuseAuthentication(s, "missing", true, services)

	other := file.Contexts[0]
	other.Name = "second"
	other.Auth.Username = "second@example.test"
	ambiguous := file
	ambiguous.Contexts = append(append([]config.NamedContext(nil), file.Contexts...), other)
	if err := config.WriteFile(s.cfgDir, ambiguous); err != nil {
		t.Fatal(err)
	}
	_, failures["AUTH_REUSE_AMBIGUOUS"] = reuseAuthentication(s, "", true, services)

	mismatched := file
	mismatched.Contexts = append([]config.NamedContext(nil), file.Contexts...)
	mismatched.Contexts[0].BaseURL = "https://other.example.test/deploy"
	if err := config.WriteFile(s.cfgDir, mismatched); err != nil {
		t.Fatal(err)
	}
	_, failures["AUTH_REUSE_SOURCE_MISMATCH"] = reuseAuthentication(s, "personal", true, services)

	s, _, services = reuseFixture(t)
	s.resolved.Config.BaseURL = "https://service.example.test/other"
	_, failures["AUTH_REUSE_TARGET_MISMATCH"] = reuseAuthentication(s, "", true, services)

	s, _, services = reuseFixture(t)
	s.resolved.ActiveContext = "absent"
	_, failures["AUTH_REUSE_TARGET_MISSING"] = reuseAuthentication(s, "", true, services)

	s, _, services = reuseFixture(t)
	services.Verify = func(config.Config, auth.Credential) error {
		f, _, _ := config.ReadFile(s.cfgDir)
		f.CurrentContext = "team"
		return config.WriteFile(s.cfgDir, f)
	}
	_, failures["AUTH_REUSE_CONFIG_CHANGED"] = reuseAuthentication(s, "", false, services)

	s, _, services = reuseFixture(t)
	resolved := 0
	services.Resolve = func(cfg config.Config) (auth.Credential, error) {
		resolved++
		return auth.Credential{Scheme: cfg.Auth.Scheme, Username: cfg.Auth.Username, Secret: strings.Repeat("s", resolved)}, nil
	}
	_, failures["AUTH_REUSE_CREDENTIAL_CHANGED"] = reuseAuthentication(s, "", false, services)

	s, _, services = reuseFixture(t)
	services.Write = func(string, config.File) error { return errors.New("read-only file system") }
	_, failures["AUTH_REUSE_WRITE_FAILED"] = reuseAuthentication(s, "", false, services)

	for code, err := range failures {
		ce := cerrors.AsCLIError(err)
		if ce == nil || ce.Code != code {
			t.Fatalf("%s: got %v", code, err)
		}
		if len(ce.NextSteps) == 0 || ce.Hint == "" {
			t.Fatalf("%s: no recovery: %+v", code, ce)
		}
		for _, step := range ce.NextSteps {
			assertRunnableStep(t, step)
		}
		// None of these is a password problem, and a new CalDAV password
		// invalidates the previous one. No step may lead to acquiring one.
		for _, forbidden := range []string{"auth login", "auth guide", "config init"} {
			if strings.Contains(strings.Join(ce.NextSteps, "\n"), forbidden) {
				t.Errorf("%s: recovery leads to %q: %v", code, forbidden, ce.NextSteps)
			}
		}
		if !strings.Contains(ce.Hint, "do not issue a new") {
			t.Errorf("%s: the hint does not rule out a new password: %q", code, ce.Hint)
		}
	}
	for _, code := range []string{"AUTH_REUSE_SOURCE_NOT_FOUND", "AUTH_REUSE_SOURCE_MISMATCH", "AUTH_REUSE_AMBIGUOUS"} {
		if first := cerrors.AsCLIError(failures[code]).NextSteps[0]; first != constants.AppName+" config get-contexts" {
			t.Errorf("%s: source selection recovers through the context listing, got %q", code, first)
		}
	}
}

func TestAuthReuseDuplicateAliasesDoNotRequireChoice(t *testing.T) {
	s, file, services := reuseFixture(t)
	alias := file.Contexts[0]
	alias.Name = "alias"
	file.Contexts = append(file.Contexts, alias)
	if err := config.WriteFile(s.cfgDir, file); err != nil {
		t.Fatal(err)
	}
	r, err := reuseAuthentication(s, "", true, services)
	if err != nil || !r.Changed || r.SourceContext != "alias" {
		t.Fatalf("same native credential: %+v %v", r, err)
	}
}

// WeCom treats an email case-insensitively, and whoami and is_self compare it
// that way. Two spellings of one address are one identity, not a conflict.
func TestAuthReuseTreatsEmailCaseAsOneIdentity(t *testing.T) {
	s, file, services := reuseFixture(t)
	alias := file.Contexts[0]
	alias.Name = "legacy"
	alias.Auth.Username = "Member@Example.Test"
	file.Contexts = append(file.Contexts, alias)
	if err := config.WriteFile(s.cfgDir, file); err != nil {
		t.Fatal(err)
	}
	r, err := reuseAuthentication(s, "", false, services)
	if err != nil || r.State != "reused" || r.SourceContext != "legacy" {
		t.Fatalf("one identity in two spellings: %+v %v", r, err)
	}
	// The spelling written is the one that was verified.
	after, _, _ := config.ReadFile(s.cfgDir)
	if target, _ := after.Context("team"); target.Auth.Username != "Member@Example.Test" {
		t.Fatalf("saved an email other than the verified one: %+v", target)
	}
}

// Basic authentication needs the email, so a context without one has no
// identity to offer. It is dropped before any credential is read, whatever
// spelling its URL uses.
func TestAuthReuseIgnoresContextsWithoutAnIdentity(t *testing.T) {
	s, file, services := reuseFixture(t)
	file.Contexts[0].Auth.Username = ""
	respelled := file.Contexts[0]
	respelled.Name = "respelled"
	respelled.BaseURL = "https://SERVICE.example.test:443/deploy/"
	file.Contexts = append(file.Contexts, respelled)
	if err := config.WriteFile(s.cfgDir, file); err != nil {
		t.Fatal(err)
	}
	services.Resolve = noCredentialAccess(t, "read a credential for a context that has no identity")
	for _, from := range []string{"", "personal", "respelled"} {
		r, err := reuseAuthentication(s, from, true, services)
		if err != nil || r.State != "unavailable" || r.Changed || r.Verified || r.Reason == "" {
			t.Fatalf("from %q: %+v %v", from, r, err)
		}
	}
}

func TestAuthReuseRetainsOperationalFailures(t *testing.T) {
	for _, kind := range []string{"store", "missing", "network", "permission"} {
		t.Run(kind, func(t *testing.T) {
			s, before, services := reuseFixture(t)
			failure := cerrors.New(cerrors.CategoryNetwork, "NETWORK_ERROR", "network unavailable")
			switch kind {
			case "store":
				failure = cerrors.New(cerrors.CategoryConfig, "CREDENTIAL_STORE_INACCESSIBLE", "host keychain unavailable")
			case "missing":
				failure = cerrors.New(cerrors.CategoryConfig, "CREDENTIAL_NOT_VISIBLE_OR_MISSING", "not visible from this sandbox")
			case "permission":
				failure = cerrors.New(cerrors.CategoryPermission, "FORBIDDEN", "access denied").WithHTTPStatus(403)
			}
			if kind == "store" || kind == "missing" {
				services.Resolve = func(config.Config) (auth.Credential, error) { return auth.Credential{}, failure }
			} else {
				services.Verify = func(config.Config, auth.Credential) error { return failure }
			}
			_, err := reuseAuthentication(s, "", false, services)
			if !errors.Is(err, failure) {
				t.Fatalf("lost diagnostic: %v", err)
			}
			after, _, _ := config.ReadFile(s.cfgDir)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failure mutated config")
			}
		})
	}
}

// The same through the real store: a keychain that cannot be read keeps its
// host recovery. Reuse reads that store too, so the error offers neither a
// new credential nor another reuse.
func TestAuthReuseKeepsInaccessibleStoreRecoveryOnTheHost(t *testing.T) {
	s, before, services := reuseFixture(t)
	keyring.MockInitWithError(errors.New("keychain is locked"))
	t.Cleanup(keyring.MockInit)
	services.Resolve = func(cfg config.Config) (auth.Credential, error) {
		return auth.Resolve(cfg, config.Secrets{}, s.store)
	}
	services.Verify = func(config.Config, auth.Credential) error {
		t.Fatal("verified without a credential")
		return nil
	}
	_, err := reuseAuthentication(s, "", false, services)
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "CREDENTIAL_STORE_INACCESSIBLE" || ce.Recovery == nil || ce.Recovery.Scope != "host" {
		t.Fatalf("unexpected error: %+v", ce)
	}
	steps := strings.Join(ce.NextSteps, "\n")
	for _, forbidden := range []string{"auth guide", "auth reuse", "Credential page"} {
		if strings.Contains(steps, forbidden) {
			t.Fatalf("inaccessible-store recovery mentions %q:\n%s", forbidden, steps)
		}
	}
	if !strings.Contains(steps, "do not issue a new CalDAV password") {
		t.Fatalf("recovery must stay on the host:\n%s", steps)
	}
	after, _, _ := config.ReadFile(s.cfgDir)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failure mutated config")
	}
}

func TestAuthReuseExpiredIdentityIsUnavailable(t *testing.T) {
	s, _, services := reuseFixture(t)
	services.Verify = func(config.Config, auth.Credential) error {
		return cerrors.New(cerrors.CategoryAuth, "CALDAV_AUTH", "CalDAV server returned HTTP 401").WithHTTPStatus(401)
	}
	r, err := reuseAuthentication(s, "", true, services)
	if err != nil || r.State != "unavailable" || r.Changed || r.Verified || r.SourceContext != "" {
		t.Fatalf("expired credential: %+v %v", r, err)
	}
}

func TestAuthReuseSelfConfigurationAndConcurrentChange(t *testing.T) {
	s, _, services := reuseFixture(t)
	s.resolved.Config.Defaults.ReadOnly = true
	r, err := reuseAuthentication(s, "", true, services)
	if err != nil || !r.Changed {
		t.Fatal("read-only preview blocked", err)
	}
	r, err = reuseAuthentication(s, "", false, services)
	if err != nil || r.State != "reused" {
		t.Fatalf("native self-configuration should remain available: %+v %v", r, err)
	}
	// An edit made while the login is being verified stops the write, whether
	// it touches the destination's service or something unrelated.
	edits := map[string]func(*config.File){
		"current context":     func(f *config.File) { f.CurrentContext = "team" },
		"destination service": func(f *config.File) { f.Contexts[1].BaseURL = "https://moved.example.test/deploy" },
		"destination identity": func(f *config.File) {
			f.Contexts[1].Auth.Username = "someone@example.test"
		},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			s, _, services := reuseFixture(t)
			var edited config.File
			services.Verify = func(config.Config, auth.Credential) error {
				f, _, e := config.ReadFile(s.cfgDir)
				if e != nil {
					return e
				}
				edit(&f)
				edited = f
				return config.WriteFile(s.cfgDir, f)
			}
			r, err := reuseAuthentication(s, "", false, services)
			if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_CONFIG_CHANGED" || r.State == "reused" {
				t.Fatalf("concurrent change: %+v %v", r, err)
			}
			after, _, _ := config.ReadFile(s.cfgDir)
			if !reflect.DeepEqual(edited, after) {
				t.Fatal("reuse overwrote a concurrent edit")
			}
		})
	}
}

func TestAuthReuseRejectsTargetOverridesAndUnknownSource(t *testing.T) {
	s, _, services := reuseFixture(t)
	services.Resolve = noCredentialAccess(t, "read a credential before the destination was matched")
	s.resolved.Config.BaseURL = "https://service.example.test/other"
	_, err := reuseAuthentication(s, "", true, services)
	if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_TARGET_MISMATCH" {
		t.Fatal(err)
	}
	s.resolved.Config.BaseURL = "https://service.example.test/deploy"
	s.resolved.Config.Auth.Scheme = "pat"
	_, err = reuseAuthentication(s, "", true, services)
	if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_TARGET_MISMATCH" {
		t.Fatalf("a scheme override was accepted: %v", err)
	}
	s.resolved.Config.Auth.Scheme = "basic"
	_, err = reuseAuthentication(s, "missing", true, services)
	if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_SOURCE_NOT_FOUND" || cerrors.ExitCode(err) != cerrors.ExitNotFound {
		t.Fatal(err)
	}
	s.resolved.ActiveContext = ""
	_, err = reuseAuthentication(s, "", true, services)
	if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_TARGET_MISSING" {
		t.Fatalf("reuse without a stored destination: %v", err)
	}
}

func TestAuthReuseRejectsCredentialRotationBeforeAssociation(t *testing.T) {
	s, before, services := reuseFixture(t)
	loads := 0
	services.Resolve = func(cfg config.Config) (auth.Credential, error) {
		loads++
		secret := "verified-secret"
		if loads > 1 {
			secret = "rotated-secret"
		}
		return auth.Credential{Scheme: cfg.Auth.Scheme, Username: cfg.Auth.Username, Secret: secret}, nil
	}
	_, err := reuseAuthentication(s, "", false, services)
	if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_CREDENTIAL_CHANGED" {
		t.Fatalf("credential rotation: %v", err)
	}
	after, _, _ := config.ReadFile(s.cfgDir)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rotation changed destination")
	}
}

func TestAuthReuseUsesPersistedDefaultsWithoutEnvironmentIdentity(t *testing.T) {
	s, file, services := reuseFixture(t)
	// The source's scheme comes from the stored defaults, not from the file.
	file.Contexts[0].Auth.Scheme = ""
	if err := config.WriteFile(s.cfgDir, file); err != nil {
		t.Fatal(err)
	}
	// The runtime carries an identity and a password from the environment.
	s.resolved.Config.Auth.Username = "environment@example.test"
	s.resolved.Secrets = config.Secrets{Password: "environment-secret"}
	sourceResolver := services.Resolve
	services.Resolve = func(cfg config.Config) (auth.Credential, error) {
		if cfg.Auth.Username != "member@example.test" || cfg.Auth.Scheme != "basic" {
			t.Fatalf("resolved something other than the stored source: %+v", cfg.Auth)
		}
		return sourceResolver(cfg)
	}
	services.Verify = func(_ config.Config, cred auth.Credential) error {
		if cred.Username != "member@example.test" || cred.Secret != "test-secret" {
			t.Fatalf("verified an environment credential: %+v", cred.Redacted())
		}
		return nil
	}
	r, err := reuseAuthentication(s, "", false, services)
	if err != nil || r.State != "reused" {
		t.Fatalf("stored defaults: %+v %v", r, err)
	}
	after, _, _ := config.ReadFile(s.cfgDir)
	target, _ := after.Context("team")
	if target.Auth.Username != "member@example.test" {
		t.Fatal("identity did not come from stored source")
	}
}

// reuseAgainstStub stores the fixture's contexts on a stub server, with one
// password under the host key every context there shares.
func reuseAgainstStub(t *testing.T, stub *calDAVStub, secret string, contexts ...config.NamedContext) (string, config.File) {
	t.Helper()
	keyring.MockInit()
	cleanCredentialEnv(t)
	dir := t.TempDir()
	file := config.File{CurrentContext: contexts[0].Name, Contexts: contexts, Defaults: config.Defaults{Timeout: 5 * time.Second}}
	for i := range file.Contexts {
		file.Contexts[i].BaseURL = stub.URL + "/deploy"
		file.Contexts[i].Auth.Scheme = "basic"
	}
	if err := config.WriteFile(dir, file); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Save(stub.URL, auth.Credential{Scheme: "basic", Username: "any", Secret: secret}, auth.NewStore(dir)); err != nil {
		t.Fatal(err)
	}
	return dir, file
}

// The whole command against the calendar-home stub: reuse verifies with the
// PROPFIND auth login and doctor send, writes only the config file, and the
// completed context resolves the shared password in a fresh process. The JSON
// is the shape the Skill documents, so it is compared as printed.
func TestAuthReuseCommandVerifiesNativeStoredCredential(t *testing.T) {
	stub := newCalDAVStub(t, "member@example.test", "retained-secret")
	dir, file := reuseAgainstStub(t, stub, "retained-secret",
		config.NamedContext{Name: "personal", Auth: config.AuthConfig{Username: "member@example.test"}},
		config.NamedContext{Name: "team"})
	// Reuse takes nothing from the environment, neither identity nor secret.
	t.Setenv("WECOM_CALENDAR_USERNAME", "environment@example.test")
	t.Setenv("WECOM_CALENDAR_PASSWORD", "environment-secret")

	stdout, _, err := captureCLI(t, dir, "--use-context", "team", "auth", "reuse", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"changed\": true,\n  \"context\": \"team\",\n  \"dry_run\": true,\n  \"source_context\": \"personal\",\n  \"state\": \"available\",\n  \"verified\": true\n}\n"; stdout != want {
		t.Fatalf("preview output changed:\n%s", stdout)
	}
	if after, _, _ := config.ReadFile(dir); !reflect.DeepEqual(file, after) {
		t.Fatal("the preview wrote the config")
	}

	stdout, _, err = captureCLI(t, dir, "--use-context", "team", "auth", "reuse")
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"changed\": true,\n  \"context\": \"team\",\n  \"dry_run\": false,\n  \"source_context\": \"personal\",\n  \"state\": \"reused\",\n  \"verified\": true\n}\n"; stdout != want {
		t.Fatalf("result output changed:\n%s", stdout)
	}
	seen := stub.seen()
	if len(seen) != 2 {
		t.Fatalf("expected one verification per run, saw %v", seen)
	}
	for _, request := range seen {
		if request != "PROPFIND /calendar/" {
			t.Fatalf("verification left the native read-only path: %v", seen)
		}
	}
	after, _, _ := config.ReadFile(dir)
	target, _ := after.Context("team")
	if target.Auth.Username != "member@example.test" || after.CurrentContext != "personal" || !reflect.DeepEqual(after.Contexts[0], file.Contexts[0]) {
		t.Fatalf("reuse touched more than the destination identity: %+v", after)
	}
	// Nothing but the config file changed: no secret was copied into a second
	// store entry, a fallback file or the config.
	entries, _ := os.ReadDir(dir)
	raw, _ := os.ReadFile(config.ConfigFilePath(dir))
	if len(entries) != 1 || strings.Contains(string(raw), "retained-secret") {
		t.Fatalf("reuse wrote a secret somewhere: %v\n%s", entries, raw)
	}
	t.Setenv("WECOM_CALENDAR_USERNAME", "")
	t.Setenv("WECOM_CALENDAR_PASSWORD", "")
	got, _, err := freshProcessCredential(t, dir, "team")
	if err != nil || got.Username != "member@example.test" || got.Secret != "retained-secret" {
		t.Fatalf("native reuse unavailable after reload: %v", err)
	}

	// A second run finds the identity in place and reads nothing.
	stdout, _, err = captureCLI(t, dir, "--use-context", "team", "auth", "reuse")
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"changed\": false,\n  \"context\": \"team\",\n  \"dry_run\": false,\n  \"reason\": \"destination identity is already configured\",\n  \"state\": \"unchanged\",\n  \"verified\": false\n}\n"; stdout != want {
		t.Fatalf("repeat output changed:\n%s", stdout)
	}
	if len(stub.seen()) != 2 {
		t.Fatal("an unchanged reuse contacted the server")
	}
}

// Contexts on one server share one stored password, and a password belongs to
// one WeCom account. With two personal contexts beside a preset, verification
// is what tells which email that password still authenticates: the other one
// is rejected by the server and skipped, so there is nothing to choose.
func TestAuthReusePicksTheIdentityTheSharedPasswordBelongsTo(t *testing.T) {
	stub := newCalDAVStub(t, "current@example.test", "shared-secret")
	dir, _ := reuseAgainstStub(t, stub, "shared-secret",
		config.NamedContext{Name: "earlier", Auth: config.AuthConfig{Username: "earlier@example.test"}},
		config.NamedContext{Name: "current", Auth: config.AuthConfig{Username: "current@example.test"}},
		config.NamedContext{Name: "team"})

	// The context whose email the stored password does not authenticate has
	// nothing to offer. That is a normal no-change result, not an error.
	stdout, _, err := captureCLI(t, dir, "--use-context", "team", "auth", "reuse", "--from-context", "earlier")
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"changed\": false,\n  \"context\": \"team\",\n  \"dry_run\": false,\n  \"reason\": \"no matching stored identity can be reused\",\n  \"state\": \"unavailable\",\n  \"verified\": false\n}\n"; stdout != want {
		t.Fatalf("unavailable output changed:\n%s", stdout)
	}
	stdout, _, err = captureCLI(t, dir, "--use-context", "team", "auth", "reuse")
	if err != nil {
		t.Fatal(err)
	}
	if out := decodeJSON(t, stdout); out["state"] != "reused" || out["source_context"] != "current" {
		t.Fatalf("reuse did not follow the stored password: %v", out)
	}
	after, _, _ := config.ReadFile(dir)
	if target, _ := after.Context("team"); target.Auth.Username != "current@example.test" {
		t.Fatalf("destination got an identity the password does not authenticate: %+v", target)
	}
}

// There is no identity endpoint to read back, so the evidence is the 207 from
// the calendar home. An answer that proves nothing is an error the caller
// sees, never an `unavailable` that would send them to a new password.
func TestAuthReuseDoesNotHideAnAnswerThatIsNotCalDAV(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	s, file, services := reuseFixture(t)
	for i := range file.Contexts {
		file.Contexts[i].BaseURL = server.URL
	}
	if err := config.WriteFile(s.cfgDir, file); err != nil {
		t.Fatal(err)
	}
	s.resolved.Config = reuseContextConfig(file.Contexts[1], file.Defaults)
	s.resolved.Config.Defaults.Timeout = 5 * time.Second
	services.Verify = func(cfg config.Config, cred auth.Credential) error { return verifyCredential(s, cfg, cred) }
	r, err := reuseAuthentication(s, "", true, services)
	if err == nil || r.Verified || r.Changed || cerrors.AsCLIError(err).Category == cerrors.CategoryAuth {
		t.Fatalf("a response that is not CalDAV was accepted or hidden: %+v %v", r, err)
	}
	after, _, _ := config.ReadFile(s.cfgDir)
	if !reflect.DeepEqual(file, after) {
		t.Fatal("failure mutated config")
	}
}

func TestAuthReusePreservesLegacyStoreKeyUnderEquivalentOverride(t *testing.T) {
	s, file, services := reuseFixture(t)
	file.Contexts[0].BaseURL = "https://SERVICE.example.test:443/deploy/"
	if err := config.WriteFile(s.cfgDir, file); err != nil {
		t.Fatal(err)
	}
	store := s.store
	credential := auth.Credential{Scheme: "basic", Username: "member@example.test", Secret: "retained-secret"}
	if _, err := auth.Save(file.Contexts[0].BaseURL, credential, store); err != nil {
		t.Fatal(err)
	}
	services.Resolve = func(cfg config.Config) (auth.Credential, error) { return auth.Resolve(cfg, config.Secrets{}, store) }
	if _, err := reuseAuthentication(s, "", false, services); err != nil {
		t.Fatal(err)
	}
	// The destination adopts the source's spelling, because that spelling is
	// the store key the password is kept under. Nothing is saved a second time.
	after, _, _ := config.ReadFile(s.cfgDir)
	if target, _ := after.Context("team"); target.BaseURL != file.Contexts[0].BaseURL {
		t.Fatalf("destination cannot resolve the retained key: %+v", target)
	}
	if _, err := store.Load(auth.AccountKey("https://service.example.test/deploy", "basic")); !errors.Is(err, auth.ErrSecretNotFound) {
		t.Fatalf("the secret was copied under the destination's own key: %v", err)
	}
	fresh, err := config.Load(config.LoadOptions{ConfigDir: s.cfgDir, Context: "team", DotenvPath: filepath.Join(t.TempDir(), "absent"), Flags: config.FlagValues{BaseURL: "https://service.example.test/deploy"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := auth.Resolve(fresh.Config, config.Secrets{}, auth.NewStore(s.cfgDir))
	if err != nil || got.Secret != credential.Secret || got.Username != credential.Username {
		t.Fatalf("equivalent override lost native credential: %v", err)
	}
	if fresh.Config.BaseURL != "https://service.example.test/deploy" {
		t.Fatal("changed request destination")
	}
	if err := auth.ForgetForConfig(fresh.Config, fresh.Config.Auth.Scheme, store); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(auth.AccountKey(file.Contexts[0].BaseURL, credential.Scheme)); err == nil {
		t.Fatal("logout left the original credential active")
	}
	fresh.Config.BaseURL = "https://service.example.test/other"
	_, err = auth.Resolve(fresh.Config, config.Secrets{}, store)
	if err == nil || cerrors.AsCLIError(err).Code != "CREDENTIAL_SERVICE_MISMATCH" {
		t.Fatalf("foreign deployment reused stored key: %v", err)
	}
	for _, step := range cerrors.AsCLIError(err).NextSteps {
		assertRunnableStep(t, step)
	}
}

// The server URL has a default, so a destination or a source may store none.
// Both then resolve the public endpoint, and reuse neither invents a URL for
// the destination nor blanks it with a source that has none.
func TestAuthReuseFollowsTheDefaultEndpoint(t *testing.T) {
	for index, without := range []string{"source", "destination"} {
		t.Run(without+" stores no URL", func(t *testing.T) {
			s, file, services := reuseFixture(t)
			file.Contexts[0].BaseURL = "https://caldav.wecom.work"
			file.Contexts[1].BaseURL = "https://caldav.wecom.work"
			file.Contexts[index].BaseURL = ""
			if err := config.WriteFile(s.cfgDir, file); err != nil {
				t.Fatal(err)
			}
			s.resolved.Config = reuseContextConfig(file.Contexts[1], file.Defaults)
			credential := auth.Credential{Scheme: "basic", Username: "member@example.test", Secret: "retained-secret"}
			if _, err := auth.Save(constants.DefaultServerURL, credential, s.store); err != nil {
				t.Fatal(err)
			}
			services.Resolve = func(cfg config.Config) (auth.Credential, error) {
				return auth.Resolve(cfg, config.Secrets{}, s.store)
			}
			r, err := reuseAuthentication(s, "", false, services)
			if err != nil || r.State != "reused" {
				t.Fatalf("default endpoint: %+v %v", r, err)
			}
			after, _, _ := config.ReadFile(s.cfgDir)
			if target, _ := after.Context("team"); target.BaseURL != file.Contexts[1].BaseURL {
				t.Fatalf("reuse rewrote the destination's server: %q", target.BaseURL)
			}
			got, _, err := freshProcessCredential(t, s.cfgDir, "team")
			if err != nil || got != credential {
				t.Fatalf("fresh process cannot resolve the shared password: %+v %v", got, err)
			}
		})
	}
}

// Reuse is offered before a password wherever one could be asked for: the
// guide, the preset's next steps and a missing identity. Each step is the
// preview, names the selected context and resolves against the command tree.
func TestAuthReuseIsOfferedBeforeAcquiringAPassword(t *testing.T) {
	keyring.MockInit()
	cleanCredentialEnv(t)
	dir := filepath.Join(t.TempDir(), "config")
	personal := config.NamedContext{Name: "personal", BaseURL: constants.DefaultServerURL, Auth: config.AuthConfig{Scheme: "basic", Username: "member@example.test"}}
	if err := config.WriteFile(dir, config.File{CurrentContext: "personal", Contexts: []config.NamedContext{personal}}); err != nil {
		t.Fatal(err)
	}
	reuse := constants.AppName + " --use-context 'team' auth reuse --dry-run"

	stdout, _, err := captureCLI(t, dir, "config", "set-context", "team")
	if err != nil {
		t.Fatal(err)
	}
	plan := decodeJSON(t, stdout)
	steps, _ := plan["next_steps"].([]any)
	if len(steps) != 3 || steps[0] != reuse || steps[2] != constants.AppName+" --use-context 'team' auth login" {
		t.Fatalf("the preset does not lead with reuse: %v", steps)
	}

	stdout, _, err = captureCLI(t, dir, "--use-context", "team", "auth", "guide")
	if err != nil {
		t.Fatal(err)
	}
	guide := decodeJSON(t, stdout)
	steps, _ = guide["next_steps"].([]any)
	instructions, _ := guide["instructions"].([]any)
	if len(steps) != 2 || steps[0] != reuse || steps[1] != constants.AppName+" --use-context 'team' auth login" {
		t.Fatalf("the guide does not lead with reuse: %v", steps)
	}
	if first, _ := instructions[0].(string); !strings.Contains(first, "auth reuse") || !strings.Contains(first, "No password is asked for") {
		t.Fatalf("the guide does not explain reuse first: %v", instructions)
	}
	for _, step := range append(steps, plan["next_steps"].([]any)...) {
		assertRunnableStep(t, step.(string))
	}

	// The personal context has its email, so nothing there can be reused.
	stdout, _, err = captureCLI(t, dir, "--use-context", "personal", "auth", "guide")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout, "auth reuse") {
		t.Fatalf("reuse was offered to a context that has an identity:\n%s", stdout)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("setup or the guide touched the credential store: %v", entries)
	}

	// The preset shares the personal context's stored password and lacks only
	// the email. That is AUTH_NO_BASIC, and reuse is its first step.
	if _, err := auth.Save(personal.BaseURL, auth.Credential{Scheme: "basic", Username: personal.Auth.Username, Secret: "retained-secret"}, auth.NewStore(dir)); err != nil {
		t.Fatal(err)
	}
	_, resolved, err := freshProcessCredential(t, dir, "team")
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "AUTH_NO_BASIC" || !resolved.Config.MayReuse || ce.NextSteps[0] != config.ReuseStep {
		t.Fatalf("a missing identity beside a signed-in context: %+v", ce)
	}
	if last := ce.NextSteps[len(ce.NextSteps)-1]; last != constants.AppName+" auth guide" || !strings.Contains(ce.Hint, "none should be issued") {
		t.Fatalf("acquisition must stay behind reuse: %+v", ce)
	}
	for _, step := range ce.NextSteps {
		assertRunnableStep(t, step)
	}

	// doctor is the first diagnostic an agent runs. It names reuse before the
	// step that leads to configuring credentials, and never for a store it
	// could not read.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	configure := "Only configure credentials if the host retry also reports them missing."
	_, _, err = captureCLI(t, dir, "--use-context", "team", "doctor", "--no-update-check")
	ce = cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "DOCTOR_UNHEALTHY" || len(ce.NextSteps) != 4 || ce.NextSteps[2] != config.ReuseStep || ce.NextSteps[3] != configure {
		t.Fatalf("doctor does not offer reuse before credentials: %+v", ce)
	}
	keyring.MockInitWithError(errors.New("keychain is locked"))
	t.Cleanup(keyring.MockInit)
	_, _, err = captureCLI(t, dir, "--use-context", "team", "doctor", "--no-update-check")
	ce = cerrors.AsCLIError(err)
	if ce == nil || ce.Recovery == nil || ce.Recovery.Scope != "host" || len(ce.NextSteps) != 3 || ce.NextSteps[2] != configure {
		t.Fatalf("doctor sent an unreadable store to reuse: %+v", ce)
	}
}
