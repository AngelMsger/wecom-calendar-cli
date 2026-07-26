package store

import (
	"path/filepath"
	"testing"
	"time"
)

func filterStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// TestQueryInstancesCalendarFilterEscapesLike guards the `--calendar` filter
// against LIKE metacharacters in a calendar id. `_` matches any single character
// in an unescaped LIKE, so without an ESCAPE clause the id "a_c" would also
// select events whose only source is "abc".
func TestQueryInstancesCalendarFilterEscapesLike(t *testing.T) {
	st := filterStore(t)
	start := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	rows := []InstanceRow{
		{UID: "wanted", OccurrenceKey: "k1", PrimaryCalendarID: "other",
			SourceCalendarIDs: `["other","a_c"]`, SourceCount: 2, Summary: "wanted",
			Start: start, End: start.Add(time.Hour), LocalDate: "2026-07-01"},
		{UID: "decoy", OccurrenceKey: "k2", PrimaryCalendarID: "other",
			SourceCalendarIDs: `["other","abc"]`, SourceCount: 2, Summary: "decoy",
			Start: start, End: start.Add(time.Hour), LocalDate: "2026-07-01"},
	}
	if err := st.ReplaceInstances(rows, 0, 1<<62); err != nil {
		t.Fatalf("replace instances: %v", err)
	}

	got, _, err := st.QueryInstances(0, 1<<62, "a_c", nil, nil, 0)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 1 || got[0].UID != "wanted" {
		var uids []string
		for _, g := range got {
			uids = append(uids, g.UID)
		}
		t.Fatalf(`--calendar "a_c" must not match the source id "abc"; got %v`, uids)
	}
}

// TestDeleteResourceFailuresChunks exercises the batched delete past the chunk
// size, which is what keeps a large calendar under SQLite's host-parameter cap.
func TestDeleteResourceFailuresChunks(t *testing.T) {
	st := filterStore(t)
	now := time.Unix(1_700_000_000, 0)
	var hrefs []string
	for i := range deleteChunk*2 + 7 {
		href := "/calendar/c1/" + string(rune('a'+i%26)) + "-" + itoa(i) + ".ics"
		hrefs = append(hrefs, href)
		if err := st.RecordResourceFailure("c1", href, "etag", "unfetchable", now); err != nil {
			t.Fatalf("record failure: %v", err)
		}
	}
	// Keep one; delete the rest.
	if err := st.DeleteResourceFailures("c1", hrefs[1:]); err != nil {
		t.Fatalf("delete failures: %v", err)
	}
	left, err := st.ResourceFailures("c1")
	if err != nil {
		t.Fatalf("read failures: %v", err)
	}
	if len(left) != 1 {
		t.Fatalf("want 1 remaining failure, got %d", len(left))
	}
	if _, ok := left[hrefs[0]]; !ok {
		t.Fatalf("the retained href is missing from %v", left)
	}
}

// TestDeleteResourceFailuresEmpty is a no-op, not an error or a full wipe.
func TestDeleteResourceFailuresEmpty(t *testing.T) {
	st := filterStore(t)
	now := time.Unix(1_700_000_000, 0)
	if err := st.RecordResourceFailure("c1", "/a.ics", "e", "unparseable", now); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteResourceFailures("c1", nil); err != nil {
		t.Fatalf("empty delete must be a no-op: %v", err)
	}
	left, err := st.ResourceFailures("c1")
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 {
		t.Fatalf("an empty delete set must remove nothing, got %d rows", len(left))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
