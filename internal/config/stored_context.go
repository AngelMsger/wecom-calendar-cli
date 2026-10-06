package config

import "strings"

// StoredContext resolves only persisted context data and built-in defaults.
// Credential reuse must not inherit another context's environment or .env identity.
func StoredContext(context NamedContext, defaults Defaults) Config {
	values := defaultLayer()
	sources := make(map[string]string, len(values))
	for key := range values {
		sources[key] = "default"
	}
	for key, value := range buildFileLayer(File{Contexts: []NamedContext{context}, Defaults: defaults}, context.Name) {
		values[key] = value
		sources[key] = "file"
	}
	resolveAuthDefaults(values, sources)
	return configFromMap(values)
}

// Equivalent flag spellings must not orphan a credential kept under a legacy key.
func preserveCredentialLookup(cfg Config, file File, context string) Config {
	stored, ok := file.Context(context)
	if !ok || stored.BaseURL == cfg.BaseURL {
		return cfg
	}
	left, e1 := NormalizeServiceURL(stored.BaseURL)
	right, e2 := NormalizeServiceURL(cfg.BaseURL)
	if e1 == nil && e2 == nil && left == right {
		cfg.CredentialBaseURL = stored.BaseURL
	}
	return cfg
}

// SameService reports whether two configurations address one service: the same
// complete normalized URL and the same scheme. WeCom CalDAV has no deployment
// flavor, organization or tenant, so that is the whole provider scope.
func SameService(a, b Config) bool {
	left, e1 := NormalizeServiceURL(a.BaseURL)
	right, e2 := NormalizeServiceURL(b.BaseURL)
	return e1 == nil && e2 == nil && left == right && a.Auth.Scheme == b.Auth.Scheme
}

// ReuseSources lists the stored contexts `auth reuse` may take an identity from
// for the named context: every other context on the same service that carries
// a WeCom email. It is empty when the named context is absent or already has
// an email, because reuse only fills a missing identity.
//
// Basic authentication always needs the email, so a context without one has no
// identity to offer and is never a source. Only the file is consulted — no
// credential, no network — which lets setup, the guide and missing-credential
// recovery point at reuse before anyone issues a new CalDAV password.
func ReuseSources(file File, name string) []NamedContext {
	target, ok := file.Context(name)
	if !ok || target.Auth.Username != "" {
		return nil
	}
	targetCfg := StoredContext(target, file.Defaults)
	var sources []NamedContext
	for _, source := range file.Contexts {
		if strings.EqualFold(source.Name, target.Name) || source.Auth.Username == "" {
			continue
		}
		if SameService(targetCfg, StoredContext(source, file.Defaults)) {
			sources = append(sources, source)
		}
	}
	return sources
}

// mayReuse decides Config.MayReuse for the context in effect. A WeCom email
// supplied at runtime, or a service override that differs from the stored
// context, means reuse would change nothing or be refused, so neither is offered.
func mayReuse(cfg Config, file File, context string) bool {
	stored, ok := file.Context(context)
	if !ok || cfg.Auth.Username != "" || !SameService(StoredContext(stored, file.Defaults), cfg) {
		return false
	}
	return len(ReuseSources(file, stored.Name)) > 0
}
