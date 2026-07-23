package ical

import (
	"strconv"
	"strings"
	"time"

	goical "github.com/emersion/go-ical"
)

// tzMap maps a TZID to a fixed-offset location derived from an embedded
// VTIMEZONE. The WeCom server's TZIDs (e.g. "TZ08") are not IANA names, so a
// FixedZone from the declared TZOFFSETTO is the faithful interpretation.
type tzMap map[string]*time.Location

func buildTZMap(cal *goical.Calendar, _ *time.Location) tzMap {
	m := tzMap{}
	for _, child := range cal.Children {
		if child.Name != goical.CompTimezone {
			continue
		}
		p := child.Props.Get(goical.PropTimezoneID)
		if p == nil || p.Value == "" {
			continue
		}
		if off, ok := offsetFromVTIMEZONE(child); ok {
			m[p.Value] = time.FixedZone(p.Value, off)
		}
	}
	return m
}

// resolve maps a TZID to a location: the embedded VTIMEZONE first, then a real
// IANA name, then the default.
func (m tzMap) resolve(tzid string, defaultLoc *time.Location) *time.Location {
	if loc, ok := m[tzid]; ok {
		return loc
	}
	if loc, err := time.LoadLocation(tzid); err == nil {
		return loc
	}
	return defaultLoc
}

// offsetFromVTIMEZONE returns the UTC offset (seconds) for a VTIMEZONE,
// preferring the STANDARD sub-component's TZOFFSETTO, then DAYLIGHT.
func offsetFromVTIMEZONE(comp *goical.Component) (int, bool) {
	for _, want := range []string{goical.CompTimezoneStandard, goical.CompTimezoneDaylight} {
		for _, sub := range comp.Children {
			if sub.Name != want {
				continue
			}
			if p := sub.Props.Get("TZOFFSETTO"); p != nil {
				if off, ok := parseUTCOffset(p.Value); ok {
					return off, true
				}
			}
		}
	}
	return 0, false
}

// parseUTCOffset parses an iCalendar UTC offset ("+0800", "-0530", "+080000").
func parseUTCOffset(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 5 || (s[0] != '+' && s[0] != '-') {
		return 0, false
	}
	sign := 1
	if s[0] == '-' {
		sign = -1
	}
	hh, err1 := strconv.Atoi(s[1:3])
	mm, err2 := strconv.Atoi(s[3:5])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	sec := hh*3600 + mm*60
	if len(s) >= 7 {
		if ss, err := strconv.Atoi(s[5:7]); err == nil {
			sec += ss
		}
	}
	return sign * sec, true
}
