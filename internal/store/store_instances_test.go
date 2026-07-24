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
// ended at or before the window start must not. It also checks source_calendar_ids
// comes back as a native array, not a JSON-encoded string.
func TestQueryInstancesOverlap(t *testing.T) {
	st := openTemp(t)
	// window will be [150, 300)
	mustInsert(t, st, inst("spanning", 100, 200)) // starts before window, ends inside -> in
	mustInsert(t, st, inst("inside", 160, 170))   // fully inside -> in
	mustInsert(t, st, inst("before", 50, 150))    // ends exactly at since -> out
	mustInsert(t, st, inst("after", 300, 310))    // starts at until (exclusive) -> out

	rows, next, err := st.QueryInstances(150, 300, "", nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next != nil {
		t.Fatal("unbounded query should not return a continuation cursor")
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.UID] = true
		if len(r.SourceCalendarIDs) != 1 || r.SourceCalendarIDs[0] != "c1" {
			t.Fatalf("source_calendar_ids should be a native []string, got %#v", r.SourceCalendarIDs)
		}
	}
	if !got["spanning"] || !got["inside"] {
		t.Fatalf("want spanning+inside occurrences, got %v", got)
	}
	if got["before"] || got["after"] {
		t.Fatalf("window boundaries leaked in: %v", got)
	}
}

// TestQueryInstancesKeysetPaging guards the cursor contract: each page is bounded
// by limit, the continuation cursor walks the whole set exactly once with no
// gaps or duplicates, and the final page returns no cursor.
func TestQueryInstancesKeysetPaging(t *testing.T) {
	st := openTemp(t)
	for _, u := range []string{"a", "b", "c", "d", "e"} {
		start := int64(200 + int(u[0]))
		mustInsert(t, st, inst(u, start, start+5))
	}

	seen := map[string]bool{}
	var cur *InstanceCursor
	pages := 0
	for {
		page, next, err := st.QueryInstances(100, 1000, "", nil, cur, 2)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, r := range page {
			if seen[r.UID] {
				t.Fatalf("uid %q returned on more than one page", r.UID)
			}
			seen[r.UID] = true
		}
		if next == nil {
			if len(page) == 0 && pages > 1 {
				t.Fatal("final page should carry the last rows, not be empty")
			}
			break
		}
		if len(page) != 2 {
			t.Fatalf("a non-final page must be full (2), got %d", len(page))
		}
		cur = next
	}
	if len(seen) != 5 {
		t.Fatalf("want all 5 distinct occurrences across pages, got %d", len(seen))
	}
}

// TestQueryInstancesStatusFilter: --status keeps only the listed statuses,
// case-insensitively (e.g. drop CANCELLED).
func TestQueryInstancesStatusFilter(t *testing.T) {
	st := openTemp(t)
	confirmed := inst("a", 200, 210)
	confirmed.Status = "CONFIRMED"
	cancelled := inst("b", 220, 230)
	cancelled.Status = "CANCELLED"
	mustInsert(t, st, confirmed)
	mustInsert(t, st, cancelled)

	rows, _, err := st.QueryInstances(100, 1000, "", []string{"confirmed"}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].UID != "a" {
		t.Fatalf("status filter should keep only CONFIRMED, got %+v", rows)
	}
}

func mustInsert(t *testing.T, st *Store, r InstanceRow) {
	t.Helper()
	if err := st.InsertInstance(r); err != nil {
		t.Fatal(err)
	}
}
