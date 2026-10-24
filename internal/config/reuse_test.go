package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/angelmsger/agent-cli-kit/reuse"
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

// The provider scope is the complete service URL. There is no flavor,
// organization or tenant to compare, and the scheme decides precedence, not
// the service. A context this CLI could not use identifies no service.
func TestReuseServiceIsTheCompleteURL(t *testing.T) {
	base := ReuseService(Config{BaseURL: "https://service.example.test/deploy", Auth: AuthConfig{Scheme: SchemeBasic}})
	if base == "" {
		t.Fatal("a usable context identifies no service")
	}
	same := []string{"https://service.example.test/deploy", "https://SERVICE.example.test:443/deploy/", "https://service.example.test/deploy/"}
	for _, url := range same {
		if got := ReuseService(Config{BaseURL: url, Auth: AuthConfig{Scheme: SchemeBasic, Username: "ignored@example.test"}}); got != base {
			t.Fatalf("%q is the same service: %q", url, got)
		}
	}
	different := []Config{
		{BaseURL: "https://other.example.test/deploy", Auth: AuthConfig{Scheme: SchemeBasic}},
		{BaseURL: "https://service.example.test/other", Auth: AuthConfig{Scheme: SchemeBasic}},
		{BaseURL: "https://service.example.test", Auth: AuthConfig{Scheme: SchemeBasic}},
		{BaseURL: "http://service.example.test/deploy", Auth: AuthConfig{Scheme: SchemeBasic}},
		{BaseURL: "https://service.example.test:8443/deploy", Auth: AuthConfig{Scheme: SchemeBasic}},
	}
	for _, other := range different {
		if ReuseService(other) == base {
			t.Fatalf("%+v was treated as the same service", other)
		}
	}
	for _, unusable := range []Config{
		{BaseURL: "https://service.example.test/deploy", Auth: AuthConfig{Scheme: "pat"}},
		{BaseURL: "", Auth: AuthConfig{Scheme: SchemeBasic}},
		{},
	} {
		if got := ReuseService(unusable); got != "" {
			t.Fatalf("%+v identified a service: %q", unusable, got)
		}
	}
}

// Contexts reach agent-cli-kit/reuse with only their stored fields. A context
// that stores no URL is on the public endpoint, and its credential key derives
// from that resolved URL, as it does when a command resolves it.
func TestReuseContextsDescribeTheStoredFile(t *testing.T) {
	cleanSetupEnv(t)
	t.Setenv("WECOM_CALENDAR_USERNAME", "injected@example.test")
	file := reuseFile()
	file.Contexts = append(file.Contexts, NamedContext{Name: "implicit", Auth: AuthConfig{Username: "other@example.test"}})
	got := ReuseContexts(file)
	public := ReuseService(Config{BaseURL: constants.DefaultServerURL, Auth: AuthConfig{Scheme: SchemeBasic}})
	want := []reuse.Context{
		{Name: "personal", Service: public, KeyBase: constants.DefaultServerURL, Scheme: SchemeBasic, Username: "member@example.test"},
		{Name: "team", Service: public, KeyBase: "https://caldav.wecom.work", Scheme: SchemeBasic},
		{Name: "elsewhere", Service: ReuseService(Config{BaseURL: "https://elsewhere.example.test/dav", Auth: AuthConfig{Scheme: SchemeBasic}}), KeyBase: "https://elsewhere.example.test/dav", Scheme: SchemeBasic, Username: "member@example.test"},
		{Name: "implicit", Service: public, KeyBase: constants.DefaultServerURL, Scheme: SchemeBasic, Username: "other@example.test"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("contexts:\n got %+v\nwant %+v", got, want)
	}
	if !SameUsername("Member@Example.Test", " member@example.test ") || SameUsername("member@example.test", "other@example.test") {
		t.Fatal("emails are compared the way whoami compares them")
	}
}

// Config.ReuseStep is what lets recovery offer reuse before a password. It is
// set only where `auth reuse` could change something and would not be refused,
// and it names the context, so it can be run as written.
func TestLoaderOffersReuseOnlyWhereItCouldCompleteTheContext(t *testing.T) {
	cleanSetupEnv(t)
	step := constants.AppName + " --use-context 'team' auth reuse"
	if got := loadReuse(t, reuseFile(), "team", FlagValues{}).ReuseStep; got != step {
		t.Fatalf("a preset beside a signed-in context was offered %q", got)
	}
	for _, context := range []string{"personal", "elsewhere"} {
		if got := loadReuse(t, reuseFile(), context, FlagValues{}).ReuseStep; got != "" {
			t.Fatalf("%s has an identity and was offered %q", context, got)
		}
	}
	// A service override that differs from the stored context is refused by
	// reuse (AUTH_REUSE_TARGET_MISMATCH), so it is not offered either.
	if got := loadReuse(t, reuseFile(), "team", FlagValues{BaseURL: "https://elsewhere.example.test/dav"}).ReuseStep; got != "" {
		t.Fatalf("reuse was offered across a service override: %q", got)
	}
	// Another spelling of the same service still qualifies, and keeps the
	// stored spelling for the credential lookup.
	cfg := loadReuse(t, reuseFile(), "team", FlagValues{BaseURL: "https://CALDAV.wecom.work:443/"})
	if cfg.ReuseStep != step || cfg.CredentialBaseURL != "https://caldav.wecom.work" || cfg.BaseURL != "https://CALDAV.wecom.work:443/" {
		t.Fatalf("an equivalent override changed the outcome: %+v", cfg)
	}
	// A scheme given by flag is kept on the step, so reuse stays within it.
	if got := loadReuse(t, reuseFile(), "team", FlagValues{AuthScheme: SchemeBasic}).ReuseStep; got != step+" --auth-scheme basic" {
		t.Fatalf("the supplied scheme was dropped: %q", got)
	}
	file := reuseFile()
	file.Contexts[0].Auth.Username = ""
	if got := loadReuse(t, file, "team", FlagValues{}).ReuseStep; got != "" {
		t.Fatalf("reuse was offered although no context has an identity: %q", got)
	}
	// An email supplied at runtime completes the invocation without reuse.
	t.Setenv("WECOM_CALENDAR_USERNAME", "environment@example.test")
	if got := loadReuse(t, reuseFile(), "team", FlagValues{}).ReuseStep; got != "" {
		t.Fatalf("reuse was offered although the environment supplies an identity: %q", got)
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
	cfg.ReuseStep = constants.AppName + " --use-context 'team' auth reuse"
	g, err := Guide(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.NextSteps, []string{cfg.ReuseStep, constants.AppName + " auth login"}) {
		t.Fatalf("reuse must precede login: %v", g.NextSteps)
	}
	if len(g.Instructions) != len(plain.Instructions)+1 || !reflect.DeepEqual(g.Instructions[1:], plain.Instructions) {
		t.Fatalf("the acquisition steps changed: %v", g.Instructions)
	}
	for _, want := range []string{"`" + cfg.ReuseStep + "`", "share one stored CalDAV password", "No password is asked for", "none is copied"} {
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
	plan := func(file File, name string, flags FlagValues) []string {
		t.Helper()
		dir := t.TempDir()
		if err := WriteFile(dir, file); err != nil {
			t.Fatal(err)
		}
		resolved, err := Load(LoadOptions{ConfigDir: dir, Context: name, Setup: true, DotenvPath: filepath.Join(dir, "absent"), Flags: flags})
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
	want := []string{selected + " auth reuse", selected + " auth guide", selected + " auth login"}
	if got := plan(file, "new", FlagValues{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("a new preset on a signed-in server: %v", got)
	}
	// The scheme a preset supplies becomes the context's own; it does not pin
	// the reuse step.
	if got := plan(file, "new", FlagValues{AuthScheme: SchemeBasic}); !reflect.DeepEqual(got, want) {
		t.Fatalf("a preset with a scheme: %v", got)
	}
	// Re-running setup on a context that has its email changes nothing here.
	selected = constants.AppName + " --use-context 'personal'"
	if got := plan(file, "personal", FlagValues{}); !reflect.DeepEqual(got, []string{selected + " auth guide", selected + " auth login"}) {
		t.Fatalf("a context with an identity: %v", got)
	}
	file.Contexts[0].Auth.Username = ""
	selected = constants.AppName + " --use-context 'new'"
	if got := plan(file, "new", FlagValues{}); !reflect.DeepEqual(got, []string{selected + " auth guide", selected + " auth login"}) {
		t.Fatalf("no context on that server has an identity: %v", got)
	}
}
