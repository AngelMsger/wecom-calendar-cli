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

- Standard family layers still apply:
  `pkg/{constants,errors,transport,caldav,timeutil}` and
  `internal/{app,output,config,auth,update}` mirror jira-cli. `pkg/caldav` is
  this project's `apiclient` analog; `pkg/timeutil` is the window parser
  `prometheus-cli` carries under the same name.
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

`sync` · `expand` · `calendar list` · `event list|get` ·
`meta set|get|list|delete` · `whoami` · `config` · `auth` · `doctor` ·
`skill status|install|path|show|uninstall` · `version` · `completion`.

Contracts (family baseline): stdout is machine-readable data, notices/errors go
to stderr; lists use the `{items,next,has_more}` envelope; `--format
json|table|ndjson`; errors are structured with `category`/`code`/`hint`. Writes
(`meta set/delete`) honor read-only posture and `--allow-writes`. Read commands
emit a `_notice.stale` on stderr when the store is out of date.

### The CLI and Skill upgrade loop

Ported from `jira-cli` unchanged in shape. `WECOM_CALENDAR_CLI_SKILL` carries
the loaded Skill's version, not a boolean: the runtime notice classifies it as
`not_loaded`, `outdated`, `unknown` (the legacy value `1`) or `current`, and is
silent only for `current`. `skill status` compares the loaded, installed and
embedded versions and returns ordered `next_steps`; `skill path` and
`skill install` report each copy's `alignment`. `doctor` adds an informational
`companion-skill` check that never changes `healthy`. Update notices list three
ordered steps: upgrade the CLI, run `skill install`, reload the agent context.
The npm banner repeats them as a secondary hint only, and `postinstall` never
writes into an agent directory.

The Skill tells agents to export the literal version, so **bump the `version:`
frontmatter and the handshake value in the Skill's "Agent handshake" section
together**. `scripts/e2e.sh` reads the version from the frontmatter and fails
when the handshake sentence names another one.

### Write outcomes

The family rule — reconcile an uncertain write before replaying it, and keep
the resource identity when a read fails after a successful write — was written
for remote writes. `meta set` and `meta delete` are the only writes here, and
their path differs in three ways that decide what applies:

- **Nothing is read back.** The result is built from the caller's own
  `(uid, namespace, key)` and the statement's row count, so there is no
  post-write read to fail and no identity to lose. The one read on the path,
  `EventExists`, runs before the statement and aborts the command when it fails.
- **No outcome is uncertain.** Each write is one autocommit SQLite statement on
  a local file. A statement that returns an error did not apply, and no network
  sits between the CLI and the store, so the "outcome unknown, verify before
  retrying" class does not exist. `pkg/errors` therefore keeps the plain retry
  guidance for `rate_limit`, `server` and `network`: only requests to the WeCom
  server raise them, and every one of those is a read.
- **Printing the result is the step that can fail after the commit.** Output
  options are validated while rendering, as in the siblings, so an unknown
  `--format` used to commit the row and then exit as a bare usage error. That
  is this CLI's analogue of the rule: `emitAfterWrite`
  (`internal/app/write_errors.go`) turns it into
  `WRITE_SUCCEEDED_OUTPUT_FAILED`, which keeps the cause's category and exit
  code, names the entry, sets `retryable: false` and points at `meta get`. A
  delete that matched nothing changed nothing and keeps the plain error.

Keep every check and read ahead of the mutating statement, and route any step
added after it through `emitAfterWrite`. `meta set` is last-write-wins with no
version check, so there is no conflict to merge in code; the Skill carries the
read-merge-write rule instead. `READONLY_BLOCKED` lists the family's three
override steps, and the Skill tells agents not to act on them without
authorization.

### Time windows

`event list` uses the family's time-window vocabulary (shared standard §3.5):
`--since <duration>` is a look-back ending now, `--from` an inclusive lower
bound and `--to` an exclusive upper bound, so the interval is `[from, to)`.
`--since` excludes `--from`/`--to`, `--to` requires `--from`, and an unusable
window is `BAD_TIME_RANGE` (usage) with a hint that restates the rules and
runnable `next_steps`. Parsing lives in `pkg/timeutil`, the lean form of the
package `prometheus-cli` and `openobserve-cli` carry; the flag wiring is
`internal/app/timeflags.go`, after `prometheus-cli`'s file of that name.

A calendar looks ahead, which the siblings' event and history filters never
do. Four differences follow from that. Each is intentional, and each has a test
in `internal/app/timeflags_test.go`:

- **The default window is 30 days either side of today**, not a window ending
  now: from midnight 30 days ago to the end of the day 30 days ahead, in the
  display timezone (`TestDefaultEventWindowIsStableForTheDay`). The bounds sit
  on local midnight so that every call made on one day resolves the same
  window. They used to follow the current millisecond, which made the `next`
  cursor of every default-window page fail with `CURSOR_MISMATCH`
  (`TestEventListDefaultWindowCursorResumes`).
- **A window may reach into the future.** `--to` may be later than now,
  `now+14d` is a valid instant, and `--from` without `--to` ends at the default
  upper bound 30 days ahead instead of at now
  (`TestEventWindowReachesIntoTheFuture`). For the same reason a bare duration
  such as `--from 7d` is rejected instead of being read as "7 days ago", as the
  sibling parsers read it: the direction has to be written, `now-7d` or
  `now+7d` (`TestParseInstantRejectsABareDuration` in `pkg/timeutil`).
- **A date-only value is midnight in the display timezone**, not UTC, because
  calendar days are local. An RFC 3339 instant with an offset is exact
  (`TestEventWindowDateIsALocalDay`).
- **There is no `--actor`.** The store holds one account's calendars; `whoami`
  and each attendee's `is_self` cover identity
  (`TestEventListHasNoActorFlag`). Do not add an actor filter without
  revisiting this.

`pkg/timeutil` also leaves out what only a metrics backend needs: epoch
integers, the space-separated UTC timestamp, the `y` unit and the step and
rendering helpers. Epochs are omitted on purpose — this CLI prints RFC 3339
only, and a compact date such as `20260701` would otherwise be read as epoch
seconds and silently select 1970.

**Paging needs fixed bounds.** A cursor is bound to its resolved window by a
digest. A window relative to the current time — `--since <duration>`, or a
bound written as `now±duration` — resolves to different bounds on every call,
so `--cursor` with it is rejected with `CURSOR_RELATIVE_WINDOW`, and the error
restates the window as absolute `--from`/`--to`. `--all` resolves a relative
window once. This follows `bitbucket-cli`'s activity filter, which resolves a
windowed query once and reads it in a single invocation, and rejects a cursor
it cannot attribute instead of guessing. The default bounds are stable for the
day, so they remain pageable; `CURSOR_MISMATCH` tells a caller who pages past
midnight to pass absolute bounds (`TestEventListCursorNeedsFixedWindow`).

**`expand` is not an event filter.** It pins the window recurring events are
expanded over, so it takes `--from`/`--to` only, lets either bound default on
its own, and refuses a look-back `--since <duration>`: a window ending now
would silently drop every future occurrence from the store
(`TestExpandWindow`, `TestExpandRejectsLookBackSince`).

**Deprecated aliases.** Before this contract the flags were `--since
YYYY-MM-DD` and `--until YYYY-MM-DD`. Both remain, on `event list` and
`expand`, as aliases of `--from` and `--to`, so no existing invocation breaks —
including `--until` on its own, which keeps its default lower bound although
the canonical `--to` requires `--from`. Each use prints one
`{"_notice":{"deprecated_flag":{…}}}` line on stderr with the `flag`, its
`replacement`, a `message` and how to `silence` it
(`WECOM_CALENDAR_CLI_NO_DEPRECATION_NOTICE=1`). The family had no
deprecated-flag notice to reuse, so the shape follows the shared standard's
§4.3 (`TestEventWindowDeprecatedAliases`, `TestDeprecationNoticeShape`,
`TestDeprecatedFlagNoticesOnStderr`). `BAD_DATE` and `BAD_WINDOW`, the codes
the old flags raised, are replaced by `BAD_TIME_RANGE`.

When a command gains a time filter, wire it through `timeFlags` rather than
parsing dates locally, and decide explicitly whether its window can be paged.

### NDJSON continuation

Ported from `bitbucket-cli` unchanged: `internal/output/output.go` is identical
to the sibling's apart from the import path. With `--format ndjson`, stdout
carries item records only; after every row is written, a page with `has_more`
prints one compact stderr line,
`{"_notice":{"pagination":{"next":…,"has_more":true},"next_steps":[…]}}`.
`Options.NoticeWriter` makes the destination injectable and `Options.NextFlag`
names the continuation flag, defaulting to `--cursor`, which the table footer
uses too. Field projection does not drop the notice, an empty page that still
has a token keeps it, and a failed stdout write advertises nothing.

`event list` is the only paginated command and passes its page descriptor to
`emitList`. `calendar list`, `meta get` and `meta list` return the whole set
and pass an empty descriptor on purpose: there is no cursor to preserve, and
none is fabricated. A command that starts paginating must pass its `pageInfo`
through, or the notice is lost before the renderer sees it.

Covered by `internal/output/pagination_test.go` (projection, an empty page
with a token, completed and unpaginated results, write failures, a custom
flag) and by `TestEventListNDJSONContinuation`, which reads the notice and
resumes through the real command. `scripts/e2e.sh` does the same against the
binary. No command creates events offline, so it seeds synthetic occurrences
with `test/seedstore` — `event list` reads only the derived `event_instances`
table — instead of starting a mock server.

### Companion Skill rules

`skills/wecom-calendar/references/working-with-the-user.md` is the single home
for the collaboration rules — reuse verified context, continue within existing
authorization, bounded reads and no polling, preserving annotations, and
evidence-first reporting. `SKILL.md` summarizes them and the topical references
link there instead of restating them. Shell examples and the JSON shown beside
them must match the binary: `scripts/e2e.sh` runs the write and recovery
sequences the Skill describes, so extend it when an example changes.

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
project was archived. One has not been checked or ported here yet: team
service presets with personal login. Treat it as applicable and not yet ported
until it is ported or recorded as an intentional difference, then drop it from
this note. The rule on replying to human-authored comments, adopted in the same
period, has nothing to apply to: this CLI has no comment surface.

**Ported, with intentional differences.** The CLI and Skill upgrade loop, the
Skill collaboration and write-recovery rules, the time-window contract and
NDJSON continuation metadata are in place; the sections above describe them.
They differ from the siblings only where the domain does:

- The time-window contract keeps the family vocabulary and rules, with the
  four calendar differences, the pageability rule, the `expand` exception and
  the deprecated aliases listed under "Time windows". Covered by
  `pkg/timeutil/parse_test.go`, `internal/app/timeflags_test.go` and the
  "time-window contract" block of `scripts/e2e.sh`.
- NDJSON continuation has no difference in the renderer. Only the e2e differs:
  it seeds the store with `test/seedstore` and asserts in the shell script,
  where `bitbucket-cli` drives a mock server from a Python helper.

- `WRITE_SUCCEEDED_READ_FAILED`, `WRITE_SUCCEEDED_RESPONSE_INVALID`, the
  "outcome unknown" wrapper for write failures and the partial-batch error are
  not ported, for the reasons under "Write outcomes".
  `WRITE_SUCCEEDED_OUTPUT_FAILED` takes their place and lives in `internal/app`
  because `pkg/caldav` has no write to wrap. Covered by
  `internal/app/write_errors_test.go` and the "committed writes" block of
  `scripts/e2e.sh`.
- The focused Skill reference is `working-with-the-user.md`, not
  `replying-to-people.md`: the reply protocol has no subject here.
- The e2e suite is offline and has no mock server, so the update-notice check
  builds a version-pinned binary and seeds the release cache instead of
  pointing the release API at a mock.
