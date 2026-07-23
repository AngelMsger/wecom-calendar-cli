package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	"github.com/charmbracelet/huh"
)

// RunWizardHuh is the `--pretty` entry point. It runs the wizard in two
// `huh.Form` phases so the field bindings can be re-seeded between them
// based on the user's intent:
//
//  1. Phase 1 (only when an existing config is present): pick action
//     (edit / add / replace), and — for edit on a multi-context file —
//     which context to edit, or — for add — the new context name.
//  2. Between phases: seed BaseURL / Username from the chosen edit target,
//     or leave them empty for add / replace.
//  3. Phase 2: collect server URL, WeCom email, and the app-specific password.
//
// Validate runs after phase 2 because huh.Field.Validate is synchronous and
// would freeze the TUI on a slow network call.
//
// Multi-context-per-run is intentionally unsupported in this mode (the
// plain path keeps it); a user who wants a second context re-runs the
// command and lands in the add path.
func RunWizardHuh(hooks WizardHooks, inputs WizardInputs) (*WizardResult, error) {
	hasExisting := inputs.Existing != nil && len(inputs.Existing.Contexts) > 0

	result := &WizardResult{
		File: File{
			CurrentContext: DefaultContextName,
			Defaults:       configFromMap(defaultLayer()).Defaults,
		},
	}
	if hasExisting {
		result.File.CurrentContext = inputs.Existing.CurrentContext
		result.File.Defaults = inputs.Existing.Defaults
	}

	prefillBy := map[string]NamedContext{}
	if hasExisting {
		for _, c := range inputs.Existing.Contexts {
			prefillBy[c.Name] = c
		}
	}

	// Phase 1 — action / edit-target / new-name. Always emit a default that
	// matches what we'd auto-pick in plain mode.
	action := "replace"
	editTarget := ""
	newName := ""
	if hasExisting {
		action = "edit"
		editTarget = inputs.Existing.CurrentContext
		if editTarget == "" {
			// A hand-edited or legacy-format config can omit current_context.
			// selectContext (loader.go) treats this as valid and falls back to
			// the sole context; mirror that here so the wizard never tries to
			// edit a nameless target. Falling back to Contexts[0] is also a
			// safe pre-selection in the multi-context case — the "Edit which
			// context?" group still lets the user change it before submit.
			editTarget = inputs.Existing.Contexts[0].Name
		}
	}

	if hasExisting {
		if err := runPhase1(&action, &editTarget, &newName, inputs.Existing.Contexts, prefillBy); err != nil {
			return nil, err
		}
	}

	// Determine the final identity and the prefill we should seed phase 2 from.
	name := DefaultContextName
	var prefill *NamedContext
	switch action {
	case "edit":
		name = editTarget
		if c, ok := prefillBy[name]; ok {
			prefill = &c
		}
	case "add":
		name = newName
	case "replace":
		name = DefaultContextName
	}

	// Phase 2 — main fields, seeded based on phase 1 outcome.
	var (
		baseURL  = constants.DefaultServerURL
		username string
		secret   string
	)
	if prefill != nil {
		if prefill.BaseURL != "" {
			baseURL = prefill.BaseURL
		}
		username = prefill.Auth.Username
	}

	// Load the kept secret once. We use it both to allow empty input on the
	// secret prompt and to route the secret post-form. Loading here (instead
	// of inside huh.Validate) keeps keychain interaction to one call.
	var kept Secrets
	hasKeptSecret := false
	if prefill != nil && inputs.LoadSecret != nil {
		if loaded, ok := inputs.LoadSecret(*prefill); ok {
			kept = loaded
			hasKeptSecret = true
		}
	}

	// secretValidator allows empty input only when we are in the
	// edit-this-context-with-stored-credentials path. Otherwise the field is
	// required — this is what stops --pretty from writing an empty secret on
	// add / replace paths.
	secretValidator := func(s string) error {
		if s != "" {
			return nil
		}
		if action != "edit" || prefill == nil || !hasKeptSecret {
			return errors.New("required")
		}
		return nil
	}

	keepHint := passwordNote
	if hasKeptSecret {
		keepHint = "Leave empty and press Enter to keep the current value. " + passwordNote
	}

	if err := runPhase2(&baseURL, &username, &secret, secretValidator, keepHint); err != nil {
		return nil, err
	}

	// Empty secret means "keep" only when the validator allowed it; that
	// invariant is enforced above, so this check is just a re-statement.
	keepSecret := secret == "" && hasKeptSecret && prefill != nil

	picks := contextPicks{
		Name: name, BaseURL: baseURL, Username: username,
		Secret: secret, KeepSecret: keepSecret,
	}

	cr := assembleContextResult(picks, kept)

	if hooks.Validate != nil {
		fmt.Fprintln(os.Stderr, "Validating credentials…")
		if err := hooks.Validate(contextConfig(cr.Context), cr.Secrets); err != nil {
			fmt.Fprintf(os.Stderr, "  validation failed: %v\n", err)
			var save bool
			confirm := huh.NewForm(huh.NewGroup(
				huh.NewConfirm().
					Title("Save configuration anyway?").
					Affirmative("Save").
					Negative("Cancel").
					Value(&save),
			)).WithInput(os.Stdin).WithOutput(os.Stderr)
			if cerr := confirm.Run(); cerr != nil {
				return nil, cerr
			}
			if !save {
				return nil, fmt.Errorf("aborted: credential validation failed")
			}
		} else {
			fmt.Fprintln(os.Stderr, "  credentials OK")
		}
	}

	if hasExisting && action != "replace" {
		for _, c := range inputs.Existing.Contexts {
			if c.Name == name {
				continue
			}
			result.File.Contexts = append(result.File.Contexts, c)
		}
	}
	result.File.Contexts = append(result.File.Contexts, cr.Context)
	result.Creds = append(result.Creds, cr)

	return result, nil
}

func runPhase1(action, editTarget, newName *string, contexts []NamedContext, prefillBy map[string]NamedContext) error {
	actionOpts := []huh.Option[string]{
		huh.NewOption("Edit an existing context", "edit"),
		huh.NewOption("Add a new context", "add"),
		huh.NewOption("Replace configuration", "replace"),
	}
	var contextOpts []huh.Option[string]
	for _, c := range contexts {
		contextOpts = append(contextOpts, huh.NewOption(
			fmt.Sprintf("%s — %s", c.Name, c.BaseURL), c.Name))
	}

	groups := []*huh.Group{
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What would you like to do?").
				Description("↑/↓ to navigate; Shift-Tab to go back to a previous step.").
				Options(actionOpts...).
				Value(action),
		),
	}

	if len(contexts) > 1 {
		groups = append(groups, huh.NewGroup(
			huh.NewSelect[string]().
				Title("Edit which context?").
				Options(contextOpts...).
				Value(editTarget),
		).WithHideFunc(func() bool { return *action != "edit" }))
	}

	groups = append(groups, huh.NewGroup(
		huh.NewInput().
			Title("New context name").
			Placeholder("work").
			Value(newName).
			Validate(func(s string) error {
				if s == "" {
					return errors.New("required")
				}
				if _, taken := prefillBy[s]; taken {
					return fmt.Errorf("context %q already exists", s)
				}
				return nil
			}),
	).WithHideFunc(func() bool { return *action != "add" }))

	form := huh.NewForm(groups...).
		WithInput(os.Stdin).
		WithOutput(os.Stderr)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return fmt.Errorf("wizard cancelled")
		}
		return err
	}
	return nil
}

// runPhase2 collects the CalDAV server URL, WeCom email, and the app-specific
// password. There is a single auth scheme (basic), so no scheme prompt is
// shown.
func runPhase2(baseURL, username, secret *string, secretValidator func(string) error, keepHint string) error {
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("CalDAV server URL").
			Placeholder(exampleBaseURL).
			Value(baseURL).
			Validate(func(s string) error {
				if s == "" {
					return errors.New("required")
				}
				return nil
			}),
		huh.NewInput().
			Title("WeCom email").
			Placeholder(exampleUsername).
			Value(username).
			Validate(func(s string) error {
				if s == "" {
					return errors.New("required")
				}
				return nil
			}),
		huh.NewInput().
			Title("App-specific password").
			Description(keepHint).
			EchoMode(huh.EchoModePassword).
			Value(secret).
			Validate(secretValidator),
	)).WithInput(os.Stdin).WithOutput(os.Stderr)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return fmt.Errorf("wizard cancelled")
		}
		return err
	}
	return nil
}
