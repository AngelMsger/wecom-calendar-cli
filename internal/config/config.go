// Package config resolves CLI configuration from layered sources: CLI flags,
// environment variables, a .env file, a YAML config file and built-in
// defaults, in that precedence order (highest first).
//
// The engine — layering, provenance, context selection, the atomic file
// write and team presets — comes from agent-cli-kit/config. This package
// registers wecom-calendar-cli's fields, environment variables, file location
// and command wording with it (schema.go), keeps the YAML shape of the file
// and its legacy flat form (file.go), how `auth reuse` sees the stored
// contexts (reuse.go), the acquisition guide (setup.go) and the wizards, and
// keeps the names the rest of the repo uses.
//
// Secrets (the app-specific CalDAV password) are never stored in the YAML
// config file. They are surfaced through Resolved.Secrets when supplied via
// flags/env/.env, or loaded from the OS keychain by the auth package.
package config

import (
	"time"

	kit "github.com/angelmsger/agent-cli-kit/config"
)

// Auth scheme values. WeCom CalDAV only speaks HTTP Basic (email +
// app-specific password), so basic is the sole scheme. The constant is kept
// for structural parity with sibling CLIs; it is always "basic".
const (
	SchemeBasic = "basic"
)

// DefaultContextName is the name given to an unnamed context, and to the
// single context synthesized from a legacy flat config file.
const DefaultContextName = kit.DefaultContextName

// Context selection sources, reported on Resolved.ContextSource. The first two
// are explicit (the caller named a context for this invocation); the rest are
// implicit (the CLI fell back to a stored or sole context).
const (
	ContextSourceFlag    = kit.ContextSourceFlag    // --use-context
	ContextSourceEnv     = kit.ContextSourceEnv     // WECOM_CALENDAR_CONTEXT
	ContextSourceCurrent = kit.ContextSourceCurrent // the file's current_context
	ContextSourceSingle  = kit.ContextSourceSingle  // the sole defined context
	ContextSourceDefault = kit.ContextSourceDefault // a context literally named "default"
	ContextSourceNone    = kit.ContextSourceNone    // nothing selected (no/empty config)
)

// NamedContext is one named CalDAV server profile inside the config file.
// Runtime defaults are shared across contexts and live in File.Defaults.
type NamedContext struct {
	Name    string `config:"name"`
	BaseURL string `config:"server"`
	Auth    AuthConfig
}

// Config holds the resolved, non-secret configuration.
type Config struct {
	// CredentialBaseURL preserves an equivalent stored URL's native lookup key.
	// It is runtime-only and never changes the request destination.
	CredentialBaseURL string `yaml:"-" json:"-"`
	// ReuseStep is the `auth reuse` command that goes before any step leading
	// to a CalDAV password for this context, or "" when the file offers nothing
	// to reuse. It is set when the context has no WeCom email and another
	// stored context on the same service has one. It is runtime-only and
	// unverified — the loader reads the config file, never a credential — and
	// exists because a new password would invalidate the previous one.
	ReuseStep string     `yaml:"-" json:"-"`
	BaseURL   string     `yaml:"server" config:"server"`
	Auth      AuthConfig `yaml:"auth"`
	Defaults  Defaults   `yaml:"defaults"`
}

// AuthConfig holds non-secret auth settings. CredentialURL is an optional,
// display-only page a team publishes about obtaining the CalDAV password; the
// CLI never requests it.
type AuthConfig struct {
	CredentialURL string `yaml:"credential_url,omitempty" config:"auth.credential_url"`
	Scheme        string `yaml:"scheme" config:"auth.scheme"`
	Username      string `yaml:"username,omitempty" config:"auth.username"`
}

// Defaults holds tunable runtime defaults.
type Defaults struct {
	Format     string        `yaml:"format" config:"defaults.format"`
	PageSize   int           `yaml:"page_size" config:"defaults.page_size"`
	Timeout    time.Duration `yaml:"timeout" config:"defaults.timeout"`
	MaxRetries int           `yaml:"max_retries" config:"defaults.max_retries"`
	// ReadOnly blocks every mutating client method. Settable from the config
	// file, from WECOM_CALENDAR_CLI_READ_ONLY, or temporarily overridden via
	// --allow-writes.
	ReadOnly bool `yaml:"read_only,omitempty" config:"defaults.read_only"`
}

// Secrets holds credentials observed in non-file layers. An empty Password
// means the secret was not supplied via flags/env/.env and must come from the
// keychain.
type Secrets struct {
	Password string `config:"secret.password"`
}

// Resolved is the outcome of Load: the merged Config plus provenance and any
// transient secrets.
type Resolved struct {
	Config  Config
	Secrets Secrets
	// Sources maps a field key (see the field* constants) to the layer name
	// that supplied its final value: "flag", "env", "dotenv", "file", "default".
	Sources map[string]string
	// ActiveContext is the name of the context whose fields were applied.
	// Empty when no config file (or no contexts) exists — pure-env usage.
	ActiveContext string
	// ContextSource records which precedence rule chose ActiveContext (one of
	// the ContextSource* constants), so callers can tell an explicit choice
	// (flag/env) from an implicit fallback (current_context, sole, default).
	ContextSource string
	// ContextNames lists every context defined in the file, in file order.
	ContextNames []string
}

// ContextSelectedExplicitly reports whether the active context was chosen for
// this invocation (via --use-context or WECOM_CALENDAR_CONTEXT) rather than
// fallen back to from the file's current_context, the sole context, or a
// "default" context. It is the signal used to decide whether to nudge an agent
// that may not realise which of several contexts it is hitting.
func (r *Resolved) ContextSelectedExplicitly() bool {
	return r.ContextSource == ContextSourceFlag || r.ContextSource == ContextSourceEnv
}

// Field keys used for layer maps and provenance tracking.
const (
	fieldCredentialURL = kit.KeyCredentialURL
	fieldServer        = kit.KeyServer
	fieldAuthScheme    = kit.KeyAuthScheme
	fieldAuthUsername  = "auth.username"
	fieldFormat        = "defaults.format"
	fieldPageSize      = "defaults.page_size"
	fieldTimeout       = "defaults.timeout"
	fieldMaxRetries    = "defaults.max_retries"
	fieldReadOnly      = "defaults.read_only"
	// Secret field key (never persisted to the YAML file).
	fieldPassword = "secret.password"
)

// defaultConfig is the configuration the built-in defaults alone resolve to.
func defaultConfig() Config {
	var c Config
	kit.Decode(kit.DefaultValues(), &c)
	return c
}

// contextConfig builds a Config from a NamedContext plus the built-in runtime
// defaults. It is used to feed the wizard's validate hook, which operates on a
// whole Config.
func contextConfig(nc NamedContext) Config {
	c := defaultConfig()
	c.BaseURL = nc.BaseURL
	c.Auth = nc.Auth
	return c
}

// FieldCredentialURL is the provenance key for the acquisition page.
const FieldCredentialURL = fieldCredentialURL
