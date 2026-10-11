package config

import kit "github.com/angelmsger/agent-cli-kit/config"

// NormalizeContextName trims and lower-cases a context name about to be saved.
// Never apply it to a name being looked up: File.Context already ignores case,
// so legacy mixed-case contexts keep their spelling and keep resolving.
func NormalizeContextName(s string) string { return kit.NormalizeContextName(s) }

// NormalizeUsername lowercases an *email* username and otherwise preserves the
// caller's casing, trimming either way. WeCom CalDAV authenticates by email +
// app-specific password and treats the address case-insensitively, so this
// CLI opts in where a username is saved and where identities are compared.
func NormalizeUsername(s string) string { return kit.NormalizeUsername(s) }
