# Changelog

All notable changes to `wecom-calendar-cli` are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-07-27

### Fixed

- **`sync --dry-run` no longer reports a synced calendar as "never synced".**
  This server returns an empty `getctag` for one collection. An empty stored tag
  and an empty server tag are indistinguishable from a calendar that has never
  been synced, so the preview mislabelled it — while diagnosing a slow sync,
  that sends you looking for the wrong cause. The preview now says "server sends
  no change-tag, so this calendar is re-listed every sync", which is what is
  actually happening.
- **A widened expansion window survives the next `sync`.** `sync` always
  rebuilt occurrences over the rolling default window (2 years back, 1 year
  ahead), wiping whatever `expand --since/--until` had established. Since the
  coverage notice tells you to widen with `expand` and the companion Skill tells
  agents to re-`sync` whenever a read is stale, the two instructions undid each
  other on every cycle. An explicit `expand --since/--until` now **pins** the
  window and every later `sync` reuses it; `expand` with no flags clears the pin
  and returns to the default. Both commands report `covered_from`,
  `covered_to` and `window_pinned`.
- **The per-event occurrence cap is no longer silent.** Expansion stops at 2000
  occurrences for a single rule (so an unbounded `FREQ=DAILY` cannot explode the
  table), but it did so without a word — a series simply stopped part-way
  through the requested window, indistinguishable from a series that genuinely
  ended. `sync` and `expand` now report `truncated_events` / `truncated_uids` on
  stdout and an `{"_notice":{"expansion_truncated":…}}` line on stderr.
- **An unparseable `EXDATE` no longer resurrects a cancelled occurrence.** Every
  other date field failed the resource on a parse error, but `EXDATE` values
  were skipped silently — dropping an exclusion, which puts a cancelled meeting
  back on the calendar as a live one. It is now parsed as strictly as `DTSTART`.
- **A recurrence rule that will not build is an error, not a silent collapse.**
  `expand` degraded such an event to a single occurrence, quietly losing the
  whole series; it now fails the rebuild (atomically, leaving prior instances
  intact) and names the offending event.
- **A large calendar no longer risks exceeding SQLite's parameter limit.**
  Pruning stale resource-failure records bound one host parameter per event in
  the calendar; the stale set is now computed in Go and deleted in batches.
- **`event list --calendar` matches ids literally.** The calendar filter used an
  unescaped `LIKE`, so `%` or `_` in an id acted as a wildcard.
- **A store read failure during `meta set` is reported as itself** rather than
  surfacing as a misleading "no live event with this uid" warning.

- **Incremental `sync` is incremental again.** A single resource the server
  permanently 404s (or that won't parse) used to withhold its whole calendar's
  change-tag, so busy calendars re-scanned and re-fetched *everything* every
  time (observed: a 763s "incremental" sync re-fetching 3025 events). Such
  resources are now recorded as known-bad by their `getetag` — the calendar's
  change-tag commits, so an unchanged calendar is skipped entirely on the next
  sync, and a re-scan no longer re-attempts them (`--full` still does).
  Additionally, the resource skip now compares the CalDAV `getetag` (what the
  next listing returns) instead of the GET `ETag` header (which differs on this
  server and defeated the skip). Note: the **first** sync after upgrading still
  does a one-time full re-fetch (to store getetags and record broken
  resources); subsequent syncs are fast.

### Added

- **Initial feature set: a local, queryable, annotatable mirror of a user's
  WeCom (Enterprise WeChat) calendars.** Unlike the sibling CLIs — stateless
  wrappers over a remote API — this one maintains a local SQLite store so
  history is queryable offline and annotatable.
- **`sync`** — reconcile CalDAV state into the local store. Incremental by
  default (per-calendar CalDAV `getctag` change-tags; unchanged calendars are
  skipped, only moved etags are re-fetched), idempotent, with `--full` to
  reconcile from scratch, `--calendar <id>` to scope one calendar, and
  `--dry-run` to preview. Events that vanish from the server are **soft-deleted**
  (tombstoned), not removed, so history and attached metadata stay resolvable.
- **`calendar list`** (`--refresh` re-lists from the server) and **`event list
  --since --until`** (`--calendar`, `--limit`, keyset `--cursor`/`--all`,
  `--status` to filter by status, `--include-meta` to attach annotations inline)
  — queries served from the store, with recurring events expanded per occurrence
  and cross-calendar duplicates de-duplicated.
- **`event get <uid>`** (aliases `view`/`show`) — the full record for one event,
  surfacing the fields the list view omits: `description`, `location`,
  `organizer`, `rrule`, and `attendees` (each flagged `is_self` for the
  configured account). `--occurrence <occurrence_key>` applies a recurring
  event's per-date RECURRENCE-ID overrides; `--include-meta` attaches its
  annotations. Closes the loop from `event list` (find a uid) to full detail.
- **`sync --progress auto|none|json`** — bounded liveness on stderr so a long
  first/`--full` sync never looks hung: a single self-updating line on a
  terminal, or structured `{"_notice":{"progress":…}}` notices for agents/pipes
  (one at start, one per scanned calendar, plus a timed heartbeat inside a large
  calendar). stdout stays byte-stable; distinct from the `--verbose`
  per-request log.
- **`whoami`** — the configured account (normalized email), so an agent can
  subtract "me" from an event's attendees.
- **`meta list --value <v>`** — reverse lookup: which events carry a given value
  (e.g. every event linked to a task id).
- **Agent-owned metadata layer** — `meta set` / `meta get` / `meta list` /
  `meta delete`, keyed by `(event uid, namespace, key)` with free-form (incl.
  JSON) values and an optional `--source`. Schema-agnostic: classification and
  external task links (e.g. a Feishu project item) are just conventional
  namespaces/keys. **`sync` and recurrence rebuild never write or delete this
  layer**, so annotations survive every re-sync.
- **WeCom / Tencent CalDAV backend handling** — the non-standard backend is
  handled internally: the calendar-home is the bare `/calendar/` collection
  (Basic-auth identity selects whose calendars), event bodies are fetched with a
  plain `GET` per `.ics` href (inline `calendar-data` and multiget do not work),
  and non-IANA TZIDs (e.g. `TZ08`) are resolved from each event's embedded
  `VTIMEZONE`.
- **Family safety contract** — the two writes (`meta set`, `meta delete`) accept
  `--dry-run` and honor a session read-only posture (`defaults.read_only` /
  `WECOM_CALENDAR_CLI_READ_ONLY=1`, overridable with `--allow-writes`). `sync`
  is a server read + synced-facts write only; it is not blocked by read-only
  mode and never touches metadata.
- **Meta commands shared with the CLI family** — `config` (multi-context
  wizard), `auth` (HTTP Basic: WeCom email + app-specific CalDAV password),
  `doctor`, `skill` (embedded companion `wecom-calendar` Skill for Claude Code /
  Codex), `completion`, `version`; structured JSON errors with stable exit
  codes; `{items, next, has_more}` list envelope; `--fields` projection; a
  `_notice.stale` on stderr when a read runs against an out-of-date store; the
  `WECOM_CALENDAR_CLI_SKILL=1` agent handshake; and an update notifier.

### Documentation

- **Recorded what an incremental sync actually costs**, measured on a real
  account (11 calendars, 3025 resources): the one-time migration run after
  upgrading took 699s and fetched all 3025 resources; every run after it fetched
  0 and took about 3s. `syncing.md` now says plainly that `resources_fetched` —
  not wall-clock time, and not `calendars_scanned` — is the number that tells
  you whether a sync did real work, and explains why a calendar with no
  server-issued change-tag is re-listed every time (a `REPORT`, never a
  re-fetch).

### Notes

- Recurrence expansion into `event_instances` with cross-calendar dedup
  (`internal/expand`) is implemented (RRULE + EXDATE + RECURRENCE-ID overrides),
  and rebuilds atomically.
- Test coverage: unit tests across `ical`, `store`, `sync` (fake CalDAV client),
  `expand`, `transport`, and `caldav`, plus `scripts/e2e.sh` offline-contract
  checks (read-only, `--dry-run`, confirmation gate, cursor, exit codes) run in
  CI on Linux. Live end-to-end behavior against the real WeCom server is
  verified manually.

[Unreleased]: https://github.com/AngelMsger/wecom-calendar-cli/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/AngelMsger/wecom-calendar-cli/releases/tag/v0.1.0
