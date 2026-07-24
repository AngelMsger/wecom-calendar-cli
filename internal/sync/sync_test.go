package sync

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/store"
	"github.com/angelmsger/wecom-calendar-cli/pkg/caldav"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// fakeClient is a scripted CalDAV client: it returns a fixed set of calendars,
// event refs per calendar, and bodies per href.
type fakeClient struct {
	calendars []caldav.Calendar
	refs      map[string][]caldav.EventRef // calendar href -> refs
	bodies    map[string]caldav.RawEvent   // event href -> body (or zero + err)
	errs      map[string]error             // event href -> GetEvent error
}

func (f *fakeClient) ServerURL() string          { return "https://caldav.test/" }
func (f *fakeClient) Ping(context.Context) error { return nil }
func (f *fakeClient) ListCalendars(context.Context) ([]caldav.Calendar, error) {
	return f.calendars, nil
}
func (f *fakeClient) ListEvents(_ context.Context, calHref string, _, _ time.Time) ([]caldav.EventRef, error) {
	return f.refs[calHref], nil
}
func (f *fakeClient) GetEvent(_ context.Context, href string) (caldav.RawEvent, error) {
	if err := f.errs[href]; err != nil {
		return caldav.RawEvent{}, err
	}
	return f.bodies[href], nil
}

const goodICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:u1\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\n" +
	"SUMMARY:Test\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func run(t *testing.T, c caldav.Client, st *store.Store) Result {
	t.Helper()
	res, err := Run(context.Background(), c, st, Options{
		Since: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		Loc:   time.UTC,
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return res
}

// TestSyncGoodEventCommitsCtag is the happy path: a parseable event lands and
// the calendar ctag is committed so the next sync can skip it.
func TestSyncGoodEventCommitsCtag(t *testing.T) {
	st := openStore(t)
	c := &fakeClient{
		calendars: []caldav.Calendar{{ID: "c1", Href: "/calendar/c1/", Ctag: "ctag1"}},
		refs:      map[string][]caldav.EventRef{"/calendar/c1/": {{Href: "/calendar/c1/u1.ics", Etag: "e1"}}},
		bodies:    map[string]caldav.RawEvent{"/calendar/c1/u1.ics": {Href: "/calendar/c1/u1.ics", Etag: "e1", ICS: goodICS}},
	}
	run(t, c, st)

	ctag, ok, err := st.CalendarState("c1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || ctag != "ctag1" {
		t.Fatalf("want committed ctag1, got ok=%v ctag=%q", ok, ctag)
	}
}

// TestSyncParseFailureWithholdsCtag guards the data-loss bug: an unparseable
// resource must not advance the calendar ctag, so the next sync re-lists and
// retries rather than skipping the calendar forever.
func TestSyncParseFailureWithholdsCtag(t *testing.T) {
	st := openStore(t)
	c := &fakeClient{
		calendars: []caldav.Calendar{{ID: "c1", Href: "/calendar/c1/", Ctag: "ctag1"}},
		refs:      map[string][]caldav.EventRef{"/calendar/c1/": {{Href: "/calendar/c1/bad.ics", Etag: "e1"}}},
		bodies:    map[string]caldav.RawEvent{"/calendar/c1/bad.ics": {Href: "/calendar/c1/bad.ics", ICS: "this is not iCalendar"}},
	}
	run(t, c, st)

	if _, ok, err := st.CalendarState("c1"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("ctag was committed despite a parse failure; the calendar would be skipped and the event lost")
	}
}

// TestSyncUnfetchable404WithholdsCtag: a 404 on a listed resource is non-fatal
// but must also withhold the ctag so the calendar is retried.
func TestSyncUnfetchable404WithholdsCtag(t *testing.T) {
	st := openStore(t)
	notFound := cerrors.New(cerrors.CategoryNotFound, "CALDAV_NOT_FOUND", "gone")
	c := &fakeClient{
		calendars: []caldav.Calendar{{ID: "c1", Href: "/calendar/c1/", Ctag: "ctag1"}},
		refs:      map[string][]caldav.EventRef{"/calendar/c1/": {{Href: "/calendar/c1/x.ics", Etag: "e1"}}},
		errs:      map[string]error{"/calendar/c1/x.ics": notFound},
	}
	run(t, c, st)

	if _, ok, _ := st.CalendarState("c1"); ok {
		t.Fatal("ctag committed despite an unfetchable resource")
	}
}

// TestSyncEmitsBoundedProgress: a sync reports its scale up front and one
// milestone per scanned calendar, without a per-resource flood.
func TestSyncEmitsBoundedProgress(t *testing.T) {
	st := openStore(t)
	c := &fakeClient{
		calendars: []caldav.Calendar{{ID: "c1", Href: "/calendar/c1/", Ctag: "ctag1"}},
		refs:      map[string][]caldav.EventRef{"/calendar/c1/": {{Href: "/calendar/c1/u1.ics", Etag: "e1"}}},
		bodies:    map[string]caldav.RawEvent{"/calendar/c1/u1.ics": {Href: "/calendar/c1/u1.ics", Etag: "e1", ICS: goodICS}},
	}
	var events []ProgressEvent
	_, err := Run(context.Background(), c, st, Options{
		Since:    time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		Until:    time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		Loc:      time.UTC,
		Progress: func(ev ProgressEvent) { events = append(events, ev) },
	})
	if err != nil {
		t.Fatal(err)
	}
	var gotStart, gotDone bool
	for _, e := range events {
		if e.Phase == ProgressStart && e.CalendarsTotal == 1 {
			gotStart = true
		}
		if e.Phase == ProgressCalendarDone && e.CalendarID == "c1" && e.EventsUpserted >= 1 {
			gotDone = true
		}
	}
	if !gotStart {
		t.Errorf("want a start event reporting the scale, got %+v", events)
	}
	if !gotDone {
		t.Errorf("want a calendar_done milestone for c1, got %+v", events)
	}
	if len(events) > 5 {
		t.Errorf("progress must be bounded (no per-resource flood), got %d events", len(events))
	}
}

// TestSyncCascadesRemovedCalendar guards the ghost-calendar bug: when a calendar
// disappears from the server, its events must stop appearing (be soft-deleted),
// not linger in queries.
func TestSyncCascadesRemovedCalendar(t *testing.T) {
	st := openStore(t)
	c := &fakeClient{
		calendars: []caldav.Calendar{{ID: "c1", Href: "/calendar/c1/", Ctag: "ctag1"}},
		refs:      map[string][]caldav.EventRef{"/calendar/c1/": {{Href: "/calendar/c1/u1.ics", Etag: "e1"}}},
		bodies:    map[string]caldav.RawEvent{"/calendar/c1/u1.ics": {Href: "/calendar/c1/u1.ics", Etag: "e1", ICS: goodICS}},
	}
	run(t, c, st)
	if masters, _ := st.MastersForExpansion(); len(masters) != 1 {
		t.Fatalf("want 1 master after first sync, got %d", len(masters))
	}

	// Second sync: the server no longer lists c1.
	c.calendars = nil
	run(t, c, st)
	masters, err := st.MastersForExpansion()
	if err != nil {
		t.Fatal(err)
	}
	if len(masters) != 0 {
		t.Fatalf("removed calendar's events still live: %d masters", len(masters))
	}
}
