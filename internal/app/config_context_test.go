package app

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/angelmsger/wecom-calendar-cli/internal/config"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// A context name that does not exist is UNKNOWN_CONTEXT wherever it is named:
// the global flag, the context variable, use-context and delete-context. Each
// lists the defined names and points at the listing command, which must exist.
func TestUnknownContextNamesTheListingCommand(t *testing.T) {
	s, _, _ := loginStateForTest(t)
	file := config.File{CurrentContext: "personal", Contexts: []config.NamedContext{
		personalContext("personal", "https://caldav.wecom.work/"),
		{Name: "team", BaseURL: "https://caldav.wecom.work", Auth: config.AuthConfig{Scheme: "basic"}},
	}}
	if err := config.WriteFile(s.cfgDir, file); err != nil {
		t.Fatal(err)
	}
	want := []string{constants.AppName + " config get-contexts"}
	for _, step := range want {
		assertRunnableStep(t, step)
	}
	for _, args := range [][]string{
		{"--use-context", "ghost", "config", "show"},
		{"config", "use-context", "ghost"},
		{"config", "delete-context", "ghost"},
	} {
		_, _, err := captureCLI(t, s.cfgDir, args...)
		ce := cerrors.AsCLIError(err)
		if ce == nil || ce.Code != "UNKNOWN_CONTEXT" || ce.Hint != "Available contexts: personal, team." || !reflect.DeepEqual(ce.NextSteps, want) {
			t.Fatalf("%v: %+v", args, ce)
		}
	}
	t.Setenv("WECOM_CALENDAR_CONTEXT", "ghost")
	if _, err := config.Load(config.LoadOptions{ConfigDir: s.cfgDir, DotenvPath: filepath.Join(s.cfgDir, "absent.env")}); cerrors.AsCLIError(err) == nil ||
		cerrors.AsCLIError(err).Message != `context "ghost" (from WECOM_CALENDAR_CONTEXT) is not defined in the config file` ||
		!reflect.DeepEqual(cerrors.AsCLIError(err).NextSteps, want) {
		t.Fatalf("the context variable: %v", err)
	}
	after, _, _ := config.ReadFile(s.cfgDir)
	if !reflect.DeepEqual(after, file) {
		t.Fatalf("a failed lookup changed the file: %+v", after)
	}
}
