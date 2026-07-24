package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/config"
	"github.com/angelmsger/wecom-calendar-cli/internal/store"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/spf13/cobra"
)

// defaultPageSize bounds an event list page when neither --limit nor --all is
// given, so a very wide window returns a first page with a continuation cursor
// instead of an unbounded dump.
const defaultPageSize = 200

func newEventCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "event",
		Short: "Query calendar events",
	}
	cmd.AddCommand(newEventListCmd(s), newEventGetCmd(s))
	return cmd
}

func newEventListCmd(s *appState) *cobra.Command {
	var since, until, calendarID, cursor, statusCSV string
	var limit int
	var all, includeMeta bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List events in a time window from the local store",
		Long: "List events overlapping [--since, --until) from the local store. Dates\n" +
			"are YYYY-MM-DD in the display timezone; --since defaults to 30 days ago\n" +
			"and --until to 30 days ahead. Run `sync` first to populate the store; a\n" +
			"staleness notice on stderr flags out-of-date data.\n\n" +
			"Results are paginated: the JSON envelope carries `has_more` and a `next`\n" +
			"cursor. Pass `--cursor <next>` to fetch the following page, or `--all` to\n" +
			"return every match in one page (fine here since the query is local).",
		Example: "  wecom-calendar-cli event list --since 2026-07-01 --until 2026-07-31\n" +
			"  wecom-calendar-cli event list --calendar 1688853806313356 --format table\n" +
			"  wecom-calendar-cli event list --since 2026-01-01 --until 2026-12-31 --all",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			loc := displayLoc()
			sinceT, untilT, err := parseWindow(since, until, loc)
			if err != nil {
				return err
			}
			sinceMs, untilMs := sinceT.UTC().UnixMilli(), untilT.UTC().UnixMilli()
			// The cursor is bound to the query filters via a digest, so reusing a
			// cursor from a different window/calendar is rejected rather than
			// silently skipping data.
			digest := filterDigest(sinceMs, untilMs, calendarID)
			var after *store.InstanceCursor
			if cursor != "" {
				if after, err = decodeCursor(cursor, digest); err != nil {
					return err
				}
			}
			// --all disables paging; otherwise --limit sets the page size, or a
			// sensible default caps the first page.
			pageLimit := defaultPageSize
			if limit > 0 {
				pageLimit = limit
			}
			if all {
				pageLimit = 0
			}

			st, err := s.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			s.staleNotice(st)
			s.coverageNotice(st, sinceT, untilT)

			// Query the expanded instances so recurring events appear on every
			// occurrence in the window (deduped across calendars).
			rows, nextCur, err := st.QueryInstances(sinceMs, untilMs, calendarID, splitCSV(statusCSV), after, pageLimit)
			if err != nil {
				return err
			}
			if includeMeta {
				if err := attachMeta(st, rows); err != nil {
					return err
				}
			}
			next := ""
			if nextCur != nil {
				next = encodeCursor(*nextCur, digest)
			}
			return s.emitList(rows, pageInfo{Next: next, HasMore: nextCur != nil})
		},
	}
	f := cmd.Flags()
	f.StringVar(&since, "since", "", "start date YYYY-MM-DD (default 30 days ago)")
	f.StringVar(&until, "until", "", "end date YYYY-MM-DD, exclusive (default 30 days ahead)")
	f.StringVar(&calendarID, "calendar", "", "restrict to one calendar id")
	f.StringVar(&statusCSV, "status", "", "keep only these statuses, comma-separated (e.g. confirmed,tentative)")
	f.BoolVar(&includeMeta, "include-meta", false, "attach each event's custom metadata")
	f.IntVar(&limit, "limit", 0, "page size (0 = default page size unless --all)")
	f.StringVar(&cursor, "cursor", "", "continue from a previous page's `next` cursor")
	f.BoolVar(&all, "all", false, "return every match in one page (no pagination)")
	return cmd
}

// splitCSV splits a comma-separated flag into trimmed, non-empty tokens.
func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// attachMeta fills each instance's Metadata from a single batched lookup.
func attachMeta(st *store.Store, rows []store.InstanceOut) error {
	if len(rows) == 0 {
		return nil
	}
	uids := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, r := range rows {
		if !seen[r.UID] {
			seen[r.UID] = true
			uids = append(uids, r.UID)
		}
	}
	byUID, err := st.MetaForUIDs(uids)
	if err != nil {
		return err
	}
	for i := range rows {
		rows[i].Metadata = byUID[rows[i].UID]
	}
	return nil
}

func newEventGetCmd(s *appState) *cobra.Command {
	var includeMeta bool
	var occurrence string
	cmd := &cobra.Command{
		Use:     "get <uid>",
		Aliases: []string{"view", "show"},
		Short:   "Show one event in full, including description, location, organizer and attendees",
		Long: "Return the full record for one event by uid — the fields `event list`\n" +
			"omits: description, location, organizer, and attendees (each flagged\n" +
			"`is_self` for the configured account, so you can tell who else is in the\n" +
			"meeting). Find the uid with `event list`. For a specific occurrence of a\n" +
			"recurring event, pass --occurrence with its `occurrence_key` to apply that\n" +
			"date's overrides.",
		Example: "  wecom-calendar-cli event get <uid>\n" +
			"  wecom-calendar-cli event get <uid> --include-meta\n" +
			"  wecom-calendar-cli event get <uid> --occurrence 1781168400000",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			uid := args[0]
			st, err := s.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			s.staleNotice(st)

			detail, err := st.EventDetail(uid, occurrence)
			if err != nil {
				return err
			}
			if detail == nil {
				return cerrors.Newf(cerrors.CategoryNotFound, "EVENT_NOT_FOUND",
					"no live event with uid %q in the local store", uid).
					WithHint("List events with `wecom-calendar-cli event list --since <date> --until <date>` to find a uid, or run `sync` if the store is empty.")
			}
			markSelf(detail.Attendees, s.cfg().Auth.Username)
			if includeMeta {
				rows, err := st.MetaList(uid, "", "", "")
				if err != nil {
					return err
				}
				detail.Metadata = rows
			}
			return s.emit(detail)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&includeMeta, "include-meta", false, "attach the event's custom metadata")
	f.StringVar(&occurrence, "occurrence", "", "apply a specific occurrence's overrides (its occurrence_key)")
	return cmd
}

// markSelf flags the attendee that matches the configured account, so an agent
// can subtract "me" when reasoning about who else attended.
func markSelf(attendees []store.AttendeeOut, selfUsername string) {
	self := config.NormalizeUsername(selfUsername)
	if self == "" {
		return
	}
	for i := range attendees {
		if config.NormalizeUsername(attendees[i].Email) == self {
			attendees[i].IsSelf = true
		}
	}
}

// filterDigest binds a cursor to the query that produced it (window + calendar),
// so a cursor cannot be replayed against a different filter set — which, with a
// raw offset, would silently skip or duplicate rows.
func filterDigest(sinceMs, untilMs int64, calID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s", sinceMs, untilMs, calID)))
	return hex.EncodeToString(sum[:8])
}

// encodeCursor/decodeCursor carry a keyset position (start_at_ms, uid,
// occurrence_key) plus the filter digest, opaque and URL-safe. Keyset paging is
// stable across inserts/deletes between calls, unlike an offset.
func encodeCursor(c store.InstanceCursor, digest string) string {
	payload := strings.Join([]string{strconv.FormatInt(c.StartMs, 10), c.UID, c.Key, digest}, "\x1f")
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeCursor(s, wantDigest string) (*store.InstanceCursor, error) {
	bad := func() error {
		return cerrors.Newf(cerrors.CategoryUsage, "BAD_CURSOR", "invalid --cursor value %q", s).
			WithHint("Pass the `next` value from a previous page verbatim, or omit --cursor to start over.")
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, bad()
	}
	parts := strings.Split(string(raw), "\x1f")
	if len(parts) != 4 {
		return nil, bad()
	}
	ms, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, bad()
	}
	if parts[3] != wantDigest {
		return nil, cerrors.New(cerrors.CategoryUsage, "CURSOR_MISMATCH",
			"this --cursor was issued for a different query window or calendar").
			WithHint("Restart paging without --cursor, keeping --since/--until/--calendar identical across pages.")
	}
	return &store.InstanceCursor{StartMs: ms, UID: parts[1], Key: parts[2]}, nil
}

// parseWindow resolves the --since/--until flags to a time range, applying
// defaults of now-30d .. now+30d. The CLI absorbs the date math so agents never
// hand-compute timestamps.
func parseWindow(since, until string, loc *time.Location) (time.Time, time.Time, error) {
	now := time.Now().In(loc)
	start := now.AddDate(0, 0, -30)
	end := now.AddDate(0, 0, 30)
	if since != "" {
		t, err := time.ParseInLocation("2006-01-02", since, loc)
		if err != nil {
			return time.Time{}, time.Time{}, badDate("since", since)
		}
		start = t
	}
	if until != "" {
		t, err := time.ParseInLocation("2006-01-02", until, loc)
		if err != nil {
			return time.Time{}, time.Time{}, badDate("until", until)
		}
		end = t
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, cerrors.New(cerrors.CategoryUsage, "BAD_WINDOW",
			"--until must be after --since")
	}
	return start, end, nil
}

func badDate(flag, val string) error {
	return cerrors.Newf(cerrors.CategoryUsage, "BAD_DATE",
		"invalid --%s date %q, expected YYYY-MM-DD", flag, val)
}

// displayLoc is the timezone used to render event times. Fixed to Asia/Shanghai
// for now; a config field will drive it later.
func displayLoc() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}
