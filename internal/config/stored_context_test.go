package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
)

// reuseFile is the layout `auth reuse` exists for: a personal context that is
// signed in, a team preset beside it on the same server, and a context on
// another server. The preset spells the URL the way `config set-context`
// writes it; the personal context spells it like the wizard default.
func reuseFile() File {
	return File{CurrentContext: "personal", Contexts: []NamedContext{
		{Name: "personal", BaseURL: constants.DefaultServerURL, Auth: AuthConfig{Scheme: SchemeBasic, Username: "member@example.test"}},
		{Name: "team", BaseURL: "https://caldav.wecom.work", Auth: AuthConfig{Scheme: SchemeBasic}},
		{Name: "elsewhere", BaseURL: "https://elsewhere.example.test/dav", Auth: AuthConfig{Scheme: SchemeBasic, Username: "member@example.test"}},
	}}
}

func loadReuse(t *testing.T, file File, context string, flags FlagValues) Config {
	t.Helper()
	dir := t.TempDir()
	if err := WriteFile(dir, file); err != nil {
		t.Fatal(err)
	}
	resolved, err := Load(LoadOptions{ConfigDir: dir, Context: context, DotenvPath: filepath.Join(dir, "absent"), Flags: flags})
	if err != nil {
		t.Fatal(err)
	}
	return resolved.Config
}

// Reuse compares what the file stores. A stored context resolves through the
// built-in defaults and the shared defaults only, never through the
// environment of the invocation that happens to be running.
func TestStoredContextIgnoresTheEnvironment(t *testing.T) {
	cleanSetupEnv(t)
	t.Setenv("WECOM_CALENDAR_SERVER", "https://env.example.test")
	t.Setenv("WECOM_CALENDAR_USERNAME", "injected@example.test")
	t.Setenv("WECOM_CALENDAR_PASSWORD", "must-not-be-used")
	cfg := StoredContext(NamedContext{Name: "team"}, Defaults{Format: "table"})
	if cfg.BaseURL != constants.DefaultServerURL || cfg.Auth.Scheme != SchemeBasic || cfg.Auth.Username != "" || cfg.Defaults.Format != "table" || cfg.Defaults.MaxRetries != constants.DefaultMaxRetries {
		t.Fatalf("a stored context resolved from something other than the file and the defaults: %+v", cfg)
	}
	cfg = StoredContext(reuseFile().Contexts[2], Defaults{})
	if cfg.BaseURL != "https://elsewhere.example.test/dav" || cfg.Auth.Username != "member@example.test" || cfg.MayReuse || cfg.CredentialBaseURL != "" {
		t.Fatalf("stored fields were not kept as written: %+v", cfg)
	}
}

// The provider scope is the complete service URL and the scheme. There is no
// flavor, organization or tenant to compare.
func TestSameServiceComparesTheCompleteURLAndTheScheme(t *testing.T) {
	base := Config{BaseURL: "https://service.example.test/deploy", Auth: AuthConfig{Scheme: SchemeBasic}}
	same := []string{"https://service.example.test/deploy", "https://SERVICE.example.test:443/deploy/", "https://service.example.test/deploy/"}
	for _, url := range same {
		if !SameService(base, Config{BaseURL: url, Auth: AuthConfig{Scheme: SchemeBasic, Username: "ignored@example.test"}}) {
			t.Fatalf("%q is the same service", url)
		}
	}
	different := []Config{
		{BaseURL: "https://other.example.test/deploy", Auth: AuthConfig{Scheme: SchemeBasic}},
		{BaseURL: "https://service.example.test/other", Auth: AuthConfig{Scheme: SchemeBasic}},
		{BaseURL: "https://service.example.test", Auth: AuthConfig{Scheme: SchemeBasic}},
		{BaseURL: "http://service.example.test/deploy", Auth: AuthConfig{Scheme: SchemeBasic}},
		{BaseURL: "https://service.example.test:8443/deploy", Auth: AuthConfig{Scheme: SchemeBasic}},
		{BaseURL: "https://service.example.test/deploy", Auth: AuthConfig{Scheme: "pat"}},
		{BaseURL: "", Auth: AuthConfig{Scheme: SchemeBasic}},
	}
	for _, other := range different {
		if SameService(base, other) || SameService(other, base) {
			t.Fatalf("%+v was treated as the same service", other)
		}
	}
	if SameService(Config{}, Config{}) {
		t.Fatal("two configurations without a URL are not a service")
	}
}

func TestReuseSourcesAreSameServiceContextsThatCarryAnIdentity(t *testing.T) {
	file := reuseFile()
	names := func(sources []NamedContext) []string {
		out := []string{}
		for _, c := range sources {
			out = append(out, c.Name)
		}
		return out
	}
	if got := names(ReuseSources(file, "TEAM")); !reflect.DeepEqual(got, []string{"personal"}) {
		t.Fatalf("sources for the preset: %v", got)
	}
	// A context that has its email is complete; nothing is reused into it.
	if got := ReuseSources(file, "personal"); got != nil {
		t.Fatalf("sources for a context with an identity: %v", names(got))
	}
	if got := ReuseSources(file, "absent"); got != nil {
		t.Fatalf("sources for an undefined context: %v", names(got))
	}
	// A second preset has no email to offer, on any spelling of the server.
	file.Contexts = append(file.Contexts, NamedContext{Name: "second", BaseURL: "https://CALDAV.wecom.work:443/", Auth: AuthConfig{Scheme: SchemeBasic}})
	if got := names(ReuseSources(file, "team")); !reflect.DeepEqual(got, []string{"personal"}) {
		t.Fatalf("a context without an identity became a source: %v", got)
	}
	// The server URL has a default, so a context that stores none is on the
	// public endpoint like the others.
	file.Contexts = append(file.Contexts, NamedContext{Name: "implicit", Auth: AuthConfig{Username: "other@example.test"}})
	if got := names(ReuseSources(file, "team")); !reflect.DeepEqual(got, []string{"personal", "implicit"}) {
		t.Fatalf("a context on the default endpoint was not matched: %v", got)
	}
	file.Contexts[0].Auth.Scheme = "pat"
	if got := names(ReuseSources(file, "team")); !reflect.DeepEqual(got, []string{"implicit"}) {
		t.Fatalf("a context with another scheme became a source: %v", got)
	}
}

// Config.MayReuse is what lets recovery offer reuse before a password. It is
// set only where `auth reuse` could change something and would not be refused.
func TestLoaderOffersReuseOnlyWhereItCouldCompleteTheContext(t *testing.T) {
	cleanSetupEnv(t)
	if !loadReuse(t, reuseFile(), "team", FlagValues{}).MayReuse {
		t.Fatal("a preset beside a signed-in context was not offered reuse")
	}
	for _, context := range []string{"personal", "elsewhere"} {
		if loadReuse(t, reuseFile(), context, FlagValues{}).MayReuse {
			t.Fatalf("%s has an identity and was offered reuse", context)
		}
	}
	// A service override that differs from the stored context is refused by
	// reuse (AUTH_REUSE_TARGET_MISMATCH), so it is not offered either.
	if loadReuse(t, reuseFile(), "team", FlagValues{BaseURL: "https://elsewhere.example.test/dav"}).MayReuse {
		t.Fatal("reuse was offered across a service override")
	}
	// Another spelling of the same service still qualifies, and keeps the
	// stored spelling for the credential lookup.
	cfg := loadReuse(t, reuseFile(), "team", FlagValues{BaseURL: "https://CALDAV.wecom.work:443/"})
	if !cfg.MayReuse || cfg.CredentialBaseURL != "https://caldav.wecom.work" || cfg.BaseURL != "https://CALDAV.wecom.work:443/" {
		t.Fatalf("an equivalent override changed the outcome: %+v", cfg)
	}
	file := reuseFile()
	file.Contexts[0].Auth.Username = ""
	if loadReuse(t, file, "team", FlagValues{}).MayReuse {
		t.Fatal("reuse was offered although no context has an identity")
	}
	// An email supplied at runtime completes the invocation without reuse.
	t.Setenv("WECOM_CALENDAR_USERNAME", "environment@example.test")
	if loadReuse(t, reuseFile(), "team", FlagValues{}).MayReuse {
		t.Fatal("reuse was offered although the environment supplies an identity")
	}
}

func TestGuideLeadsWithReuseOnlyWhenOffered(t *testing.T) {
	cfg := Config{BaseURL: constants.DefaultServerURL, Auth: AuthConfig{Scheme: SchemeBasic}}
	plain, err := Guide(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plain.NextSteps, []string{constants.AppName + " auth login"}) || strings.Contains(strings.Join(plain.Lines(), "\n"), "auth reuse") {
		t.Fatalf("reuse was offered without a source: %+v", plain)
	}
	cfg.MayReuse = true
	g, err := Guide(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.NextSteps, []string{ReuseStep, constants.AppName + " auth login"}) {
		t.Fatalf("reuse must precede login: %v", g.NextSteps)
	}
	if len(g.Instructions) != len(plain.Instructions)+1 || !reflect.DeepEqual(g.Instructions[1:], plain.Instructions) {
		t.Fatalf("the acquisition steps changed: %v", g.Instructions)
	}
	for _, want := range []string{"auth reuse --dry-run", "share one stored CalDAV password", "No password is asked for", "none is copied"} {
		if !strings.Contains(g.Instructions[0], want) {
			t.Fatalf("the reuse instruction does not say %q: %s", want, g.Instructions[0])
		}
	}
	// Login and the wizards print the same lines, reuse first.
	if lines := g.Lines(); !strings.Contains(lines[1], "auth reuse") {
		t.Fatalf("the printed guide does not lead with reuse: %v", lines)
	}
}

// `config set-context` stays offline and credential-free; it reads only the
// planned file to decide what to suggest next.
func TestPresetLeadsWithReuseWhenAnotherContextHasAnIdentity(t *testing.T) {
	cleanSetupEnv(t)
	plan := func(file File, name string) []string {
		t.Helper()
		dir := t.TempDir()
		if err := WriteFile(dir, file); err != nil {
			t.Fatal(err)
		}
		resolved, err := Load(LoadOptions{ConfigDir: dir, Context: name, Setup: true, DotenvPath: filepath.Join(dir, "absent")})
		if err != nil {
			t.Fatal(err)
		}
		p, err := PlanServiceContext(file, name, resolved, false, false)
		if err != nil {
			t.Fatal(err)
		}
		return p.NextSteps
	}
	selected := constants.AppName + " --use-context 'new'"
	file := reuseFile()
	if got := plan(file, "new"); !reflect.DeepEqual(got, []string{selected + " auth reuse --dry-run", selected + " auth guide", selected + " auth login"}) {
		t.Fatalf("a new preset on a signed-in server: %v", got)
	}
	// Re-running setup on a context that has its email changes nothing here.
	selected = constants.AppName + " --use-context 'personal'"
	if got := plan(file, "personal"); !reflect.DeepEqual(got, []string{selected + " auth guide", selected + " auth login"}) {
		t.Fatalf("a context with an identity: %v", got)
	}
	file.Contexts[0].Auth.Username = ""
	selected = constants.AppName + " --use-context 'new'"
	if got := plan(file, "new"); !reflect.DeepEqual(got, []string{selected + " auth guide", selected + " auth login"}) {
		t.Fatalf("no context on that server has an identity: %v", got)
	}
}
