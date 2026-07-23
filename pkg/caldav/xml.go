package caldav

import (
	"encoding/xml"
	"strings"
)

// --- Request bodies (WebDAV / CalDAV) ---

const propfindCalendars = `<?xml version="1.0" encoding="utf-8"?>` +
	`<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:cs="http://calendarserver.org/ns/">` +
	`<d:prop><d:resourcetype/><d:displayname/><cs:getctag/></d:prop></d:propfind>`

// calendarQuery lists VEVENT resources overlapping [start,end); both are UTC
// timestamps formatted as 20060102T150405Z. Only getetag is requested — this
// server does not return usable inline calendar-data.
func calendarQuery(start, end string) string {
	return `<?xml version="1.0" encoding="utf-8"?>` +
		`<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">` +
		`<d:prop><d:getetag/></d:prop>` +
		`<c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT">` +
		`<c:time-range start="` + start + `" end="` + end + `"/>` +
		`</c:comp-filter></c:comp-filter></c:filter></c:calendar-query>`
}

// --- Response parsing (207 multistatus) ---

type msMultistatus struct {
	XMLName   xml.Name     `xml:"DAV: multistatus"`
	Responses []msResponse `xml:"DAV: response"`
	SyncToken string       `xml:"DAV: sync-token"`
}

type msResponse struct {
	Href string `xml:"DAV: href"`
	// Status is set on whole-response failures (e.g. the home self-entry that
	// this server reports as "404 object not found").
	Status    string       `xml:"DAV: status"`
	Propstats []msPropstat `xml:"DAV: propstat"`
}

type msPropstat struct {
	Status string `xml:"DAV: status"`
	Prop   msProp `xml:"DAV: prop"`
}

type msProp struct {
	DisplayName  string         `xml:"DAV: displayname"`
	GetEtag      string         `xml:"DAV: getetag"`
	GetCtag      string         `xml:"http://calendarserver.org/ns/ getctag"`
	ResourceType msResourceType `xml:"DAV: resourcetype"`
}

type msResourceType struct {
	Calendar   *struct{} `xml:"urn:ietf:params:xml:ns:caldav calendar"`
	Collection *struct{} `xml:"DAV: collection"`
}

func parseMultistatus(body []byte) (*msMultistatus, error) {
	var ms msMultistatus
	if err := xml.Unmarshal(body, &ms); err != nil {
		return nil, err
	}
	return &ms, nil
}

// okProp returns the prop from the first propstat whose status is 2xx, and
// whether one was found. Props under non-2xx propstats (this server buries
// "not found" props in a 404 propstat) are ignored.
func (r msResponse) okProp() (msProp, bool) {
	for _, ps := range r.Propstats {
		if statusIsOK(ps.Status) {
			return ps.Prop, true
		}
	}
	return msProp{}, false
}

// statusIsOK reports whether a WebDAV status line ("HTTP/1.1 200 OK") is 2xx.
func statusIsOK(status string) bool {
	fields := strings.Fields(status)
	for _, f := range fields {
		if len(f) == 3 && f[0] == '2' {
			return true
		}
	}
	return false
}
