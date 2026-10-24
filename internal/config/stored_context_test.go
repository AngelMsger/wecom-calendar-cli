package config

import (
	"testing"

	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
)

// Reuse compares what the file stores. A stored context resolves through the
// built-in defaults and the shared defaults only, never through the
// environment of the invocation that happens to be running.
func TestStoredContextIgnoresTheEnvironment(t *testing.T) {
	cleanSetupEnv(t)
	t.Setenv("WECOM_CALENDAR_SERVER", "https://env.example.test")
	t.Setenv("WECOM_CALENDAR_USERNAME", "injected@example.test")
	t.Setenv("WECOM_CALENDAR_PASSWORD", "must-not-be-used")
	cfg := StoredContext(NamedContext{Name: "team"}, Defaults{Format: "table"})
	if cfg.BaseURL != constants.DefaultServerURL || cfg.Auth.Scheme != SchemeBasic || cfg.Auth.Username != "" || cfg.Defaults.Format != "table" || cfg.Defaults.MaxRetries != constants.DefaultMaxRetries {
		t.Fatalf("a stored context resolved from something other than the file and the defaults: %+v", cfg)
	}
	cfg = StoredContext(reuseFile().Contexts[2], Defaults{})
	if cfg.BaseURL != "https://elsewhere.example.test/dav" || cfg.Auth.Username != "member@example.test" || cfg.ReuseStep != "" || cfg.CredentialBaseURL != "" {
		t.Fatalf("stored fields were not kept as written: %+v", cfg)
	}
}
