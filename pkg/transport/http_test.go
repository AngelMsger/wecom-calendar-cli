package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
)

// The client is tested in agent-cli-kit; these check wecom-calendar-cli's
// defaults. The redirect guard is covered by redirect_test.go.
func TestNewSendsWecomCalendarUserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := New(Options{}).Do(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got != constants.UserAgent() {
		t.Fatalf("User-Agent = %q, want %q", got, constants.UserAgent())
	}
}

// attempts sends one request with method through a client whose first answer
// is a 503, and reports how many times the request was sent.
func attempts(t *testing.T, opt Options, method string) int {
	t.Helper()
	doer := &recordingDoer{statuses: []int{http.StatusServiceUnavailable, http.StatusOK}}
	opt.Doer, opt.MaxRetries, opt.RetryBaseDelay = doer, 2, time.Millisecond
	req, err := http.NewRequest(method, "https://caldav.example.com/calendar/", strings.NewReader("<x/>"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := New(opt).Do(context.Background(), req)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	resp.Body.Close()
	return doer.callCount
}

// GET, HEAD and the read-only CalDAV verbs are retried; every other method,
// including the CalDAV writes, is sent exactly once.
func TestNewRetriesTheCalDAVReadMethods(t *testing.T) {
	for method, want := range map[string]int{
		http.MethodGet: 2, http.MethodHead: 2, "PROPFIND": 2, "REPORT": 2,
		http.MethodPost: 1, http.MethodPut: 1, http.MethodDelete: 1, "PROPPATCH": 1, "MKCALENDAR": 1,
	} {
		if got := attempts(t, Options{}, method); got != want {
			t.Errorf("%s: %d attempts, want %d", method, got, want)
		}
	}
}

// A caller's RetryMethods add to the CalDAV read methods rather than replace them.
func TestNewKeepsTheCalDAVReadMethodsWithCallerRetryMethods(t *testing.T) {
	opt := Options{RetryMethods: []string{http.MethodOptions}}
	for _, method := range []string{http.MethodOptions, "PROPFIND", "REPORT"} {
		if got := attempts(t, opt, method); got != 2 {
			t.Errorf("%s: %d attempts, want 2", method, got)
		}
	}
}
