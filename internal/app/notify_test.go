package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The release check keeps the URLs and the override variable it had before it
// moved to agent-cli-kit, including the lower-case repository owner.
func TestReleaseCheckerKeepsItsURLsAndOverride(t *testing.T) {
	const endpoint = "https://api.github.com/repos/angelmsger/wecom-calendar-cli/releases/latest"
	if got := releaseChecker.DefaultEndpoint(); got != endpoint {
		t.Fatalf("DefaultEndpoint() = %q, want %q", got, endpoint)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
	}))
	defer srv.Close()
	t.Setenv("WECOM_CALENDAR_RELEASE_API", srv.URL)

	st := releaseChecker.Check(context.Background(), srv.Client(), "0.0.1")
	const detail = "a newer release is available: 0.0.1 -> 9.9.9; see https://github.com/angelmsger/wecom-calendar-cli/releases/latest"
	if !st.Available || st.Detail != detail {
		t.Fatalf("Check() = %+v, want an available update with detail %q", st, detail)
	}
}
