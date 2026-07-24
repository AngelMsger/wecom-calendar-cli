# Changelog

All notable changes to `wecom-calendar-cli` are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

### Notes

- Recurrence expansion into `event_instances` with cross-calendar dedup
  (`internal/expand`) is implemented (RRULE + EXDATE + RECURRENCE-ID overrides),
  and rebuilds atomically.
- Test coverage: unit tests across `ical`, `store`, `sync` (fake CalDAV client),
  `expand`, `transport`, and `caldav`, plus `scripts/e2e.sh` offline-contract
  checks (read-only, `--dry-run`, confirmation gate, cursor, exit codes) run in
  CI on Linux. Live end-to-end behavior against the real WeCom server is
  verified manually.

[Unreleased]: https://github.com/AngelMsger/wecom-calendar-cli/commits/main
