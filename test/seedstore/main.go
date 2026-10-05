// Command seedstore writes a few synthetic occurrences into a local store, so
// scripts/e2e.sh can page `event list` without a server.
//
// The e2e suite is offline and has no mock CalDAV server. `event list` reads
// only the derived event_instances table, so seeding that table is enough to
// exercise real pagination: three occurrences in the last few hours and one two
// days ahead, all placed relative to the current time so they fall inside the
// default window whenever the suite runs.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/store"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: seedstore <config-dir>")
		os.Exit(2)
	}
	if err := seed(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "seedstore:", err)
		os.Exit(1)
	}
}

func seed(configDir string) error {
	st, err := store.Open(filepath.Join(configDir, constants.DatabaseFileName))
	if err != nil {
		return err
	}
	defer st.Close()

	now := time.Now()
	var rows []store.InstanceRow
	for _, e := range []struct {
		uid    string
		offset time.Duration
	}{
		{"e2e-a", -3 * time.Hour},
		{"e2e-b", -2 * time.Hour},
		{"e2e-c", -1 * time.Hour},
		{"e2e-ahead", 48 * time.Hour},
	} {
		start := now.Add(e.offset)
		rows = append(rows, store.InstanceRow{
			UID:               e.uid,
			OccurrenceKey:     e.uid + "-k",
			PrimaryCalendarID: "e2e-calendar",
			SourceCalendarIDs: `["e2e-calendar"]`,
			SourceCount:       1,
			Summary:           "Synthetic " + e.uid,
			Start:             start,
			End:               start.Add(30 * time.Minute),
			Status:            "CONFIRMED",
			LocalDate:         start.Format("2006-01-02"),
		})
	}
	// Record the coverage a real rebuild would, so reads print no
	// partial-coverage notice.
	return st.ReplaceInstances(rows,
		now.AddDate(-2, 0, 0).UnixMilli(), now.AddDate(1, 0, 0).UnixMilli())
}
