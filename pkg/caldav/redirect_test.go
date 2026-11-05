package caldav

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// A PROPFIND or REPORT carries its query in the body, and Go follows a 301,
// 302 or 303 with a GET that drops it. With agent-cli-kit v0.7.0 the client
// followed such a redirect, so the page that GET got (a login page, say)
// answered the CalDAV read, and the error carried that page's status. Since
// v0.8.0 the redirect comes back and is reported as itself: CALDAV_SERVER
// with the redirect's status, and no GET reaches the target.
func TestDAVRedirectIsReportedNotFollowedAsGet(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther} {
		var gets int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/login" {
				gets++
				_, _ = w.Write([]byte("<html>sign in</html>"))
				return
			}
			http.Redirect(w, r, "/login", status)
		}))
		client, err := Build(BuildParams{BaseURL: srv.URL, Timeout: 5 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.ListCalendars(context.Background())
		srv.Close()
		ce := cerrors.AsCLIError(err)
		if ce == nil || ce.Code != "CALDAV_SERVER" || ce.HTTPStatus != status || gets != 0 {
			t.Fatalf("%d: got %+v after %d GETs; want CALDAV_SERVER with the redirect's status and no GET", status, ce, gets)
		}
	}
}

// A 307 or 308 keeps the method and body, so the read is still followed.
func TestDAVPreservingRedirectIsFollowed(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/moved") {
				http.Redirect(w, r, "/moved"+r.URL.Path, status)
				return
			}
			body, _ := io.ReadAll(r.Body)
			if r.Method != "PROPFIND" || !strings.Contains(string(body), "propfind") {
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
		_, err = client.ListCalendars(context.Background())
		srv.Close()
		if err != nil {
			t.Fatalf("%d: %v; want the PROPFIND followed with its body", status, err)
		}
	}
}
