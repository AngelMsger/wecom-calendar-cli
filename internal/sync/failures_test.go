package sync

import (
	"slices"
	"testing"
)

// TestStaleFailures: only recorded failures the server has stopped listing are
// forgotten. Computing this set here is what keeps the DELETE bound to a
// handful of parameters instead of one per event in the calendar.
func TestStaleFailures(t *testing.T) {
	failed := map[string]string{
		"/c/gone.ics":      "e1",
		"/c/still.ics":     "e2",
		"/c/also-gone.ics": "e3",
	}
	listed := []string{"/c/still.ics", "/c/fresh.ics"}

	got := staleFailures(failed, listed)
	slices.Sort(got)
	want := []string{"/c/also-gone.ics", "/c/gone.ics"}
	if !slices.Equal(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

// TestStaleFailuresNoneRecorded is the common case — no failures at all — and
// must not issue any delete work.
func TestStaleFailuresNoneRecorded(t *testing.T) {
	if got := staleFailures(nil, []string{"/c/a.ics"}); got != nil {
		t.Fatalf("want no stale failures, got %v", got)
	}
}

// TestStaleFailuresEmptyListing: when the server lists nothing, every recorded
// failure is stale.
func TestStaleFailuresEmptyListing(t *testing.T) {
	failed := map[string]string{"/c/a.ics": "e1"}
	got := staleFailures(failed, nil)
	if !slices.Equal(got, []string{"/c/a.ics"}) {
		t.Fatalf("want the only failure reported stale, got %v", got)
	}
}
