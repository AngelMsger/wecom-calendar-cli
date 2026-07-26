package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/store"
)

func expandTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// TestResolveExpandWindowDefaultsWhenUnpinned: with no pin recorded, the window
// is the rolling default.
func TestResolveExpandWindowDefaultsWhenUnpinned(t *testing.T) {
	st := expandTestStore(t)

	start, end, pinned := resolveExpandWindow(st)
	if pinned {
		t.Fatal("a fresh store must not report a pinned window")
	}
	if d := start.Sub(expandWindowStart()).Abs(); d > time.Minute {
		t.Fatalf("unpinned start should be the rolling default, differed by %v", d)
	}
	if d := end.Sub(expandWindowEnd()).Abs(); d > time.Minute {
		t.Fatalf("unpinned end should be the rolling default, differed by %v", d)
	}
}

// TestExpandPinSurvivesResolve is the regression guard for the bug where every
// `sync` silently reverted a widened expansion window: once `expand
// --since/--until` pins a window, the window a later sync resolves must be that
// window, not the rolling default.
func TestExpandPinSurvivesResolve(t *testing.T) {
	st := expandTestStore(t)
	want0 := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	want1 := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := setExpandPin(st, true, want0, want1); err != nil {
		t.Fatalf("set pin: %v", err)
	}
	start, end, pinned := resolveExpandWindow(st)
	if !pinned {
		t.Fatal("a pinned window must be reported as pinned")
	}
	if !start.Equal(want0) || !end.Equal(want1) {
		t.Fatalf("want pinned window %s..%s, got %s..%s", want0, want1, start, end)
	}
}

// TestExpandPinClearedByBareExpand: running `expand` with no flags forgets the
// pin, so there is a documented way back to the rolling default.
func TestExpandPinClearedByBareExpand(t *testing.T) {
	st := expandTestStore(t)
	pinStart := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	pinEnd := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := setExpandPin(st, true, pinStart, pinEnd); err != nil {
		t.Fatalf("set pin: %v", err)
	}

	if err := setExpandPin(st, false, expandWindowStart(), expandWindowEnd()); err != nil {
		t.Fatalf("clear pin: %v", err)
	}
	if _, _, pinned := resolveExpandWindow(st); pinned {
		t.Fatal("expand with no flags must clear the pin")
	}
}

// TestResolveExpandWindowIgnoresCorruptPin: a half-written or nonsensical pin
// falls back to the default rather than producing an inverted window.
func TestResolveExpandWindowIgnoresCorruptPin(t *testing.T) {
	for _, tc := range []struct{ name, start, end string }{
		{"unparseable", "not-a-number", "1700000000000"},
		{"inverted", "1800000000000", "1700000000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := expandTestStore(t)
			if err := st.SetSyncMeta(store.MetaPinnedStartMs, tc.start); err != nil {
				t.Fatal(err)
			}
			if err := st.SetSyncMeta(store.MetaPinnedEndMs, tc.end); err != nil {
				t.Fatal(err)
			}
			if _, _, pinned := resolveExpandWindow(st); pinned {
				t.Fatal("a corrupt pin must fall back to the default window")
			}
		})
	}
}
