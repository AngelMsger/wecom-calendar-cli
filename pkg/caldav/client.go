package caldav

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
	origin string // scheme://host, no trailing slash
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
	req, err := http.NewRequest(http.MethodGet, c.absURL(href), nil)
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
	req, err := http.NewRequest(method, c.absURL(path), strings.NewReader(body))
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

// absURL resolves a server path (or absolute URL) to a full request URL.
func (c *apiClient) absURL(pathOrURL string) string {
	if strings.HasPrefix(pathOrURL, "http://") || strings.HasPrefix(pathOrURL, "https://") {
		return pathOrURL
	}
	if !strings.HasPrefix(pathOrURL, "/") {
		pathOrURL = "/" + pathOrURL
	}
	return c.origin + pathOrURL
}

// lastSegment returns the final non-empty path segment of an href.
func lastSegment(href string) string {
	trimmed := strings.TrimRight(href, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}
