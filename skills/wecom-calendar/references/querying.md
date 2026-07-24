# Querying the store — calendars and events

All queries read the **local SQLite store**, never the server. Run `sync`
first (see [syncing.md](syncing.md)); if a query prints
`{"_notice":{"stale":…}}` on stderr, the store is behind — re-sync and re-run.

## List calendars

```bash
wecom-calendar-cli calendar list             # calendars from the store
wecom-calendar-cli calendar list --refresh   # live list from the server
```

Each item carries the calendar `id`, display name (`display_name`), and its
stored change-tag. The `id` is what `event list --calendar` and
`sync --calendar` accept. `--refresh` fetches the calendar list from the server
and prints that live view directly; it does **not** write to or reconcile the
local store — run `sync` to update the store.

## List events in a window

`event list` takes an optional date window; both bounds are `YYYY-MM-DD` in the
display timezone. `--since` defaults to 30 days ago and `--until` to 30 days
ahead, and **`--until` is exclusive** (the window is `[since, until)`):

```bash
wecom-calendar-cli event list                                             # default -30d .. +30d
wecom-calendar-cli event list --since 2026-07-01 --until 2026-08-01        # all of July
wecom-calendar-cli event list --since 2026-07-21 --until 2026-07-26 --calendar <id>
```

- An event is returned when its occurrence **overlaps** the window, not only
  when it starts inside it — a meeting that began earlier but is still running
  is included. Recurring events are expanded so each occurrence in the window
  appears as its own item.
- `--calendar <id>` scopes to one calendar; omit it to search all of them
  (cross-calendar duplicates of the same event are de-duplicated into one
  logical occurrence).
- Occurrences are expanded over a bounded window (default 2 years back to 1 year
  ahead). A query beyond that prints a `{"_notice":{"partial_coverage":…}}` on
  stderr; widen it with `wecom-calendar-cli expand --since <date> --until <date>`.
- `--status <csv>` keeps only the listed statuses (case-insensitive), e.g.
  `--status confirmed,tentative` to drop CANCELLED occurrences.
- `--include-meta` attaches each event's custom metadata inline (see
  [metadata.md](metadata.md)) — one batched lookup, handy for "events + their
  task links" in a single call.
- Soft-deleted (tombstoned) events are hidden.

Each item is an expanded occurrence with these fields: `uid`, `occurrence_key`,
`primary_calendar_id`, `source_calendar_ids` (a JSON array of the calendars this
occurrence appears in), `source_count`, `summary`, `start`, `end` (absolute
instants already resolved from the embedded VTIMEZONE), `all_day`, `status`, and
`local_date`. The list view is deliberately lean — `description`, `location`,
`organizer`, and `attendees` are **not** on an occurrence; get them per event
with `event get` (below). The **`uid` is the key you pass to `event get` and the
`meta` commands**.

## Full event detail — `event get`

`event get <uid>` (aliases `view`, `show`) returns one event in full — the
fields `event list` omits:

```bash
wecom-calendar-cli event get <uid>
wecom-calendar-cli event get <uid> --include-meta          # also attach its metadata
wecom-calendar-cli event get <uid> --occurrence <occurrence_key>
```

The single-object result adds `description`, `location`, `organizer`, `rrule`,
`recurring`, and `attendees` — an array where each attendee has `email`, `name`,
`response_status`, and **`is_self`** (true for your own account, so you can tell
who *else* is in the meeting). Pass `--occurrence` with an occurrence's
`occurrence_key` (from `event list`) to apply that date's RECURRENCE-ID
overrides. An unknown uid returns a structured `EVENT_NOT_FOUND` error.

Typical loop: `event list` for the window → take a `uid` → `event get <uid>` for
the people and the agenda. Both hit the local store, so the extra call is cheap.

## Who am I — `whoami`

`whoami` prints the configured account (`server`, `username`, `scheme`,
`configured`). Use it to know which normalized email counts as "me" when reading
`attendees[].is_self`.

## Output shaping

- `--format json` (default) prints the full envelope; `--format table` is a
  compact human view; `--format ndjson` streams items one JSON object per line
  for large windows.
- `--fields a,b.c` projects the output down to just the fields you need — for
  example `--fields uid,summary,start` when you only want a title list. This
  composes with any format.

## Pagination (event list)

All list commands print the family envelope `{items, next, has_more}`. Only
`event list` paginates; `calendar list` and `meta list` return the whole set in
one page (`has_more` is always false). For `event list`:

```bash
wecom-calendar-cli event list --since 2026-01-01 --until 2027-01-01 --limit 100
# -> has_more: true, next: "<cursor>"
wecom-calendar-cli event list --since 2026-01-01 --until 2027-01-01 --limit 100 --cursor "<cursor>"
wecom-calendar-cli event list --since 2026-01-01 --until 2027-01-01 --all
```

- `--limit N` sizes each page (default 200 when omitted).
- The cursor is opaque and **bound to the query** — pass the `next` value back
  verbatim and keep `--since/--until/--calendar` identical across pages, or the
  CLI rejects it with `CURSOR_MISMATCH`. Do not construct a cursor by hand.
- `--all` returns every match in one page (fine for a local store query).
