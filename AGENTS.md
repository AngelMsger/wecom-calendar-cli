# Agent Guide — wecom-calendar-cli

This project is a member of the `oa-cli` agent-facing CLI family and mirrors its
siblings (`jira-cli`, `confluence-cli`, …). Read the workspace guide
(`../../AGENTS.md`) and the shared standards under `../../docs/` first. Port the
established shape; do not reinvent cross-cutting contracts.

## What it does

Sync a user's WeCom (Enterprise WeChat) calendars over CalDAV into a local
SQLite store, then serve fast agent-friendly queries over that data, plus a
free-form metadata layer for agent-maintained annotations.

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
- Incremental sync keys off the per-calendar `getctag`.

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

Feature-complete against the family Definition of Done: config/auth/doctor,
sync (incremental + idempotent, ctag + etag), recurrence expansion into
`event_instances` with cross-calendar dedup (`internal/expand`), the
agent-owned metadata layer, companion Skill, generated CLI docs, update-notice,
CI (gofmt/vet/unit tests/`scripts/e2e.sh`/docs-drift on Linux + Windows runtime),
and npm distribution. All of it verified against the live WeCom server.
