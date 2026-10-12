package config

import (
	kit "github.com/angelmsger/agent-cli-kit/config"
)

// FlagValues carries the global CLI flags that override configuration. Empty
// fields are ignored (not treated as overrides).
type FlagValues struct {
	AuthScheme    string `config:"auth.scheme"`
	CredentialURL string `config:"auth.credential_url"`
	BaseURL       string `config:"server"`
	Format        string `config:"defaults.format"`
	Timeout       string `config:"defaults.timeout"`
}

// LoadOptions controls where configuration is read from. All fields are
// optional; sensible defaults are used when empty.
type LoadOptions struct {
	// Setup resolves Context even when new, ignores runtime context selection,
	// and excludes personal environment fields before auth-scheme inference.
	Setup bool
	// ConfigDir overrides the directory containing config.yaml.
	ConfigDir string
	// DotenvPath overrides the .env file path. Empty means ".env".
	DotenvPath string
	// Flags carries global flag overrides (highest precedence).
	Flags FlagValues
	// Context selects a named context (from the --use-context flag). It wins
	// over WECOM_CALENDAR_CONTEXT and the file's current_context.
	Context string
}

// Load resolves configuration from all sources and returns the merged result
// with per-field provenance. Context selection is the flag,
// WECOM_CALENDAR_CONTEXT, the file's current_context, the sole context, then a
// context literally named "default"; naming a context that does not exist is
// UNKNOWN_CONTEXT. With no context selected (a missing file, or several
// contexts and no current_context) the default layer still supplies the
// public server URL.
func Load(opt LoadOptions) (*Resolved, error) {
	dir := opt.ConfigDir
	if dir == "" {
		d, err := ResolveConfigDir()
		if err != nil {
			return nil, err
		}
		dir = d
	}
	file, _, err := ReadFile(dir)
	if err != nil {
		return nil, err
	}
	r, err := kit.Load(file, kit.LoadOptions{
		Setup:      opt.Setup,
		DotenvPath: opt.DotenvPath,
		Flags:      kit.Encode(opt.Flags),
		Context:    opt.Context,
	})
	if err != nil {
		return nil, err
	}
	out := &Resolved{
		Sources:       r.Sources,
		ActiveContext: r.ActiveContext,
		ContextSource: r.ContextSource,
		ContextNames:  r.ContextNames,
	}
	kit.Decode(r.Values, &out.Config)
	kit.Decode(r.Values, &out.Secrets)
	out.Config.CredentialBaseURL = r.StoredServer
	out.Config.MayReuse = mayReuse(out.Config, file, r.ActiveContext)
	return out, nil
}

// ExplainField returns a human-readable provenance label for a field key,
// e.g. ExplainField(sources, "server") -> "env". Unknown fields report "default".
func ExplainField(sources map[string]string, field string) string {
	return kit.ExplainField(sources, field)
}

// UnknownContextHint builds the hint shown when a context override (flag,
// env, current_context, or an argument naming a context) does not exist: the
// available names, else a pointer to get-contexts.
func UnknownContextHint(name string, available []string) string {
	return kit.UnknownContextHint(name, available)
}

// ListContextsStep is the command that lists contexts, as a next step.
func ListContextsStep() string { return kit.ListContextsStep() }

// UseContext makes the named context current and returns its stored name, or
// UNKNOWN_CONTEXT when there is no such context.
func UseContext(f *File, name string) (string, error) { return kit.UseContext(f, name) }

// DeleteContext removes the named context and returns it; the first remaining
// context becomes current when it was. It returns UNKNOWN_CONTEXT or
// LAST_CONTEXT instead when the name is unknown or the context is the only one.
func DeleteContext(f *File, name string) (NamedContext, error) { return kit.DeleteContext(f, name) }

// ReminderBlock is the --help block shown when several contexts exist.
func ReminderBlock(active, source string, names []string) string {
	return kit.ReminderBlock(active, source, names)
}

// ContextNotice is the _notice payload for a command that runs against one of
// several contexts chosen implicitly.
func ContextNotice(active, source string, names []string) map[string]any {
	return kit.ContextNotice(active, source, names)
}

// Field key accessors for callers outside this package (e.g. config show).
const (
	FieldServer     = fieldServer
	FieldAuthScheme = fieldAuthScheme
	FieldAuthUser   = fieldAuthUsername
	FieldFormat     = fieldFormat
	FieldTimeout    = fieldTimeout
	FieldPageSize   = fieldPageSize
	FieldMaxRetries = fieldMaxRetries
	FieldReadOnly   = fieldReadOnly
)
