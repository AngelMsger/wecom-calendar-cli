package store

import (
	"testing"
	"time"
)

func inst(uid string, startMs, endMs int64) InstanceRow {
	return InstanceRow{
		UID:               uid,
		OccurrenceKey:     uid + "-k",
		PrimaryCalendarID: "c1",
		SourceCalendarIDs: `["c1"]`,
		SourceCount:       1,
		Summary:           uid,
		Start:             time.UnixMilli(startMs).UTC(),
		End:               time.UnixMilli(endMs).UTC(),
	}
}

// TestQueryInstancesOverlap guards the overlap semantics: an occurrence that
// began before the window but is still running must be returned, while one that
// ended at or before the window start must not.
func TestQueryInstancesOverlap(t *testing.T) {
	st := openTemp(t)
	// window will be [150, 300)
	mustInsert(t, st, inst("spanning", 100, 200)) // starts before window, ends inside -> in
	mustInsert(t, st, inst("inside", 160, 170))   // fully inside -> in
	mustInsert(t, st, inst("before", 50, 150))    // ends exactly at since -> out
	mustInsert(t, st, inst("after", 300, 310))    // starts at until (exclusive) -> out

	rows, hasMore, err := st.QueryInstances(150, 300, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if hasMore {
		t.Fatal("unbounded query should not report has_more")
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.UID] = true
	}
	if !got["spanning"] || !got["inside"] {
		t.Fatalf("want spanning+inside occurrences, got %v", got)
	}
	if got["before"] || got["after"] {
		t.Fatalf("window boundaries leaked in: %v", got)
	}
}

// TestQueryInstancesPagination guards the cursor contract: limit bounds the page
// and has_more is reported accurately across offsets.
func TestQueryInstancesPagination(t *testing.T) {
	st := openTemp(t)
	for _, u := range []string{"a", "b", "c", "d", "e"} {
		start := int64(200 + int(u[0]))
		mustInsert(t, st, inst(u, start, start+5))
	}
	page1, more1, err := st.QueryInstances(100, 1000, "", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 || !more1 {
		t.Fatalf("page1: want 2 rows + has_more, got %d rows more=%v", len(page1), more1)
	}
	page3, more3, err := st.QueryInstances(100, 1000, "", 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page3) != 1 || more3 {
		t.Fatalf("page3: want 1 row + no has_more, got %d rows more=%v", len(page3), more3)
	}
}

func mustInsert(t *testing.T, st *Store, r InstanceRow) {
	t.Helper()
	if err := st.InsertInstance(r); err != nil {
		t.Fatal(err)
	}
}
