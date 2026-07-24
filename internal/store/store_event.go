package store

import "database/sql"

// AttendeeOut is one participant of an event, for `event get` output. IsSelf is
// filled by the command layer (it knows the configured account); the store
// leaves it false.
type AttendeeOut struct {
	Email          string `json:"email,omitempty"`
	Name           string `json:"name,omitempty"`
	ResponseStatus string `json:"response_status,omitempty"`
	IsSelf         bool   `json:"is_self"`
}

// EventDetailOut is the full record for a single event, surfacing the fields the
// expanded-instance view omits (description, location, organizer, attendees).
// Metadata is attached by the command layer when --include-meta is set.
type EventDetailOut struct {
	UID          string        `json:"uid"`
	CalendarID   string        `json:"calendar_id"`
	CalendarName string        `json:"calendar_name,omitempty"`
	Summary      string        `json:"summary"`
	Description  string        `json:"description,omitempty"`
	Location     string        `json:"location,omitempty"`
	Status       string        `json:"status,omitempty"`
	Start        string        `json:"start,omitempty"`
	End          string        `json:"end,omitempty"`
	AllDay       bool          `json:"all_day"`
	Recurring    bool          `json:"recurring"`
	RRule        string        `json:"rrule,omitempty"`
	Organizer    string        `json:"organizer,omitempty"`
	Sequence     int           `json:"sequence,omitempty"`
	LastModified string        `json:"last_modified,omitempty"`
	RecurrenceID string        `json:"recurrence_id,omitempty"` // set when an occurrence override was merged
	Attendees    []AttendeeOut `json:"attendees"`
	Metadata     []MetaRow     `json:"metadata,omitempty"`
}

// EventDetail returns the full record for a live event by uid, or nil if none
// exists. When the same uid appears under several calendars, the primary is
// chosen deterministically (most-recently-modified, then highest sequence, then
// lexicographic calendar id) — the same ordering the expansion uses. When
// occurrenceKey is non-empty and a matching RECURRENCE-ID override exists, its
// non-empty fields (and its attendees, if any) override the master's so a
// specific occurrence reads accurately.
func (s *Store) EventDetail(uid, occurrenceKey string) (*EventDetailOut, error) {
	row := s.db.QueryRow(
		`SELECT e.calendar_id, COALESCE(c.display_name,''), COALESCE(e.summary,''),
		        COALESCE(e.description,''), COALESCE(e.location,''), COALESCE(e.status,''),
		        COALESCE(e.dtstart_at,''), COALESCE(e.dtend_at,''), e.all_day,
		        COALESCE(e.rrule,''), COALESCE(e.organizer,''), COALESCE(e.sequence,0),
		        COALESCE(e.last_modified_at,'')
		   FROM calendar_events e
		   LEFT JOIN calendars c ON c.calendar_id = e.calendar_id
		  WHERE e.uid=? AND e.recurrence_id_key='' AND e.deleted_at IS NULL
		  ORDER BY e.last_modified_at_ms DESC, e.sequence DESC, e.calendar_id ASC
		  LIMIT 1`, uid)

	var d EventDetailOut
	var allDay int
	switch err := row.Scan(&d.CalendarID, &d.CalendarName, &d.Summary, &d.Description,
		&d.Location, &d.Status, &d.Start, &d.End, &allDay, &d.RRule, &d.Organizer,
		&d.Sequence, &d.LastModified); err {
	case nil:
	case sql.ErrNoRows:
		return nil, nil
	default:
		return nil, err
	}
	d.UID = uid
	d.AllDay = allDay != 0
	d.Recurring = d.RRule != ""

	att, err := s.attendees(d.CalendarID, uid, "")
	if err != nil {
		return nil, err
	}
	d.Attendees = att

	if occurrenceKey != "" {
		if err := s.mergeOccurrence(&d, uid, occurrenceKey); err != nil {
			return nil, err
		}
	}
	return &d, nil
}

// attendees returns the participants of one (calendar, uid, recurrence) event.
func (s *Store) attendees(calID, uid, recKey string) ([]AttendeeOut, error) {
	rows, err := s.db.Query(
		`SELECT email, COALESCE(name,''), COALESCE(response_status,'')
		   FROM event_attendees
		  WHERE calendar_id=? AND uid=? AND recurrence_id_key=?
		  ORDER BY email`, calID, uid, recKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AttendeeOut
	for rows.Next() {
		var a AttendeeOut
		if err := rows.Scan(&a.Email, &a.Name, &a.ResponseStatus); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// mergeOccurrence overlays a RECURRENCE-ID override's non-empty fields onto the
// master detail. A missing override is not an error (the key simply had no
// exception); the master stands.
func (s *Store) mergeOccurrence(d *EventDetailOut, uid, key string) error {
	row := s.db.QueryRow(
		`SELECT COALESCE(summary,''), COALESCE(description,''), COALESCE(location,''),
		        COALESCE(status,''), COALESCE(dtstart_at,''), COALESCE(dtend_at,''),
		        COALESCE(recurrence_id_raw,'')
		   FROM calendar_events
		  WHERE calendar_id=? AND uid=? AND recurrence_id_key=? AND deleted_at IS NULL`,
		d.CalendarID, uid, key)
	var summary, desc, loc, status, start, end, ridRaw string
	switch err := row.Scan(&summary, &desc, &loc, &status, &start, &end, &ridRaw); err {
	case nil:
	case sql.ErrNoRows:
		return nil
	default:
		return err
	}
	d.RecurrenceID = ridRaw
	overlay := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	overlay(&d.Summary, summary)
	overlay(&d.Description, desc)
	overlay(&d.Location, loc)
	overlay(&d.Status, status)
	overlay(&d.Start, start)
	overlay(&d.End, end)
	if att, err := s.attendees(d.CalendarID, uid, key); err != nil {
		return err
	} else if len(att) > 0 {
		d.Attendees = att
	}
	return nil
}
