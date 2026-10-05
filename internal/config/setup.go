package config

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// guideDocumentationURL is this project's own setup section. WeCom publishes no
// page about the CalDAV password that this project has verified, so the guide
// does not point at a vendor URL.
const guideDocumentationURL = "https://github.com/AngelMsger/wecom-calendar-cli#team-setup-and-personal-login"

// serviceField excludes personal identity and secrets before environment
// inference. The WeCom email and the CalDAV password belong to one person; the
// server URL, the scheme and the optional credential page are shared.
func serviceField(field string) bool {
	switch field {
	case fieldFormat, fieldReadOnly, fieldServer, fieldAuthScheme, fieldCredentialURL:
		return true
	default:
		return false
	}
}

// resolveAuthDefaults is the hook the layered loader calls once the layers are
// merged. WeCom CalDAV has a single scheme and the default layer already
// supplies it, so there is nothing to infer after the merge.
func resolveAuthDefaults(values, sources map[string]string) {}

// NormalizeServiceURL retains the deployment path and rejects credential-bearing URLs.
// It is used at setup and persistence boundaries, never to derive an API route.
func NormalizeServiceURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", cerrors.New(cerrors.CategoryConfig, "NO_BASE_URL", "no service URL configured").WithNextSteps(constants.AppName + " config set-context <name> --base-url <url>")
	}

	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(s, "\r\n\t") {
		return "", cerrors.New(cerrors.CategoryConfig, "BAD_BASE_URL", "service URL must be an HTTP(S) URL without credentials, query or fragment")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		host := u.Hostname()
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		u.Host = host
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func validateCredentialURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || strings.ContainsAny(raw, "\r\n\t") {
		return cerrors.New(cerrors.CategoryConfig, "BAD_CREDENTIAL_URL", "credential URL must be an absolute HTTP(S) page URL without embedded credentials")
	}
	return nil
}

// ValidateService checks only public setup fields and never probes a server.
func ValidateService(cfg Config) error {
	if _, err := NormalizeServiceURL(cfg.BaseURL); err != nil {
		return err
	}
	if err := validateCredentialURL(cfg.Auth.CredentialURL); err != nil {
		return err
	}
	if cfg.Auth.Scheme != SchemeBasic {
		return cerrors.New(cerrors.CategoryConfig, "AUTH_BAD_SCHEME", "unsupported authentication scheme").WithHint("Use: basic. WeCom CalDAV accepts only HTTP Basic with the WeCom email and an app-specific CalDAV password.")
	}
	return nil
}

// FieldChange describes a public configuration change; credentials are never included.
type FieldChange struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

// ContextPlan is shared by preview and execution. File is the exact proposed file.
type ContextPlan struct {
	Context        string                 `json:"context"`
	Changed        bool                   `json:"changed"`
	CurrentContext string                 `json:"current_context"`
	Changes        map[string]FieldChange `json:"changes"`
	NextSteps      []string               `json:"next_steps"`
	File           File                   `json:"-"`
}

// PlanServiceContext builds a non-secret, target-specific merge without side effects.
func PlanServiceContext(file File, name string, resolved *Resolved, overwrite, activate bool) (ContextPlan, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || strings.ContainsAny(name, "\r\n\t") {
		return ContextPlan{}, cerrors.New(cerrors.CategoryUsage, "CTX_NAME_EMPTY", "provide a non-empty context name")
	}
	old, exists := file.Context(name)
	if exists {
		name = old.Name
	}
	cfg := resolved.Config
	normalized, err := NormalizeServiceURL(cfg.BaseURL)
	if err != nil {
		return ContextPlan{}, err
	}
	cfg.BaseURL = normalized
	if err := ValidateService(cfg); err != nil {
		return ContextPlan{}, err
	}
	next := old
	next.Name = name
	changes := map[string]FieldChange{}
	conflicts := map[string]FieldChange{}
	merge := func(field string, dst *string, value string) {
		source := resolved.Sources[field]
		if exists && *dst != "" && source != "flag" && source != "env" && source != "dotenv" {
			return
		}
		before := *dst
		if field == fieldServer && before != "" {
			if normal, e := NormalizeServiceURL(before); e == nil && normal == value {
				return
			}
		}
		if before == value {
			return
		}
		change := FieldChange{Before: before, After: value}
		changes[field] = change
		if exists && before != "" {
			conflicts[field] = change
		}
		*dst = value
	}
	merge(fieldServer, &next.BaseURL, cfg.BaseURL)
	merge(fieldAuthScheme, &next.Auth.Scheme, cfg.Auth.Scheme)
	merge(fieldCredentialURL, &next.Auth.CredentialURL, cfg.Auth.CredentialURL)
	if len(conflicts) > 0 && !overwrite {
		return ContextPlan{}, cerrors.New(cerrors.CategoryConflict, "CONFIG_CONTEXT_CONFLICT", "team presets conflict with the existing context").WithDetails(conflicts).WithHint("Inspect the field differences; use --overwrite to update the supplied service fields, or choose another context name.").WithNextSteps(constants.AppName + " config set-context --help")
	}
	result := file
	result.Contexts = append([]NamedContext(nil), file.Contexts...)
	replaced := false
	for i, c := range result.Contexts {
		if c.Name == name {
			result.Contexts[i] = next
			replaced = true
			break
		}
	}
	if !replaced {
		result.Contexts = append(result.Contexts, next)
	}
	if len(file.Contexts) == 0 || activate {
		result.CurrentContext = name
	}
	if result.CurrentContext != file.CurrentContext {
		changes["current_context"] = FieldChange{Before: file.CurrentContext, After: result.CurrentContext}
	}
	quotedName := "'" + strings.ReplaceAll(name, "'", "'\"'\"'") + "'"
	return ContextPlan{Context: name, Changed: !reflect.DeepEqual(file, result), CurrentContext: result.CurrentContext, Changes: changes,
		NextSteps: []string{constants.AppName + " --use-context " + quotedName + " auth guide", constants.AppName + " --use-context " + quotedName + " auth login"}, File: result}, nil
}

// AuthGuide is an offline acquisition guide, not a capability or authentication probe.
type AuthGuide struct {
	Server           string   `json:"server"`
	Scheme           string   `json:"scheme"`
	CredentialURL    string   `json:"credential_url"`
	Source           string   `json:"source"`
	Instructions     []string `json:"instructions"`
	DocumentationURL string   `json:"documentation_url"`
	NextSteps        []string `json:"next_steps"`
}

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

// Lines returns the same guidance for plain prompts and terminal forms. The
// credential page line appears only when a team configured one.
func (g AuthGuide) Lines() []string {
	lines := []string{fmt.Sprintf("Service: %s (%s)", g.Server, g.Scheme)}
	if g.CredentialURL != "" {
		lines = append(lines, "Credential page: "+g.CredentialURL)
	}
	return append(lines, g.Instructions...)
}

// WithCredentialGuide preserves host-store recovery and appends acquisition guidance
// only to absence errors, never to inaccessible-store errors.
func WithCredentialGuide(err error, cfg Config) error {
	ce := cerrors.AsCLIError(err)
	switch ce.Code {
	case "AUTH_NO_BASIC", "CREDENTIAL_NOT_VISIBLE_OR_MISSING":
		if g, e := Guide(cfg, nil); e == nil {
			ce.NextSteps = append(ce.NextSteps, constants.AppName+" auth guide")
			if g.CredentialURL != "" {
				ce.NextSteps = append(ce.NextSteps, "Credential page: "+g.CredentialURL)
			}
		}
	}
	return ce
}
