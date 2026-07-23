// Package caldav is a minimal, purpose-built CalDAV client for the WeCom
// (Tencent Exmail) calendar server. That server is markedly non-standard:
// generic CalDAV libraries fail against it. Empirically the only working shape
// is:
//
//   - calendar-home is the BARE collection "/calendar/" (the Basic-auth user
//     determines whose calendars are served); the root "/", ".well-known" and
//     "/principals/" all return 403/404.
//   - PROPFIND /calendar/ Depth:1 lists calendars at "/calendar/<id>/" with a
//     displayname and a calendarserver getctag.
//   - REPORT calendar-query with a time-range on a calendar lists event ".ics"
//     hrefs + etags (inline calendar-data comes back empty; calendar-multiget
//     returns 403).
//   - each event body is fetched with a plain GET on its ".ics" href.
//   - TZIDs are Tencent-custom (e.g. "TZ08"), but every event embeds a
//     VTIMEZONE defining them, so the parser must read the embedded timezone.
//
// This package backs the wecom-calendar-cli command layer and is importable as
// a library. Its surface mirrors the sibling projects' pkg/apiclient.
package caldav

// Calendar is a CalDAV calendar collection under the home set.
type Calendar struct {
	// ID is the stable local identifier: the last path segment of Href.
	ID string `json:"id"`
	// Href is the server path of the collection, e.g. "/calendar/1688853806313356/".
	Href string `json:"href"`
	// DisplayName is the human-facing calendar name.
	DisplayName string `json:"display_name"`
	// Ctag changes whenever any resource in the collection changes; it is the
	// cheap incremental-sync signal.
	Ctag string `json:"ctag"`
}

// EventRef identifies one event resource within a calendar without its body.
type EventRef struct {
	// Href is the server path of the ".ics" resource.
	Href string `json:"href"`
	// Etag changes when the resource body changes.
	Etag string `json:"etag"`
}

// RawEvent is a fetched event resource: its iCalendar body plus identity.
type RawEvent struct {
	Href string `json:"href"`
	Etag string `json:"etag"`
	// ICS is the raw iCalendar (RFC 5545) document, stored verbatim.
	ICS string `json:"ics"`
}
