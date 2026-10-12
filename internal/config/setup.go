package config

import (
	kit "github.com/angelmsger/agent-cli-kit/config"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
)

// guideDocumentationURL is this project's own setup section. WeCom publishes no
// page about the CalDAV password that this project has verified, so the guide
// does not point at a vendor URL.
const guideDocumentationURL = "https://github.com/AngelMsger/wecom-calendar-cli#team-setup-and-personal-login"

// NormalizeServiceURL retains the deployment path and rejects credential-bearing
// URLs; a bare host is BAD_BASE_URL. It is used at setup and persistence
// boundaries, never to derive an API route.
func NormalizeServiceURL(raw string) (string, error) { return kit.NormalizeServiceURL(raw) }

// ValidateService checks only public setup fields and never probes a server.
// basic is the only scheme it accepts.
func ValidateService(cfg Config) error { return kit.ValidateService(kit.Encode(cfg)) }

// FieldChange describes a public configuration change; credentials are never included.
type FieldChange = kit.FieldChange

// ContextPlan is shared by preview and execution. File is the exact proposed file.
type ContextPlan = kit.ContextPlan[NamedContext, Defaults]

// PlanServiceContext builds a non-secret, target-specific merge without side
// effects: the service URL, auth scheme and credential page from resolved.
// When another context on the planned service already has a WeCom email, the
// next steps lead with `auth reuse --dry-run`.
func PlanServiceContext(file File, name string, resolved *Resolved, overwrite, activate bool) (ContextPlan, error) {
	plan, err := kit.PlanServiceContext(file, name, kit.Encode(resolved.Config), resolved.Sources, overwrite, activate)
	if err != nil {
		return ContextPlan{}, err
	}
	// A member who is already signed in on this service needs no password for
	// the preset; offer that before the steps that lead to acquiring one.
	if len(ReuseSources(plan.File, plan.Context)) > 0 {
		selected := constants.AppName + " --use-context " + QuoteContextArg(plan.Context)
		plan.NextSteps = append([]string{selected + reuseArgs}, plan.NextSteps...)
	}
	return plan, nil
}

// QuoteContextArg quotes a context name for a POSIX shell, so a recovery step
// that names it can be pasted as written.
func QuoteContextArg(name string) string { return kit.QuoteArg(name) }

// reuseArgs is the preview form every recovery step advertises: it verifies
// and reports, and writes nothing.
const reuseArgs = " auth reuse --dry-run"

// ReuseStep is the recovery step that precedes acquiring a credential when
// Config.MayReuse is set.
const ReuseStep = constants.AppName + reuseArgs

// AuthGuide is an offline acquisition guide, not a capability or authentication
// probe. WeCom has no deployment flavor, so its flavor field stays empty and is
// omitted.
type AuthGuide = kit.AuthGuide

// Guide describes where the CalDAV password comes from.
//
// WeCom issues it in the mobile app and has no web credential page, so unlike
// the siblings there is no built-in or fallback URL: CredentialURL stays empty
// and the navigation steps in Instructions are the acquisition location, with
// Source "builtin". A team may configure a display-only page of its own, which
// then fills CredentialURL and names its layer in Source. No request is ever
// sent to CredentialURL.
//
// Issuing a new password invalidates the previous one for every calendar
// client, so the instructions also say when not to issue one: an inaccessible
// credential store is recovered on the host, never by acquiring a credential.
// For the same reason a context that `auth reuse` could complete (cfg.MayReuse)
// is told so first, ahead of every step that leads to a password.
func Guide(cfg Config, sources map[string]string) (AuthGuide, error) {
	if err := ValidateService(cfg); err != nil {
		return AuthGuide{}, err
	}
	base, _ := NormalizeServiceURL(cfg.BaseURL)
	g := AuthGuide{Server: base, Scheme: cfg.Auth.Scheme, Source: "builtin",
		DocumentationURL: guideDocumentationURL,
		NextSteps:        []string{constants.AppName + " auth login"}}
	g.Instructions = []string{
		"Sign in with your full WeCom email as the username and an app-specific CalDAV password as the secret. Your normal WeCom login password is not accepted.",
		"The password is issued in the WeCom mobile app; there is no web page for it. Open Workbench, then Calendar, then the calendar settings, then \"Sync to other calendars\".",
		"Issuing a new CalDAV password invalidates the previous one, and every calendar client still using the old password stops syncing. Reuse the current password if you have it; issue a new one only when you have none or the server rejected the stored one (HTTP 401).",
		"If the stored password merely cannot be read here (CREDENTIAL_STORE_INACCESSIBLE, or any error whose recovery.scope is host), do not issue a new one. Retry the command with access to the user's home directory and OS keychain.",
	}
	if cfg.MayReuse {
		g.Instructions = append([]string{
			"Another context on this server already has a WeCom email, and contexts on one server share one stored CalDAV password. Before you enter or issue a password, run `auth reuse --dry-run` in this context and, when it reports `available`, `auth reuse`: it verifies that stored login and records its email here. No password is asked for and none is copied.",
		}, g.Instructions...)
		g.NextSteps = append([]string{ReuseStep}, g.NextSteps...)
	}
	if cfg.Auth.CredentialURL != "" {
		g.CredentialURL = cfg.Auth.CredentialURL
		g.Source = sources[fieldCredentialURL]
		if g.Source == "" {
			g.Source = "config"
		}
		g.Instructions = append(g.Instructions, "Your team keeps its own notes on the credential page. Open it in a browser to read; the CLI never requests it, and the password must never be sent to it.")
	}
	return g, nil
}

// WithCredentialGuide preserves host-store recovery and appends acquisition guidance
// only to absence errors, never to inaccessible-store errors. AuthGuide.Lines
// prints the credential page line only when a team configured one.
func WithCredentialGuide(err error, cfg Config) error {
	return kit.WithCredentialGuide(err, func() (AuthGuide, error) { return Guide(cfg, nil) })
}
