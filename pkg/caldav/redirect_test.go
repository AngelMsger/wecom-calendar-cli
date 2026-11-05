package caldav

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// davReads are the CalDAV reads that carry their query in a body: a
// PROPFIND (ListCalendars) and a REPORT (ListEvents).
var davReads = []struct {
	method string
	call   func(Client) error
}{
	{"PROPFIND", func(c Client) error { _, err := c.ListCalendars(context.Background()); return err }},
	{"REPORT", func(c Client) error {
		_, err := c.ListEvents(context.Background(), "/calendar/1/", time.Unix(1700000000, 0), time.Unix(1700086400, 0))
		return err
	}},
}

// A PROPFIND or REPORT carries its query in the body, and Go follows a 301,
// 302 or 303 with a GET that drops it. With agent-cli-kit v0.7.0 the client
// followed such a redirect on the same origin, so the page that GET got (a
// login page, say) answered the CalDAV read, and refused it on another origin
// (NETWORK). Since v0.8.0 the redirect comes back, on either origin, and is
// reported as itself: CALDAV_SERVER with the redirect's status, not
// retryable (the same request is redirected again), with a hint to check the
// server URL. It is sent once, even with retries configured, and no GET
// reaches the target.
func TestDAVRedirectIsReportedNotFollowedAsGet(t *testing.T) {
	var targetHits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		_, _ = w.Write([]byte("<html>sign in</html>"))
	}))
	defer other.Close()
	for _, read := range davReads {
		for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther} {
			for _, crossOrigin := range []bool{false, true} {
				var sends atomic.Int32
				targetHits.Store(0)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/login" {
						targetHits.Add(1)
						_, _ = w.Write([]byte("<html>sign in</html>"))
						return
					}
					sends.Add(1)
					target := "/login"
					if crossOrigin {
						target = other.URL + "/login"
					}
					http.Redirect(w, r, target, status)
				}))
				client, err := Build(BuildParams{BaseURL: srv.URL, Timeout: 5 * time.Second, MaxRetries: 2})
				if err != nil {
					t.Fatal(err)
				}
				ce := cerrors.AsCLIError(read.call(client))
				srv.Close()
				name := read.method
				if crossOrigin {
					name += " cross-origin"
				}
				if ce == nil || ce.Code != "CALDAV_SERVER" || ce.HTTPStatus != status || ce.Retryable ||
					!strings.Contains(ce.Hint, "redirected the request") ||
					len(ce.NextSteps) == 0 || ce.NextSteps[0] != "wecom-calendar-cli doctor" {
					t.Fatalf("%s %d: got %+v; want CALDAV_SERVER with the redirect's status, not retryable, pointing at the URL", name, status, ce)
				}
				if sends.Load() != 1 || targetHits.Load() != 0 {
					t.Fatalf("%s %d: sent %d times, %d requests reached the target; want one send and none", name, status, sends.Load(), targetHits.Load())
				}
			}
		}
	}
}

// A 307 or 308 on the same origin keeps the method and body, so the read is
// still followed.
func TestDAVPreservingRedirectIsFollowed(t *testing.T) {
	for _, read := range davReads {
		for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/moved") {
					http.Redirect(w, r, "/moved"+r.URL.Path, status)
					return
				}
				body, _ := io.ReadAll(r.Body)
				if r.Method != read.method || len(body) == 0 {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusMultiStatus)
				_, _ = w.Write([]byte(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"></d:multistatus>`))
			}))
			client, err := Build(BuildParams{BaseURL: srv.URL, Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			err = read.call(client)
			srv.Close()
			if err != nil {
				t.Fatalf("%s %d: %v; want the read followed with its method and body", read.method, status, err)
			}
		}
	}
}
