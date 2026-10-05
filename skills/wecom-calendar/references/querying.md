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

`event list` takes an optional window `[from, to)`: **`--from` is inclusive and
`--to` is exclusive**, so adjacent windows never overlap.

```bash
wecom-calendar-cli event list                                       # 30 days either side of today
wecom-calendar-cli event list --from 2026-07-01 --to 2026-08-01     # all of July
wecom-calendar-cli event list --since 7d                            # the last 7 days, ending now
wecom-calendar-cli event list --from now --to now+14d               # the next two weeks
wecom-calendar-cli event list --from 2026-07-21 --to 2026-07-26 --calendar <id>
```

- `--since <duration>` looks back from now (`24h`, `7d`, `2w`). It ends at the
  current instant, so it never returns upcoming events — use `--from`/`--to`
  to look ahead.
- `--from` and `--to` each take a date `YYYY-MM-DD`, an RFC 3339 instant with an
  offset (`2026-07-01T09:00:00+08:00`), or `now`, `now-7d`, `now+14d`. A bound
  may lie in the future. A bare duration (`--from 7d`) is rejected: write
  `now-7d` or `now+7d`.
- **A date means midnight in the display timezone (Asia/Shanghai), not UTC** —
  `--from 2026-07-01` is `2026-07-01T00:00:00+08:00`. An instant with an offset
  is taken exactly.
- `--since` cannot be combined with `--from`/`--to`, and `--to` requires
  `--from`. A broken window is a `BAD_TIME_RANGE` usage error (exit 2) whose
  `next_steps` hold working examples.
- With no window flag the window runs from midnight 30 days ago to the end of
  the day 30 days ahead, so it is the same for every call made on one day.
  With `--from` alone it ends at that same point 30 days ahead, not now.
- There is no `--actor` filter. Use `whoami` and each attendee's `is_self` to
  tell your own participation apart.

`--until`, and `--since` with a date, are the previous spellings of `--to` and
`--from`. They still work and print one
`{"_notice":{"deprecated_flag":{…}}}` line each on stderr naming the
replacement; write the new flags. (`--until` on its own keeps its old default
lower bound; `--to` on its own is an error.)

- An event is returned when its occurrence **overlaps** the window, not only
  when it starts inside it — a meeting that began earlier but is still running
  is included. Recurring events are expanded so each occurrence in the window
  appears as its own item.
- `--calendar <id>` scopes to one calendar; omit it to search all of them
  (cross-calendar duplicates of the same event are de-duplicated into one
  logical occurrence).
- Occurrences are expanded over a bounded window (default 2 years back to 1 year
  ahead). A query beyond that prints a `{"_notice":{"partial_coverage":…}}` on
  stderr; widen it with `wecom-calendar-cli expand --from <date> --to <date>`.
  A window set that way is **pinned**: every later `sync` reuses it, so it does
  not revert on the next refresh. Run `expand` with no flags to forget the pin
  and go back to the rolling default. Both commands report the window they used
  as `covered_from` / `covered_to` and whether it was pinned (`window_pinned`).
  `expand` takes dates or RFC 3339 instants, either bound alone, and has no
  look-back `--since <duration>` — a window ending now would drop every future
  occurrence, so it is rejected with `BAD_TIME_RANGE`.
- A very long window can push a frequently recurring series past the per-event
  occurrence limit. When that happens the run prints
  `{"_notice":{"expansion_truncated":…}}` on stderr and reports
  `truncated_events` / `truncated_uids` on stdout — the later occurrences of
  those series are missing from the window. Narrow the window to get them back.
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
  compact human view whose footer shows the `--cursor` to continue with;
  `--format ndjson` prints the items only on stdout, one JSON object per line,
  and reports a further page on stderr (see below).
- `--fields a,b.c` projects the output down to just the fields you need — for
  example `--fields uid,summary,start` when you only want a title list. This
  composes with any format.

## Pagination (event list)

All list commands print the family envelope `{items, next, has_more}`. Only
`event list` paginates; `calendar list` and `meta list` return the whole set in
one page (`has_more` is always false). For `event list`:

```bash
wecom-calendar-cli event list --from 2026-01-01 --to 2027-01-01 --limit 100
# -> has_more: true, next: "<cursor>"
wecom-calendar-cli event list --from 2026-01-01 --to 2027-01-01 --limit 100 --cursor "<cursor>"
wecom-calendar-cli event list --from 2026-01-01 --to 2027-01-01 --all
```

- `--limit N` sizes each page (default 200 when omitted). It is a page size,
  not a cap on the total.
- The cursor is opaque and **bound to the query** — pass the `next` value back
  verbatim and keep `--from/--to/--calendar` identical across pages, or the
  CLI rejects it with `CURSOR_MISMATCH`. Do not construct a cursor by hand.
- **Page with absolute `--from`/`--to`.** A window relative to the current time
  — `--since <duration>`, or a bound written as `now±duration` — resolves to
  different bounds on every call, so `--cursor` with it is rejected with
  `CURSOR_RELATIVE_WINDOW`. The error's `next_steps` restate the window as
  absolute bounds: start again from the first page with those, or read the
  relative window in one call with `--all`.
- The default window follows the local day, so a cursor from a call without
  window flags works for the rest of that day. Pass absolute bounds anyway when
  paging could run past midnight.
- `--all` returns every match in one page. Use it when the task needs the
  complete window; otherwise read one page and follow `next` only until you
  have what the question needs. Say so when you stopped before `has_more` was
  false.

With `--format ndjson`, stdout holds only the item lines. After a page that has
more, one extra line arrives on **stderr**:

```json
{"_notice":{"next_steps":["Pass next as --cursor to retrieve the next page."],"pagination":{"has_more":true,"next":"<cursor>"}}}
```

Pass `pagination.next` as `--cursor`, with the same window, to continue. A
complete result and the unpaginated list commands print no such line, and
`--fields` does not remove it. Capture stderr alongside stdout whenever you
page NDJSON; the notice never changes the exit code.
