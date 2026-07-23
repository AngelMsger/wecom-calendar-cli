package config

import "strings"

// NormalizeContextName is the canonical form used to store and look up context
// names. Names are trimmed and lower-cased: kubectl-style conventions are
// universally lowercase, and case-sensitive names create silent footguns —
// `--use-context Work` against a file containing `work` should not be a
// hard error. Lookups remain case-insensitive (see File.Context) so legacy
// mixed-case configs keep working until they are next re-saved.
func NormalizeContextName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// NormalizeUsername lowercases an *email* username and otherwise preserves the
// caller's casing. WeCom CalDAV authenticates by email + app-specific password
// and treats the address case-insensitively (every mail provider does in
// practice). The `@` heuristic is good enough: emails always contain it.
// Trimming runs in both branches because trailing whitespace in a copy-pasted
// email is a surprisingly common mistake.
func NormalizeUsername(s string) string {
	s = strings.TrimSpace(s)
	if strings.ContainsRune(s, '@') {
		return strings.ToLower(s)
	}
	return s
}
