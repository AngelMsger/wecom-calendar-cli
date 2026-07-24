// Package ical parses the iCalendar (RFC 5545) bodies returned by the WeCom
// CalDAV server into structured events.
//
// The WeCom / Tencent backend uses non-IANA TZIDs (e.g. "TZ08") but embeds a
// VTIMEZONE defining each one. go-ical's own DateTime helper calls
// time.LoadLocation(tzid) and hard-fails on such TZIDs, so this package
// resolves the embedded VTIMEZONE offsets itself and parses date-times
// manually. go-ical is used only for tokenizing the document structure.
package ical

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	goical "github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

const (
	dateFormat     = "20060102"
	dateTimeFormat = "20060102T150405"
	dateTimeUTC    = "20060102T150405Z"
)

// Attendee is one participant of an event.
type Attendee struct {
	Name           string `json:"name,omitempty"`
	Email          string `json:"email,omitempty"`
	ResponseStatus string `json:"response_status,omitempty"`
}

// Event is a parsed VEVENT component (a master or a RECURRENCE-ID override).
type Event struct {
	UID          string    `json:"uid"`
	Summary      string    `json:"summary"`
	Description  string    `json:"description,omitempty"`
	Location     string    `json:"location,omitempty"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	AllDay       bool      `json:"all_day"`
	Status       string    `json:"status,omitempty"`
	Sequence     int       `json:"sequence,omitempty"`
	LastModified time.Time `json:"last_modified,omitempty"`
	// RecurrenceIDKey is "" for a master event, else the normalized nominal
	// instant of the overridden occurrence (UTC ms for date-times, YYYY-MM-DD
	// for all-day). RecurrenceIDRaw preserves the original value.
	RecurrenceIDKey string     `json:"recurrence_id_key,omitempty"`
	RecurrenceIDRaw string     `json:"recurrence_id_raw,omitempty"`
	RRule           string     `json:"rrule,omitempty"`
	ExDates         []string   `json:"exdates,omitempty"` // normalized keys
	Attendees       []Attendee `json:"attendees,omitempty"`
	Organizer       string     `json:"organizer,omitempty"`
	// RRuleOption is the parsed recurrence rule with Dtstart set to Start, ready
	// for expansion. Nil for non-recurring events. Not serialized.
	RRuleOption *rrule.ROption `json:"-"`
}

// Parse decodes an iCalendar document into its events. defaultLoc is used for
// floating times and as a last resort; pass the configured display timezone.
func Parse(ics string, defaultLoc *time.Location) ([]Event, error) {
	if defaultLoc == nil {
		defaultLoc = time.UTC
	}
	cal, err := goical.NewDecoder(strings.NewReader(ics)).Decode()
	if err != nil {
		return nil, err
	}
	tz := buildTZMap(cal, defaultLoc)

	var out []Event
	for i := range cal.Events() {
		ev := cal.Events()[i]
		parsed, err := parseEvent(&ev, tz, defaultLoc)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

// parseEvent maps one VEVENT to an Event. It returns an error on semantic
// failures — a missing UID, an unparseable DTSTART/DTEND, or an invalid
// RECURRENCE-ID/RRULE — rather than silently degrading to zero values. That
// matters because sync treats an unparseable resource as unfetched (withholding
// the calendar ctag) instead of committing a degenerate event and dropping the
// real one; e.g. an unparseable RECURRENCE-ID would otherwise collapse an
// override into a master and collide with the true master on (uid, "").
func parseEvent(ev *goical.Event, tz tzMap, defaultLoc *time.Location) (Event, error) {
	e := Event{
		UID:         text(ev.Props, "UID"),
		Summary:     text(ev.Props, goical.PropSummary),
		Description: text(ev.Props, goical.PropDescription),
		Location:    text(ev.Props, goical.PropLocation),
		Status:      strings.ToUpper(text(ev.Props, goical.PropStatus)),
		Organizer:   cleanMailto(text(ev.Props, "ORGANIZER")),
	}
	if e.UID == "" {
		return e, fmt.Errorf("ical: VEVENT missing UID")
	}
	if seq := text(ev.Props, "SEQUENCE"); seq != "" {
		e.Sequence, _ = strconv.Atoi(seq)
	}
	if lm := ev.Props.Get("LAST-MODIFIED"); lm != nil {
		e.LastModified, _, _ = parseValue(lm, tz, defaultLoc)
	}

	p := ev.Props.Get(goical.PropDateTimeStart)
	if p == nil {
		return e, fmt.Errorf("ical: VEVENT %q missing DTSTART", e.UID)
	}
	var err error
	if e.Start, e.AllDay, err = parseValue(p, tz, defaultLoc); err != nil {
		return e, fmt.Errorf("ical: VEVENT %q DTSTART %q: %w", e.UID, p.Value, err)
	}
	if p := ev.Props.Get(goical.PropDateTimeEnd); p != nil {
		if e.End, _, err = parseValue(p, tz, defaultLoc); err != nil {
			return e, fmt.Errorf("ical: VEVENT %q DTEND %q: %w", e.UID, p.Value, err)
		}
	}

	if p := ev.Props.Get("RECURRENCE-ID"); p != nil {
		t, allDay, err := parseValue(p, tz, defaultLoc)
		if err != nil {
			return e, fmt.Errorf("ical: VEVENT %q RECURRENCE-ID %q: %w", e.UID, p.Value, err)
		}
		e.RecurrenceIDRaw = p.Value
		e.RecurrenceIDKey = OccurrenceKey(t, allDay)
	}
	if p := ev.Props.Get("RRULE"); p != nil {
		e.RRule = p.Value
		opt, err := ev.Props.RecurrenceRule()
		if err != nil {
			return e, fmt.Errorf("ical: VEVENT %q RRULE %q: %w", e.UID, p.Value, err)
		}
		if opt != nil {
			if !e.Start.IsZero() {
				opt.Dtstart = e.Start
			}
			e.RRuleOption = opt
		}
	}
	for _, p := range ev.Props.Values("EXDATE") {
		for _, part := range strings.Split(p.Value, ",") {
			pp := goical.Prop{Name: "EXDATE", Params: p.Params, Value: part}
			if t, allDay, err := parseValue(&pp, tz, defaultLoc); err == nil {
				e.ExDates = append(e.ExDates, OccurrenceKey(t, allDay))
			}
		}
	}
	for _, p := range ev.Props.Values("ATTENDEE") {
		e.Attendees = append(e.Attendees, Attendee{
			Name:           p.Params.Get("CN"),
			Email:          cleanMailto(p.Value),
			ResponseStatus: p.Params.Get("PARTSTAT"),
		})
	}
	return e, nil
}

// parseValue parses a DATE or DATE-TIME prop, resolving TZID against the
// embedded VTIMEZONE map (go-ical would hard-fail on non-IANA TZIDs).
func parseValue(p *goical.Prop, tz tzMap, defaultLoc *time.Location) (t time.Time, allDay bool, err error) {
	v := p.Value
	if strings.EqualFold(p.Params.Get("VALUE"), "DATE") || len(v) == len(dateFormat) {
		t, err = time.ParseInLocation(dateFormat, v, defaultLoc)
		return t, true, err
	}
	if strings.HasSuffix(v, "Z") {
		t, err = time.ParseInLocation(dateTimeUTC, v, time.UTC)
		return t, false, err
	}
	loc := defaultLoc
	if tzid := p.Params.Get(goical.PropTimezoneID); tzid != "" {
		loc = tz.resolve(tzid, defaultLoc)
	}
	t, err = time.ParseInLocation(dateTimeFormat, v, loc)
	return t, false, err
}

// OccurrenceKey normalizes an instant to the stable per-occurrence key used for
// recurrence-id / exdate / instance identity: UTC milliseconds for date-times,
// YYYY-MM-DD for all-day.
func OccurrenceKey(t time.Time, allDay bool) string {
	if allDay {
		return t.Format("2006-01-02")
	}
	return strconv.FormatInt(t.UTC().UnixMilli(), 10)
}

func cleanMailto(v string) string {
	return strings.TrimPrefix(strings.TrimPrefix(v, "mailto:"), "MAILTO:")
}

func text(props goical.Props, name string) string {
	if p := props.Get(name); p != nil {
		return strings.TrimSpace(p.Value)
	}
	return ""
}
