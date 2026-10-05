# Agent Guide — wecom-calendar-cli

This project is a member of the `oa-cli` agent-facing CLI family and mirrors its
siblings (`jira-cli`, `confluence-cli`, …). Read the workspace guide
(`../../AGENTS.md`) and the shared standards under `../../docs/` first. Port the
established shape; do not reinvent cross-cutting contracts.

## What it does

Sync a user's WeCom (Enterprise WeChat) calendars over CalDAV into a local
SQLite store, then serve fast agent-friendly queries over that data, plus a
free-form metadata layer for agent-maintained annotations.

## Relationship to the official wecom-cli

Tencent's official [`wecom-cli`](https://github.com/WecomTeam/wecom-cli)
(`@wecom/cli`) also covers calendars. The two tools are complements, and that
division shapes what belongs here:

- **This CLI owns reading at depth.** CalDAV has no date-range limit, whereas
  the official CLI lists schedules only in a short window around today. Full
  history, offline queries, recurrence expansion and the agent-owned metadata
  layer are the reason this project exists; protect them first.
- **The official CLI owns acting on the calendar.** Creating, updating and
  cancelling events, attendee management and free/busy lookup are done there.
  Do not add calendar writes here: this CLI stays read-only towards WeCom, and
  its only writes are local metadata.
- **Keep the comparison current.** The figures live in three user-facing
  places: the README (its introduction and the section "Alongside the official
  wecom-cli"), the landing page (`docs/index.html`), and the companion Skill.
  Each carries the same "Checked against wecom-cli <version> on <date>" line.
  Re-verify and update all three together when the official limits change;
  other documents link to the README instead of repeating the figures.

The project was archived from 2026-08-19 to 2026-10-05 on the assumption that
the official CLI replaced it, and reinstated once daily use exposed the history
limit. Overlap with the official CLI is therefore not, by itself, a reason to
stop maintaining this one.

## Documented domain divergence — a local store

The sibling CLIs are stateless request/response wrappers over a remote API. This
one is **stateful**: it maintains a local SQLite store. That is the product
requirement (incremental, idempotent history you can query offline and annotate),
and it is the single intentional departure from the family shape.

Consequences and rules:

- Standard family layers still apply: `pkg/{constants,errors,transport,caldav}`
  and `internal/{app,output,config,auth,update}` mirror jira-cli. `pkg/caldav`
  is this project's `apiclient` analog.
- The extra layer lives in `internal/{store,sync,ical,expand,meta}`. `sync` is
  the only writer to raw-fact/derived tables; `expand` (recurrence) is a pure
  rebuild from stored data.
- **`event_metadata` is an agent-owned layer that `sync`/`expand` must never
  write or delete.** Re-syncing, soft-deleting, or rebuilding instances must
  leave every metadata row intact (covered by a store test). Classification and
  external-task links are just conventional namespaces/keys in this layer; the
  schema hard-codes no specific tool.
- The database lives next to `config.yaml` at `<config_dir>/calendar.db` and
  moves with `--config`. It may contain personal calendar data — never commit
  it (`.gitignore` covers `*.db*`).

## The WeCom / Tencent CalDAV backend (non-standard — read before touching pkg/caldav)

Empirically verified; generic CalDAV libraries fail here:

- Calendar-home is the **bare `/calendar/`** collection (Basic-auth identity
  selects whose calendars). Root `/`, `.well-known`, `/principals/` all 403/404.
- `PROPFIND /calendar/ Depth:1` lists calendars at `/calendar/<id>/` with a
  calendarserver `getctag`.
- `REPORT calendar-query` + time-range lists event `.ics` hrefs + etags.
  Inline `calendar-data` comes back empty; `calendar-multiget` returns 403.
- Each event body is fetched with a plain `GET` on its `.ics` href.
- TZIDs are non-IANA (e.g. `TZ08`) but every event embeds a VTIMEZONE defining
  them — `internal/ical` resolves the embedded offset itself because go-ical's
  `DateTime` hard-fails on such TZIDs.
- Incremental sync keys off the per-calendar `getctag`; within a scanned
  calendar it skips resources whose `getetag` (the one from `calendar-query`,
  **not** the GET `ETag` header — they differ here) is unchanged.
- The server lists resources it then 404s, and some bodies won't parse. These
  are recorded in `calendar_resource_failures` by `getetag` and skipped, so a
  permanently-broken resource does **not** withhold its calendar's `getctag` and
  force a perpetual full re-scan. `--full` re-attempts them.

## Commands

`sync` · `calendar list` · `event list` · `meta set|get|list|delete` ·
`config` · `auth` · `doctor` · `version` · `completion`.

Contracts (family baseline): stdout is machine-readable data, notices/errors go
to stderr; lists use the `{items,next,has_more}` envelope; `--format
json|table|ndjson`; errors are structured with `category`/`code`/`hint`. Writes
(`meta set/delete`) honor read-only posture and `--allow-writes`. Read commands
emit a `_notice.stale` on stderr when the store is out of date.

## Build & test

```bash
make build        # -> bin/wecom-calendar-cli
make lint         # gofmt + go vet
make test         # go test ./...
make cross        # cross-compile dist/ for all platforms
```

Go 1.25 (`go.mod`), `CGO_ENABLED=0` (pure-Go SQLite via modernc.org/sqlite).
Version is injected via ldflags into `pkg/constants`.

## Status

Actively maintained.

Implemented and verified against the live WeCom server: config/auth/doctor,
sync (incremental + idempotent, ctag + etag), recurrence expansion into
`event_instances` with cross-calendar dedup (`internal/expand`), the
agent-owned metadata layer, companion Skill, generated CLI docs, update-notice,
CI (gofmt/vet/unit tests/`scripts/e2e.sh`/docs-drift on Linux + Windows
runtime), and npm distribution.

**Alignment backlog.** The family adopted several shared contracts while this
project was archived, and none of them has been checked or ported here yet: the
event and history filter contract, the Skill collaboration and write-recovery
rules, the CLI and Skill upgrade loop, team service presets with personal
login, and NDJSON continuation metadata. Treat each as applicable and not yet
ported until it is ported or recorded as an intentional difference, then remove
it from this list. The rule on replying to human-authored comments, adopted in
the same period, has nothing to apply to: this CLI has no comment surface.
