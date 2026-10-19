package config

import (
	kit "github.com/angelmsger/agent-cli-kit/config"
)

// StoredContext resolves only persisted context data and built-in defaults.
// Credential reuse must not inherit another context's environment or .env identity.
func StoredContext(context NamedContext, defaults Defaults) Config {
	var cfg Config
	kit.Decode(kit.StoredContext(context, defaults), &cfg)
	return cfg
}
