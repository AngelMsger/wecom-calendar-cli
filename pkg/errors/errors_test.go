package errors

import "testing"

// The model is tested in agent-cli-kit; this checks that wecom-calendar-cli's
// own guidance is what an error built through this package carries.
func TestErrorsCarryWecomCalendarGuidance(t *testing.T) {
	e := New(CategoryConfig, "NO_BASE_URL", "no CalDAV server URL is configured")
	want := []string{"wecom-calendar-cli config init", "wecom-calendar-cli config show --explain"}
	if len(e.NextSteps) != len(want) || e.NextSteps[0] != want[0] || e.NextSteps[1] != want[1] {
		t.Fatalf("NextSteps = %v, want wecom-calendar-cli's config guidance %v", e.NextSteps, want)
	}
	if ExitCode(e) != ExitConfig {
		t.Fatalf("ExitCode = %d, want %d", ExitCode(e), ExitConfig)
	}
}
