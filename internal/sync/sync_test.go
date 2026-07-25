package sync

import (
	"context"
	"path/filepath"
	stdsync "sync"
	"testing"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/store"
	"github.com/angelmsger/wecom-calendar-cli/pkg/caldav"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// fakeClient is a scripted CalDAV client: it returns a fixed set of calendars,
// event refs per calendar, and bodies per href, and records which hrefs were
// GET-ed so tests can assert what was (not) fetched.
type fakeClient struct {
	calendars []caldav.Calendar
	refs      map[string][]caldav.EventRef // calendar href -> refs
	bodies    map[string]caldav.RawEvent   // event href -> body (or zero + err)
	errs      map[string]error             // event href -> GetEvent error

	mu   stdsync.Mutex
	gets map[string]int // href -> times GetEvent was called
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
	f.mu.Lock()
	if f.gets == nil {
		f.gets = map[string]int{}
	}
	f.gets[href]++
	f.mu.Unlock()
	if err := f.errs[href]; err != nil {
		return caldav.RawEvent{}, err
	}
	return f.bodies[href], nil
}

func (f *fakeClient) getCount(href string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets[href]
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

// TestSyncParseFailureRecordedNotBlocking: an unparseable resource (with a
// getetag) is recorded known-bad and does not advance its own etag, so it never
// masquerades as a good event; but, like a 404, it must not block the calendar's
// ctag forever. --full re-attempts it (e.g. after a parser fix).
func TestSyncParseFailureRecordedNotBlocking(t *testing.T) {
	st := openStore(t)
	c := &fakeClient{
		calendars: []caldav.Calendar{{ID: "c1", Href: "/calendar/c1/", Ctag: "ctag1"}},
		refs:      map[string][]caldav.EventRef{"/calendar/c1/": {{Href: "/calendar/c1/bad.ics", Etag: "e1"}}},
		bodies:    map[string]caldav.RawEvent{"/calendar/c1/bad.ics": {Href: "/calendar/c1/bad.ics", Etag: "e1", ICS: "this is not iCalendar"}},
	}
	run(t, c, st)

	ctag, ok, err := st.CalendarState("c1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || ctag != "ctag1" {
		t.Fatalf("ctag must commit with the unparseable resource recorded, got ok=%v ctag=%q", ok, ctag)
	}
	if fails, _ := st.ResourceFailures("c1"); fails["/calendar/c1/bad.ics"] != "e1" {
		t.Fatalf("unparseable resource should be recorded known-bad, got %v", fails)
	}
	// No live event was created from the garbage.
	if masters, _ := st.MastersForExpansion(); len(masters) != 0 {
		t.Fatalf("no event should exist for unparseable content, got %d", len(masters))
	}
}

// TestSyncPermanent404DoesNotBlockCtag is the performance-regression guard: a
// resource the server lists but 404s (with a getetag) must be recorded as
// known-bad and NOT withhold the calendar's ctag — otherwise one permanently
// broken resource forces the whole calendar to re-scan forever.
func TestSyncPermanent404DoesNotBlockCtag(t *testing.T) {
	st := openStore(t)
	notFound := cerrors.New(cerrors.CategoryNotFound, "CALDAV_NOT_FOUND", "gone")
	c := &fakeClient{
		calendars: []caldav.Calendar{{ID: "c1", Href: "/calendar/c1/", Ctag: "ctag1"}},
		refs:      map[string][]caldav.EventRef{"/calendar/c1/": {{Href: "/calendar/c1/x.ics", Etag: "e1"}}},
		errs:      map[string]error{"/calendar/c1/x.ics": notFound},
	}
	run(t, c, st)

	ctag, ok, err := st.CalendarState("c1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || ctag != "ctag1" {
		t.Fatalf("ctag must commit despite a recorded 404, got ok=%v ctag=%q", ok, ctag)
	}
	if fails, _ := st.ResourceFailures("c1"); fails["/calendar/c1/x.ics"] != "e1" {
		t.Fatalf("the 404 should be recorded as known-bad with its getetag, got %v", fails)
	}
}

// TestSyncSkipsKnownBadOnRescan: once a resource is recorded known-bad, a later
// re-scan of the calendar (ctag changed) must not re-fetch it, while still
// fetching genuinely new resources.
func TestSyncSkipsKnownBadOnRescan(t *testing.T) {
	st := openStore(t)
	notFound := cerrors.New(cerrors.CategoryNotFound, "CALDAV_NOT_FOUND", "gone")
	c := &fakeClient{
		calendars: []caldav.Calendar{{ID: "c1", Href: "/calendar/c1/", Ctag: "ctag1"}},
		refs:      map[string][]caldav.EventRef{"/calendar/c1/": {{Href: "/calendar/c1/bad.ics", Etag: "b1"}}},
		errs:      map[string]error{"/calendar/c1/bad.ics": notFound},
	}
	run(t, c, st) // records bad.ics as known-bad, commits ctag1

	// Calendar changes (new ctag) and a new good resource appears; bad.ics still
	// listed with the same getetag.
	c.calendars[0].Ctag = "ctag2"
	c.refs["/calendar/c1/"] = []caldav.EventRef{
		{Href: "/calendar/c1/bad.ics", Etag: "b1"},
		{Href: "/calendar/c1/good.ics", Etag: "g1"},
	}
	c.bodies = map[string]caldav.RawEvent{"/calendar/c1/good.ics": {Href: "/calendar/c1/good.ics", Etag: "g1", ICS: goodICS}}
	run(t, c, st)

	if n := c.getCount("/calendar/c1/bad.ics"); n != 1 {
		t.Fatalf("known-bad resource must not be re-fetched on re-scan (want 1 GET total, got %d)", n)
	}
	if n := c.getCount("/calendar/c1/good.ics"); n != 1 {
		t.Fatalf("the new good resource should be fetched once, got %d", n)
	}
}

// TestSyncFullRetriesKnownBad: --full ignores the known-bad record and re-attempts.
func TestSyncFullRetriesKnownBad(t *testing.T) {
	st := openStore(t)
	notFound := cerrors.New(cerrors.CategoryNotFound, "CALDAV_NOT_FOUND", "gone")
	c := &fakeClient{
		calendars: []caldav.Calendar{{ID: "c1", Href: "/calendar/c1/", Ctag: "ctag1"}},
		refs:      map[string][]caldav.EventRef{"/calendar/c1/": {{Href: "/calendar/c1/bad.ics", Etag: "b1"}}},
		errs:      map[string]error{"/calendar/c1/bad.ics": notFound},
	}
	run(t, c, st)

	if _, err := Run(context.Background(), c, st, Options{
		Full: true, Since: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), Loc: time.UTC,
	}); err != nil {
		t.Fatal(err)
	}
	if n := c.getCount("/calendar/c1/bad.ics"); n != 2 {
		t.Fatalf("--full should retry the known-bad resource (want 2 GETs, got %d)", n)
	}
}

// TestSyncSecondSyncSkipsUnchanged is the end-to-end incremental guard: a good
// resource fetched once is not re-fetched on the next sync (getetag matches).
func TestSyncSecondSyncSkipsUnchanged(t *testing.T) {
	st := openStore(t)
	c := &fakeClient{
		calendars: []caldav.Calendar{{ID: "c1", Href: "/calendar/c1/", Ctag: "ctag1"}},
		refs:      map[string][]caldav.EventRef{"/calendar/c1/": {{Href: "/calendar/c1/u1.ics", Etag: "e1"}}},
		bodies:    map[string]caldav.RawEvent{"/calendar/c1/u1.ics": {Href: "/calendar/c1/u1.ics", Etag: "DIFFERENT-get-etag", ICS: goodICS}},
	}
	run(t, c, st) // fetches once; stores the getetag "e1", not the GET etag
	run(t, c, st) // ctag unchanged -> whole calendar skipped

	if n := c.getCount("/calendar/c1/u1.ics"); n != 1 {
		t.Fatalf("unchanged resource must not be re-fetched (want 1 GET, got %d)", n)
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
