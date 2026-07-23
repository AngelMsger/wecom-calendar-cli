package caldav

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/angelmsger/wecom-calendar-cli/pkg/transport"
)

// homePath is the bare calendar-home collection. The Basic-auth identity
// determines whose calendars are served, so this path is the same for everyone.
const homePath = "/calendar/"

// utcStamp is the CalDAV time-range timestamp layout (UTC).
const utcStamp = "20060102T150405Z"

// Client is the CalDAV surface the command layer depends on. All operations are
// read-only; the WeCom server exposes no usable write path yet.
type Client interface {
	// ServerURL returns the configured base URL (origin).
	ServerURL() string
	// Ping verifies connectivity and credentials against the calendar-home.
	Ping(ctx context.Context) error
	// ListCalendars enumerates the calendars under the home set.
	ListCalendars(ctx context.Context) ([]Calendar, error)
	// ListEvents lists event resources in a calendar overlapping [since, until).
	ListEvents(ctx context.Context, calendarHref string, since, until time.Time) ([]EventRef, error)
	// GetEvent fetches one event resource's iCalendar body.
	GetEvent(ctx context.Context, href string) (RawEvent, error)
}

type apiClient struct {
	origin string   // scheme://host, no trailing slash
	base   *url.URL // origin as a URL (path "/"), for same-origin href resolution
	http   *transport.Client
}

func (c *apiClient) ServerURL() string { return c.origin + "/" }

func (c *apiClient) Ping(ctx context.Context) error {
	// A 207 multistatus (even one whose self-entry is 404) proves the endpoint
	// speaks CalDAV and the credentials were accepted.
	_, _, err := c.dav(ctx, "PROPFIND", homePath, propfindCalendars, "0")
	return err
}

func (c *apiClient) ListCalendars(ctx context.Context) ([]Calendar, error) {
	body, _, err := c.dav(ctx, "PROPFIND", homePath, propfindCalendars, "1")
	if err != nil {
		return nil, err
	}
	ms, err := parseMultistatus(body)
	if err != nil {
		return nil, cerrors.Wrap(err, cerrors.CategoryParse, "CALDAV_DECODE",
			"could not parse the calendar list response")
	}
	var out []Calendar
	for _, r := range ms.Responses {
		prop, ok := r.okProp()
		if !ok || prop.ResourceType.Calendar == nil {
			continue // the home self-entry and non-calendar collections
		}
		out = append(out, Calendar{
			ID:          lastSegment(r.Href),
			Href:        r.Href,
			DisplayName: strings.TrimSpace(prop.DisplayName),
			Ctag:        strings.TrimSpace(prop.GetCtag),
		})
	}
	return out, nil
}

func (c *apiClient) ListEvents(ctx context.Context, calendarHref string, since, until time.Time) ([]EventRef, error) {
	body := calendarQuery(since.UTC().Format(utcStamp), until.UTC().Format(utcStamp))
	resp, _, err := c.dav(ctx, "REPORT", calendarHref, body, "1")
	if err != nil {
		return nil, err
	}
	ms, err := parseMultistatus(resp)
	if err != nil {
		return nil, cerrors.Wrap(err, cerrors.CategoryParse, "CALDAV_DECODE",
			"could not parse the event list response")
	}
	var out []EventRef
	for _, r := range ms.Responses {
		prop, ok := r.okProp()
		if !ok || prop.GetEtag == "" {
			continue // the collection self-entry (404 propstat)
		}
		out = append(out, EventRef{Href: r.Href, Etag: strings.TrimSpace(prop.GetEtag)})
	}
	return out, nil
}

func (c *apiClient) GetEvent(ctx context.Context, href string) (RawEvent, error) {
	target, err := c.resolveURL(href)
	if err != nil {
		return RawEvent{}, err
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return RawEvent{}, cerrors.Wrap(err, cerrors.CategoryInternal, "REQUEST", "could not build request")
	}
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return RawEvent{}, cerrors.Wrap(err, cerrors.CategoryNetwork, "NETWORK", "could not reach the CalDAV server")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return RawEvent{}, c.statusError(resp.StatusCode, data)
	}
	return RawEvent{Href: href, Etag: strings.TrimSpace(resp.Header.Get("ETag")), ICS: string(data)}, nil
}

// dav issues a WebDAV request expecting a 207 multistatus and returns the body.
func (c *apiClient) dav(ctx context.Context, method, path, body, depth string) ([]byte, http.Header, error) {
	target, err := c.resolveURL(path)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		return nil, nil, cerrors.Wrap(err, cerrors.CategoryInternal, "REQUEST", "could not build request")
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("Depth", depth)
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, nil, cerrors.Wrap(err, cerrors.CategoryNetwork, "NETWORK", "could not reach the CalDAV server")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, nil, c.statusError(resp.StatusCode, data)
	}
	return data, resp.Header, nil
}

func (c *apiClient) statusError(status int, body []byte) error {
	cat := cerrors.FromHTTPStatus(status)
	code := "CALDAV_" + strings.ToUpper(string(cat))
	msg := fmt.Sprintf("CalDAV server returned HTTP %d", status)
	e := cerrors.New(cat, code, msg).WithHTTPStatus(status)
	if status == http.StatusForbidden {
		// A blanket 403 from this server usually means a wrong path/verb, not a
		// permission problem; keep the hint honest.
		e = e.WithHint("The WeCom CalDAV server returns 403 for unsupported paths/verbs as well as real permission failures.")
	}
	return e
}

// resolveURL turns a server-supplied href (a path, or an absolute URL echoed
// back in a multistatus) into a full request URL, and refuses to leave the
// configured origin. CalDAV responses can carry absolute hrefs; without this
// guard a server (or a MITM injecting a redirect-like href) could point a
// resource at another host and the transport's auth decorator would send the
// Basic app password there. Credentials must only ever reach the configured
// server, so a cross-origin href is a hard error rather than a followed link.
func (c *apiClient) resolveURL(href string) (string, error) {
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return "", cerrors.Newf(cerrors.CategoryParse, "CALDAV_BAD_HREF",
			"the server returned an unparseable resource href %q", href)
	}
	abs := c.base.ResolveReference(ref)
	if abs.Scheme != c.base.Scheme || !strings.EqualFold(abs.Host, c.base.Host) {
		return "", cerrors.Newf(cerrors.CategoryParse, "CALDAV_CROSS_ORIGIN",
			"refusing a CalDAV href on a different origin (%s://%s) than the configured server (%s://%s)",
			abs.Scheme, abs.Host, c.base.Scheme, c.base.Host).
			WithHint("Credentials are only ever sent to the configured server. If this host is legitimate, set it as the base URL.")
	}
	return abs.String(), nil
}

// lastSegment returns the final non-empty path segment of an href.
func lastSegment(href string) string {
	trimmed := strings.TrimRight(href, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}
