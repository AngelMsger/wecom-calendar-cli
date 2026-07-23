# Querying the store — calendars and events

All queries read the **local SQLite store**, never the server. Run `sync`
first (see [syncing.md](syncing.md)); if a query prints
`{"_notice":{"stale":…}}` on stderr, the store is behind — re-sync and re-run.

## List calendars

```bash
wecom-calendar-cli calendar list             # calendars from the store
wecom-calendar-cli calendar list --refresh   # re-list from the server first
```

Each item carries the calendar `id`, display name, and its stored change-tag.
The `id` is what `event list --calendar` and `sync --calendar` accept.
`--refresh` reaches the server to reconcile the calendar set (not the events)
before printing — use it when a calendar was just created or renamed.

## List events in a window

`event list` requires an explicit date window; both bounds are inclusive
`YYYY-MM-DD` dates:

```bash
wecom-calendar-cli event list --since 2026-07-01 --until 2026-07-31
wecom-calendar-cli event list --since 2026-07-21 --until 2026-07-25 --calendar <id>
wecom-calendar-cli event list --since 2026-07-01 --until 2026-12-31 --limit 200
```

- `--since` / `--until` bound the window; an event is returned when it overlaps
  it. Recurring events are expanded so each occurrence in the window appears as
  its own item.
- `--calendar <id>` scopes to one calendar; omit it to search all of them
  (cross-calendar duplicates of the same event are de-duplicated).
- `--limit N` sizes each page (see pagination below).
- Soft-deleted (tombstoned) events are hidden by default.

Each item includes at least the event `uid`, `calendar_id`, `summary`, start /
end (absolute instants, already resolved from the embedded VTIMEZONE),
all-day flag, location, and organizer/attendees when present. The **`uid` is
the key you pass to `meta` commands** — see [metadata.md](metadata.md).

## Output shaping

- `--format json` (default) prints the full envelope; `--format table` is a
  compact human view; `--format ndjson` streams items one JSON object per line
  for large windows.
- `--fields a,b.c` projects the output down to just the fields you need — for
  example `--fields uid,summary,start` when you only want a title list. This
  composes with any format.

## Pagination

`calendar list`, `event list` and `meta list` return the family envelope
`{items, next, has_more}`, one page per call:

```bash
wecom-calendar-cli event list --since 2026-01-01 --until 2026-12-31 --limit 100
# has_more: true, next: "<cursor>"
wecom-calendar-cli event list --since 2026-01-01 --until 2026-12-31 --cursor "<cursor>"
wecom-calendar-cli event list --since 2026-01-01 --until 2026-12-31 --all
```

The cursor is opaque — pass the `next` value back verbatim; do not construct
one. `--all` walks every page in a single call (fine for a store query, since
it is local). Prefer a tighter window over `--all` on a year-wide range when
you only need part of it.
