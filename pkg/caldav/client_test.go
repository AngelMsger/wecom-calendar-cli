package caldav

import (
	"net/url"
	"testing"

	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

func testClient() *apiClient {
	return &apiClient{
		origin: "https://caldav.example.com",
		base:   &url.URL{Scheme: "https", Host: "caldav.example.com", Path: "/"},
	}
}

// TestResolveURLSameOrigin covers the normal case: a server-relative href
// resolves against the configured origin.
func TestResolveURLSameOrigin(t *testing.T) {
	c := testClient()
	cases := map[string]string{
		"/calendar/1/x.ics":                      "https://caldav.example.com/calendar/1/x.ics",
		"https://caldav.example.com/calendar/2/": "https://caldav.example.com/calendar/2/",
	}
	for in, want := range cases {
		got, err := c.resolveURL(in)
		if err != nil {
			t.Fatalf("resolveURL(%q): unexpected error %v", in, err)
		}
		if got != want {
			t.Fatalf("resolveURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestResolveURLRejectsCrossOrigin guards the credential-leak bug: a href on a
// different host (absolute or protocol-relative) must be refused, never
// followed, so the auth decorator can't send Basic credentials off-origin.
func TestResolveURLRejectsCrossOrigin(t *testing.T) {
	c := testClient()
	for _, href := range []string{
		"https://evil.example.net/calendar/1/x.ics",
		"//evil.example.net/x.ics",
		"http://caldav.example.com/x.ics", // downgraded scheme is also off-origin
	} {
		_, err := c.resolveURL(href)
		if err == nil {
			t.Fatalf("resolveURL(%q): want cross-origin rejection, got nil", href)
		}
		if code := cerrors.AsCLIError(err).Code; code != "CALDAV_CROSS_ORIGIN" {
			t.Fatalf("resolveURL(%q): want CALDAV_CROSS_ORIGIN, got %q", href, code)
		}
	}
}
