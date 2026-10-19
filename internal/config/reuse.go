package config

import (
	"github.com/angelmsger/agent-cli-kit/reuse"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
)

// ReuseContexts describes every stored context to agent-cli-kit/reuse with
// only its persisted fields and the built-in defaults, so no environment
// identity leaks into a source. A context that stores no server URL resolves
// the public endpoint, and that resolved URL is the spelling its credential key
// derives from.
func ReuseContexts(file File) []reuse.Context {
	out := make([]reuse.Context, 0, len(file.Contexts))
	for _, c := range file.Contexts {
		out = append(out, reuseContext(c, file.Defaults))
	}
	return out
}

func reuseContext(c NamedContext, defaults Defaults) reuse.Context {
	cfg := StoredContext(c, defaults)
	return reuse.Context{Name: c.Name, Service: ReuseService(cfg), KeyBase: cfg.BaseURL,
		Scheme: cfg.Auth.Scheme, Username: c.Auth.Username}
}

// ReuseService identifies a service for reuse: the complete normalized URL.
// WeCom CalDAV has no deployment flavor, organization or tenant, so that is the
// whole scope. A context this CLI could not use — an unusable URL, or a scheme
// other than basic — identifies nothing, so it is neither a source nor a
// destination.
func ReuseService(cfg Config) string {
	if ValidateService(Config{BaseURL: cfg.BaseURL, Auth: AuthConfig{Scheme: cfg.Auth.Scheme}}) != nil {
		return ""
	}
	base, err := NormalizeServiceURL(cfg.BaseURL)
	if err != nil {
		return ""
	}
	return base
}

// SameUsername compares WeCom emails the way whoami and is_self do: one
// address in two spellings is one identity.
func SameUsername(a, b string) bool { return NormalizeUsername(a) == NormalizeUsername(b) }

// SchemeSupplied reports whether the invocation supplied the scheme by flag,
// environment or .env, including one a password in the environment implies.
func SchemeSupplied(sources map[string]string) bool {
	switch sources[fieldAuthScheme] {
	case "flag", "env", "dotenv":
		return true
	}
	return false
}

// reuseStep is the `auth reuse` step that goes before any step leading to a
// CalDAV password for the named context, or "" when an offline look at the
// file finds nothing reuse could adopt. A WeCom email supplied at runtime, or a
// service override that differs from the stored context, means reuse would
// change nothing or be refused, so neither is offered.
func reuseStep(cfg Config, sources map[string]string, file File, name string) string {
	stored, ok := file.Context(name)
	if !ok {
		return ""
	}
	target := reuseContext(stored, file.Defaults)
	if target.Service != ReuseService(cfg) || target.Scheme != cfg.Auth.Scheme {
		return ""
	}
	runtimeUser := ""
	switch sources[fieldAuthUsername] {
	case "flag", "env", "dotenv":
		runtimeUser = cfg.Auth.Username
	}
	if !reuse.MayReuse(target, ReuseContexts(file), runtimeUser, SchemeSupplied(sources), validUnder(StoredContext(stored, file.Defaults))) {
		return ""
	}
	step := constants.AppName + " --use-context " + QuoteContextArg(stored.Name) + " auth reuse"
	if sources[fieldAuthScheme] == "flag" {
		step += " --auth-scheme " + cfg.Auth.Scheme
	}
	return step
}

// validUnder reports whether the context stays a valid configuration under a
// scheme, so reuse is not offered for a scheme its deployment rejects.
func validUnder(cfg Config) func(string) bool {
	return func(scheme string) bool {
		c := cfg
		c.Auth.Scheme = scheme
		return ValidateService(c) == nil
	}
}
