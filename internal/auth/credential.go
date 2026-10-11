// Package auth declares wecom-calendar-cli's auth scheme and resolves WeCom
// CalDAV credentials from configuration or secure storage. The credential
// model, header encoding and recovery errors come from agent-cli-kit/auth;
// this package registers WeCom Calendar's scheme and wording with it.
package auth

import (
	kit "github.com/angelmsger/agent-cli-kit/auth"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
)

// Scheme identifies an authentication scheme. WeCom CalDAV only speaks HTTP
// Basic (email + app-specific password); the constant is kept for structural
// parity with sibling CLIs and is always "basic".
const (
	SchemeBasic = "basic"
)

func init() {
	kit.Register(&kit.Product{
		Schemes: []kit.Scheme{
			{Name: SchemeBasic, Kind: kit.Basic,
				Missing: "basic auth requires both a username and an app-specific password"},
		},
		DefaultScheme:    SchemeBasic,
		Name:             "WeCom Calendar",
		Doctor:           constants.AppName + " doctor",
		MissingStep:      missingStep,
		InaccessibleStep: "Do not run `config init` or `auth login`, and do not issue a new CalDAV password, unless the same check also fails in the host environment.",
		MismatchHint:     "The service URL in effect is not the one the stored credential belongs to. Select the context for the intended service; this is not a password problem.",
		MismatchSteps:    []string{constants.AppName + " config get-contexts", constants.AppName + " config show --explain"},
	})
}

// missingStep is the last step when no stored password is visible. Resolve
// puts `auth reuse` ahead of it when another context could be reused.
const missingStep = "Only if the host retry also reports missing credentials, run `" + constants.AppName + " auth login` (or `config init`) in the user's terminal, or set WECOM_CALENDAR_* environment variables. Do not issue a new CalDAV password before then: it invalidates the previous one."

// Credential is a fully resolved credential ready to authenticate requests:
// the WeCom email as Username and the app-specific CalDAV password as Secret.
// Its Validate, Header, Decorator and Redacted methods follow the scheme
// registered above.
type Credential = kit.Credential

// AccountKey derives the keychain account identifier for a base URL and scheme.
func AccountKey(baseURL, scheme string) string { return kit.AccountKey(baseURL, scheme) }
