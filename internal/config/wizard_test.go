package config

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
)

// runPlainWizard drives the plain wizard on scripted input and fails instead
// of hanging when a prompt never returns.
func runPlainWizard(t *testing.T, input string, inputs WizardInputs) (*WizardResult, string, error) {
	t.Helper()
	var out bytes.Buffer
	type outcome struct {
		result *WizardResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := RunWizard(NewPlainDriver(strings.NewReader(input), &out), WizardHooks{}, inputs)
		done <- outcome{result, err}
	}()
	select {
	case got := <-done:
		return got.result, out.String(), got.err
	case <-time.After(10 * time.Second):
		t.Fatal("the wizard kept prompting after its input ended")
		return nil, "", nil
	}
}

// The scripted setup that existed before team presets still works unchanged:
// three answers, then end of input, and the trailing "Add another context?"
// takes its default.
func TestPlainWizardKeepsTheScriptedSetup(t *testing.T) {
	result, prompts, err := runPlainWizard(t, "\nMember@Example.Test\napp-password\n", WizardInputs{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Creds) != 1 {
		t.Fatalf("contexts: %+v", result.Creds)
	}
	got := result.Creds[0]
	want := NamedContext{Name: DefaultContextName, BaseURL: constants.DefaultServerURL, Auth: AuthConfig{Scheme: SchemeBasic, Username: "member@example.test"}}
	if got.Context != want || got.Secrets.Password != "app-password" || result.File.CurrentContext != DefaultContextName {
		t.Fatalf("unexpected result: %+v", got)
	}
	// The wizard prints the same acquisition guide as auth login.
	for _, wanted := range []string{"CalDAV server URL [" + constants.DefaultServerURL + "]", "WeCom email", "App-specific password", "Sync to other calendars", "invalidates the previous one"} {
		if !strings.Contains(prompts, wanted) {
			t.Fatalf("prompt stream is missing %q:\n%s", wanted, prompts)
		}
	}
	if strings.Contains(prompts, "Credential page:") {
		t.Fatalf("the wizard showed a credential page nobody configured:\n%s", prompts)
	}
}

// Input that ends before a required answer is an error, not an endless prompt.
func TestPlainWizardStopsWhenInputEndsBeforeARequiredAnswer(t *testing.T) {
	if _, _, err := runPlainWizard(t, "\n", WizardInputs{}); err != io.EOF {
		t.Fatalf("want io.EOF, got %v", err)
	}
}

// Team presets reach the wizard through Prefill, and the credential page they
// carry survives an edit of the context.
func TestPlainWizardAppliesPresetsAndKeepsTheCredentialPage(t *testing.T) {
	stored := setupFixture()
	existing := File{CurrentContext: stored.Name, Contexts: []NamedContext{stored}}
	asked := ""
	inputs := WizardInputs{
		Existing: &existing,
		Prefill: func(name string, prefill *NamedContext) (*NamedContext, error) {
			asked = name
			preset := *prefill
			return &preset, nil
		},
		// A stored secret is offered only when an existing context is edited.
		LoadSecret: func(NamedContext) (Secrets, bool) { return Secrets{Password: "stored-password"}, true },
	}
	result, prompts, err := runPlainWizard(t, "edit\n\n\n\n", inputs)
	if err != nil {
		t.Fatal(err)
	}
	if asked != stored.Name || len(result.Creds) != 1 {
		t.Fatalf("prefill asked for %q, contexts %+v", asked, result.Creds)
	}
	if got := result.Creds[0]; got.Context != stored || got.Secrets.Password != "stored-password" {
		t.Fatalf("edit lost a preset field or the kept secret: %+v", got)
	}
	if !strings.Contains(prompts, "Credential page: "+stored.Auth.CredentialURL) || !strings.Contains(prompts, "press Enter to keep current") {
		t.Fatalf("prompt stream:\n%s", prompts)
	}

	// Adding a context never offers another context's stored secret.
	result, prompts, err = runPlainWizard(t, "add\nsecond\n\nother@example.test\nnew-password\n", WizardInputs{
		Existing: &existing,
		Prefill: func(name string, _ *NamedContext) (*NamedContext, error) {
			return &NamedContext{Name: name, BaseURL: "https://preset.example.test/dav", Auth: AuthConfig{Scheme: SchemeBasic}}, nil
		},
		LoadSecret: func(NamedContext) (Secrets, bool) { return Secrets{Password: "stored-password"}, true },
	})
	if err != nil {
		t.Fatal(err)
	}
	added := result.Creds[0]
	if added.Context.Name != "second" || added.Context.BaseURL != "https://preset.example.test/dav" || added.Secrets.Password != "new-password" {
		t.Fatalf("add did not start from the preset: %+v", added)
	}
	if strings.Contains(prompts, "press Enter to keep current") {
		t.Fatalf("a new context was offered a stored secret:\n%s", prompts)
	}
}
