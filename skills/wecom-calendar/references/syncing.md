# Syncing the local store

`sync` is the only path that pulls data from the WeCom server. It reconciles
the CalDAV state into the local SQLite store, and everything else reads from
that store.

```bash
wecom-calendar-cli sync                       # incremental sync of every calendar
wecom-calendar-cli sync --calendar <id>       # just one calendar
wecom-calendar-cli sync --full                # ignore change-tags, reconcile all
wecom-calendar-cli sync --dry-run             # report what would change, write nothing
wecom-calendar-cli sync --progress none       # silence the progress notices
```

## Progress — sync can take a while, and says so

A first `sync` (or `--full`) pulls the full history one `.ics` at a time and can
run for a while, so it reports **bounded** progress on **stderr** — never a
per-request flood. `--progress` is `auto` by default: a single self-updating
status line on an interactive terminal, or, for an agent/pipe, structured
notices — one at the start (`{"phase":"start","calendars_total":N}`), one per
scanned calendar, and a timed heartbeat inside a large calendar so it never goes
silent. **stdout is untouched** — it still carries only the final JSON summary,
so treat a `{"_notice":{"progress":…}}` line as liveness, not a result. Use
`--progress none` to silence it, or `--progress json` to force notices on a
terminal. (Distinct from `--verbose`, which logs every HTTP request for
debugging — far more output, opt-in only.)

## Incremental by default

Each calendar carries a server-issued **change-tag** (a CalDAV `getctag`).
`sync` compares the stored tag against the server's:

- **Tag unchanged** → the calendar is skipped entirely (no event fetches). This
  is what makes routine syncs cheap.
- **Tag changed** → `sync` lists the calendar's event hrefs + etags in the
  requested time range, fetches only the `.ics` bodies whose etag moved, and
  updates the stored tag.

The sync report tells you what happened per calendar: calendars scanned,
events added / updated / soft-deleted, and calendars skipped as unchanged.

Some WeCom calendars list resources the server then cannot serve (a `GET` 404s)
or whose body will not parse. Such resources are recorded as **known-bad** by
their change-tag and skipped on later syncs, so one broken resource no longer
forces its whole calendar to re-scan forever. `--full` re-attempts them.

> **First sync after upgrading is slow, then fast.** The incremental fix needs
> to store fresh change-tags and record the broken resources, so the *first*
> `sync` after updating re-fetches everything one more time. Every sync after
> that skips unchanged calendars and only fetches what actually moved.
>
> Measured on a real account (11 calendars, 3025 resources): the migration run
> took 699s and fetched all 3025; the next runs fetched 0 and took ~3s. If a
> `sync` still re-fetches everything on the *second* run, that is a real problem
> — check `resources_fetched` in the result, not the wall-clock time.

### Why `calendars_scanned` is rarely zero

A calendar can only be skipped when the server hands out a change-tag to compare
against. This server returns an **empty** `getctag` for at least one collection,
and an empty tag carries no information — so that calendar is re-listed on every
sync. That costs one `REPORT`, not a re-fetch: the per-resource comparison still
skips every unchanged resource, so `resources_fetched` stays 0.

`sync --dry-run` names this case explicitly ("server sends no change-tag"), so
do not read it as "never synced". **`resources_fetched` is the number that tells
you whether a sync did real work**; `calendars_scanned` counts re-listings,
which are cheap.

## Cadence — how often to sync

An unchanged sync is cheap (a change-tag check per calendar, no fetches), so
running it often is fine. Since queries read the local store and never reach the
server, keep the store warm by scheduling `sync` yourself — e.g. a `cron` or
`launchd` job every 10–15 minutes:

```cron
*/15 * * * * /usr/local/bin/wecom-calendar-cli sync --progress none >/dev/null 2>&1
```

The tool does not sync on its own (reads stay offline and instant); a read only
prints a `_notice.stale` hint when the store is behind.

## `--full` — reconcile from scratch

`--full` ignores the change-tags **and** the known-bad records, re-listing and
re-fetching every calendar's events. Use it when you suspect the store drifted
(a partial sync was interrupted, the server's tag semantics missed a change, or
a previously-unparseable resource should be retried after a fix). It is heavier
but produces the same end state — `sync` is idempotent.

## Idempotency

Running `sync` twice with no server-side change is a no-op: the second run sees
identical change-tags and etags and writes nothing. An interrupted sync can be
safely re-run; it resumes reconciliation rather than duplicating rows. Event
identity is the CalDAV UID, so re-fetching an event updates its row in place
instead of inserting a duplicate.

## Soft-delete, not hard-delete

When an event disappears from the server, `sync` marks its stored row
**deleted** (a tombstone) rather than removing it. This keeps history queryable
and — critically — keeps any metadata attached to that UID resolvable. `event
list` hides soft-deleted events by default. A hard purge is out of scope for
routine sync.

## The recurrence rebuild at the end of a sync

Every `sync` finishes by rebuilding the expanded occurrences that `event list`
reads. It uses the window pinned by an earlier `expand --since/--until` if there
is one, and the rolling default (2 years back to 1 year ahead) otherwise — so
widening coverage once survives every later sync instead of reverting. The sync
result reports the window it used as `covered_from` / `covered_to` plus
`window_pinned`, and flags any series cut short by the per-event occurrence
limit as `truncated_events` / `truncated_uids` (with an
`{"_notice":{"expansion_truncated":…}}` line on stderr). See
[querying.md](querying.md).

## Metadata is never touched

`sync` (and the recurrence rebuild it triggers) writes only the raw-fact and
derived tables. The **`event_metadata` layer is agent-owned**: re-syncing,
soft-deleting an event, or rebuilding instances all leave every metadata row
intact. You can annotate an event today, re-sync repeatedly, and the
annotation still resolves by UID. See [metadata.md](metadata.md).

## WeCom / Tencent CalDAV quirks (handled for you)

The WeCom CalDAV backend is non-standard; `sync` handles these so you do not
have to:

- The calendar-home is the bare `/calendar/` collection — Basic-auth identity
  selects whose calendars you see. `sync` does not probe `/`, `.well-known` or
  `/principals/` (they 403/404 here).
- Event bodies are fetched with a plain `GET` per `.ics` href — inline
  `calendar-data` in the range report comes back empty and multiget is
  rejected, so `sync` fetches bodies one at a time.
- Non-IANA TZIDs (e.g. `TZ08`) are resolved from the `VTIMEZONE` each event
  embeds, so start/end times land at the correct absolute instant even though
  the TZID is not a real Olson zone.

## When to sync (agents)

- **Before answering any query** if you have not synced this session, or if the
  data could be stale.
- **Whenever a read prints `{"_notice":{"stale":…}}` on stderr** — the store is
  behind the server. Re-run `sync`, then re-query.
- `--dry-run` first if you only need to know *whether* anything changed without
  writing to the store.
