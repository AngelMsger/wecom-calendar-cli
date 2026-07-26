package app

import "testing"

// TestScanDecision pins the reasons `sync --dry-run` reports. They are what a
// user reads when diagnosing a slow sync, so a wrong label sends them chasing
// the wrong cause.
func TestScanDecision(t *testing.T) {
	const (
		neverSynced = "never synced"
		noChangeTag = "server sends no change-tag, so this calendar is re-listed every sync"
	)
	cases := []struct {
		name                          string
		full, storeExists, stateFound bool
		storedCtag, serverCtag        string
		wantScan                      bool
		wantReason                    string
	}{
		// The regression this test exists for: a calendar the server gives no
		// getctag for has an empty stored ctag too, which is indistinguishable
		// from "never synced" unless the server value is checked first. It may
		// have synced many times; reporting it as never synced is a lie.
		{"no server change-tag", false, true, true, "", "", true, noChangeTag},
		{"no server change-tag, nothing stored yet", false, true, false, "", "", true, noChangeTag},

		{"genuinely never synced", false, true, false, "", "abc", true, neverSynced},
		{"stored ctag blank but server has one", false, true, true, "", "abc", true, neverSynced},
		{"no store on disk at all", false, false, false, "", "abc", true, neverSynced},
		{"unchanged", false, true, true, "abc", "abc", false, "ctag unchanged"},
		{"changed", false, true, true, "abc", "def", true, "ctag changed"},
		{"full overrides everything", true, true, true, "abc", "abc", true, "full rescan"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scan, reason := scanDecision(tc.full, tc.storeExists, tc.stateFound, tc.storedCtag, tc.serverCtag)
			if scan != tc.wantScan || reason != tc.wantReason {
				t.Fatalf("want (%v, %q), got (%v, %q)", tc.wantScan, tc.wantReason, scan, reason)
			}
		})
	}
}
