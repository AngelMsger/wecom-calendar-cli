# Changelog

All notable changes to `wecom-calendar-cli` are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`auth reuse`.** Gives the selected context the WeCom email of another
  context on the same server whose stored login still verifies, so a team
  preset beside a personal context needs no CalDAV password. Stored passwords
  are keyed by server host and scheme, so contexts on one server already share
  one: the command copies no secret and records only the email. It matches the
  complete normalized server URL and the scheme before any credential access,
  verifies with the calendar-home request `auth login` and `doctor` send,
  leaves a context that already has an email unchanged, and never reads
  credentials from the environment, switches the scheme or activates a
  context. `--dry-run` verifies and previews; `--from-context <name>` chooses a
  source. The result has `context`, `state` (`available`, `reused`,
  `unchanged`, `unavailable`), `changed`, `verified`, `dry_run`, and
  `source_context` or `reason`. `unchanged` and `unavailable` are normal
  results and not evidence of authentication.
- **Reuse errors.** Several verified identities return `AUTH_REUSE_AMBIGUOUS`
  (exit 11) with the candidate contexts in `details`. `--from-context` naming
  an unknown context or one on another server returns
  `AUTH_REUSE_SOURCE_NOT_FOUND` (exit 6) or `AUTH_REUSE_SOURCE_MISMATCH`; a
  missing destination or a service override returns
  `AUTH_REUSE_TARGET_MISSING` or `AUTH_REUSE_TARGET_MISMATCH`. An edit to the
  config file or a replaced password during verification stops the write with
  `AUTH_REUSE_CONFIG_CHANGED` or `AUTH_REUSE_CREDENTIAL_CHANGED`, and a failed
  write is `AUTH_REUSE_WRITE_FAILED`. Each names a command that exists and says
  that no CalDAV password should be issued. Network, permission and
  credential-store failures keep their own errors.

### Changed

- **Reuse is offered before a new password.** A new CalDAV password
  invalidates the previous one, so when the selected context has no WeCom
  email and another stored context on the same server has one, the CLI now
  points at `auth reuse --dry-run` first: in the `next_steps` of
  `config set-context` and `auth guide`, at the top of the guide's
  `instructions`, as the first step of `AUTH_NO_BASIC` with a hint that
  explains it, and ahead of the login steps of
  `CREDENTIAL_NOT_VISIBLE_OR_MISSING` and of an unhealthy `doctor`. The check
  reads only the config file. `CREDENTIAL_STORE_INACCESSIBLE` is unchanged and
  never suggests it, and nothing changes for a context without such a
  neighbour.
- **An equivalent server URL override keeps its stored credential.** A
  `--base-url` or `WECOM_CALENDAR_SERVER` that only re-spells the selected
  context's URL, such as the host in another case or an explicit default port,
  now resolves the credential stored under the context's own spelling, and
  `auth logout` removes that entry. Previously it looked up a different key and
  reported the credential as missing. An override naming a different complete
  URL cannot use that entry (`CREDENTIAL_SERVICE_MISMATCH`).
- **Companion Skill covers login reuse.** The Skill routes a context that
  lacks only its WeCom email to `auth reuse`, ahead of acquiring a password,
  and documents the result fields, states and errors in `team-setup.md`. Skill
  bumped to `0.5.0`.

## [0.3.0] - 2026-10-05

### Added

- **CLI and Skill upgrade loop.** Update notices now carry ordered
  `next_steps`: upgrade the CLI, run `skill install`, reload the agent context.
  `skill status` compares the loaded, installed and embedded Skill versions and
  returns `loaded_version`, `loaded_status` and `next_steps` (`next` is now the
  first of those steps); `skill path` and `skill install` report each copy's
  `version` and `alignment`. `doctor` adds an informational `companion-skill`
  check that does not change its `healthy` verdict. The npm setup text treats
  refreshing the Skill as an explicit step after every install or upgrade.
- **Write outcomes for metadata writes.** When `meta set` or `meta delete` has
  committed but its result cannot be printed — an unknown `--format`, for
  example — the error is now `WRITE_SUCCEEDED_OUTPUT_FAILED`. It names the
  `(uid, namespace, key)` entry and the status it reached, keeps the output
  failure's category, exit code and cause, sets `retryable: false`, and gives a
  read-only `meta get` recovery command. Previously the command exited with a
  bare usage error although the store had changed.
- **The family time-window flags on `event list`.** `--from <instant>` is an
  inclusive lower bound, `--to <instant>` an exclusive upper bound, and
  `--since <duration>` (`24h`, `7d`) a look-back ending now. `--since` cannot
  be combined with `--from`/`--to`, and `--to` requires `--from`. An instant is
  a date, an RFC 3339 timestamp with an offset, or `now`, `now-7d`, `now+14d`.
  Because a calendar looks ahead, a bound may lie in the future, `--from` alone
  ends 30 days ahead instead of now, and a date means midnight in the display
  timezone (Asia/Shanghai) rather than UTC.
- **`expand --from/--to`.** The expansion window is pinned with the same two
  flags. Either may be given alone; a look-back `--since <duration>` is
  rejected, because a window ending now would drop every future occurrence.
- **NDJSON continuation metadata.** With `--format ndjson`, a page that has
  more now prints one compact line on stderr after its rows:
  `{"_notice":{"pagination":{"next":…,"has_more":true},"next_steps":[…]}}`.
  stdout still holds item records only, `--fields` does not remove the notice,
  and complete or unpaginated results print none. Previously an NDJSON page
  gave no sign that more rows existed.
- **`CURSOR_RELATIVE_WINDOW`.** `--cursor` with a window relative to the
  current time (`--since <duration>`, or a bound written as `now±duration`) is
  rejected with this usage error, whose `next_steps` restate the window as
  absolute `--from`/`--to`. Such a window resolves to different bounds on every
  call, so its cursor could only ever fail with `CURSOR_MISMATCH`. `--all`
  reads a relative window in one call.
- **`pkg/timeutil`.** The window parser is importable, as in `prometheus-cli`.
- **Team service presets.** `config set-context <name>` writes a named
  context's service settings — the CalDAV server URL, the auth scheme and an
  optional credential page — from flags, the environment or `.env`, without
  credentials or network access. It ignores the WeCom email and the CalDAV
  password, preserves other contexts, usernames and the shared defaults, and
  does not rewrite the file for identical input. Changing a non-empty field
  needs `--overwrite`; without it `CONFIG_CONTEXT_CONFLICT` (exit 11) lists
  each field's `before` and `after` in `details`. `--dry-run` previews with the
  same merge. The first context becomes current, an existing one only with
  `--activate`. `--base-url` is optional, because the server defaults to the
  public WeCom endpoint.
- **`--auth-scheme` and `--credential-url`**, with `WECOM_CALENDAR_AUTH_SCHEME`,
  `WECOM_CALENDAR_CREDENTIAL_URL` and the `auth.credential_url` config key.
  `basic` is the only scheme WeCom CalDAV accepts; any other value is
  `AUTH_BAD_SCHEME`. The credential page is display-only — the CLI never
  requests it — and `config show` reports it.
- **`auth guide`.** An offline description of where the CalDAV password comes
  from: `server`, `scheme`, `credential_url`, `source`, `instructions`,
  `documentation_url` and `next_steps`. WeCom issues the password in its mobile
  app and has no web page for it, so `instructions` holds the navigation steps
  and `credential_url` stays empty unless a team configured a page of its own.
  The instructions also say that a new password invalidates the previous one,
  and when not to issue one. `auth login`, `config init` and missing-credential
  errors use the same guide.
- **Error `details`.** A structured error may carry an optional `details`
  object with non-secret context.

### Changed

- **The Skill handshake is versioned.** `WECOM_CALENDAR_CLI_SKILL` now carries
  the loaded Skill's version. The stderr Skill notice reports `status`
  (`not_loaded`, `outdated`, `unknown` or `current`), `loaded_version`,
  `embedded_version` and `next_steps`, and stays silent only when the versions
  match. The legacy value `1` is reported as `unknown` until the agent reloads
  the refreshed Skill and exports its version.
- **Companion Skill collaboration rules.** The Skill now tells agents to reuse
  identifiers and windows they already hold, continue within the authorization
  they have, treat `--dry-run` as a check rather than a second approval, use
  `--allow-writes` only for a write the task authorizes, read bounded windows
  and pages, avoid polling without a deadline, read before overwriting an
  annotation (`meta set` replaces the whole value), and report the result with
  its evidence and limits. The rules live in one reference,
  `working-with-the-user.md`, linked from the entry points. Skill bumped to
  `0.2.0`.
- **Window errors use `BAD_TIME_RANGE`.** An unreadable or contradictory
  window on `event list` or `expand` now returns the family's `BAD_TIME_RANGE`
  usage error with a hint and runnable examples. It replaces `BAD_DATE` and
  `BAD_WINDOW`; the category and exit code (usage, 2) are unchanged.
- **The default `event list` window is aligned to the local day.** It still
  covers 30 days either side of now, but now runs from midnight 30 days ago to
  the end of the day 30 days ahead in the display timezone, so it is a superset
  of the previous window and identical for every call made on one day.
- **Companion Skill follows the new flags.** Every example uses `--from`/`--to`,
  and the Skill documents `--since <duration>`, look-ahead windows, the local
  meaning of a date, paging with fixed bounds and the NDJSON pagination notice.
  Skill bumped to `0.3.0`.
- **`auth login` verifies before it stores, and stores the identity.** It
  checks the password with an authenticated request to the calendar home, then
  saves it together with the WeCom email and the scheme, so a later process
  resolves the same credential without another prompt. A service that came only
  from the environment becomes a `default` context. A service URL that differs
  from the selected context's is refused with `CONTEXT_BASE_URL_MISMATCH`
  before anything is stored. `CREDENTIAL_SAVE_FAILED` and
  `LOGIN_CONFIG_WRITE_FAILED` say what was stored and ask for the same password
  again. Previously the password was stored unchecked and the email was left
  out of the config file. The command's output is unchanged.
- **`config init` starts from presets and shows the guide.** The server URL and
  credential page offered for a context come from flags, the environment,
  `.env` and that context, and a configured credential page survives an edit.
  The acquisition guide replaces the one-line password note. In the plain
  wizard, a server URL that is not a plain HTTP(S) URL now stops setup before
  the password prompt, instead of failing validation afterwards.
- **Credential recovery steps.** `CREDENTIAL_NOT_VISIBLE_OR_MISSING` and
  `AUTH_NO_BASIC` now list `auth guide`, after the host retry.
  `CREDENTIAL_STORE_INACCESSIBLE` never does: its steps say not to run
  `auth login` or issue a new CalDAV password, which would invalidate the one
  other calendar clients use. `AUTH_LOGIN_NEEDS_TTY` points at `auth guide`.
- **Companion Skill covers team setup.** A new reference, `team-setup.md`,
  documents presets, the guide, personal login and their failures, and the
  Skill keeps "the credential store is inaccessible" apart from "a credential
  must be acquired". Skill bumped to `0.4.0`.

### Deprecated

- **`--until`** on `event list` and `expand` is a deprecated alias of `--to`.
- **`--since YYYY-MM-DD`** on `event list` and `expand` is a deprecated alias
  of `--from`. On `event list`, `--since` now takes a duration.
- **Deprecation notices.** Both aliases keep working and select the same window
  as their replacements — including `--until` on its own, which keeps its
  default lower bound although `--to` requires `--from`. Each use prints one
  `{"_notice":{"deprecated_flag":{…}}}` line on stderr naming the flag and its
  replacement. Set `WECOM_CALENDAR_CLI_NO_DEPRECATION_NOTICE=1` to silence it.

### Fixed

- **`READONLY_BLOCKED` recovery steps.** A blocked `meta` write listed the
  generic permission step about calendar visibility in the WeCom client. It now
  lists the read-only overrides, as the sibling CLIs do.
- **Skill accuracy.** Corrected examples and claims that did not match the
  binary: the 401 error code is `CALDAV_AUTH`; the `meta set --dry-run` and
  `READONLY_BLOCKED` samples show the real output; a value that parses as JSON
  keeps that type, so numeric text is stored as a number unless passed as a
  quoted JSON string; a missing metadata entry is an empty result rather than a
  `not_found` error; `meta delete` examples include the `--yes` an agent needs;
  `doctor` is described by the checks it runs; and a cursor needs the same
  absolute window on every page.
- **The cursor of a default-window `event list` page.** With no window flag the
  bounds were relative to the current millisecond, so the next call computed a
  different window and the `next` cursor of the first page always failed with
  `CURSOR_MISMATCH`. The default bounds now sit on local midnight, and the
  cursor resumes for the rest of the day.
- **Rate-limit recovery step.** It told callers to narrow `--since`/`--until`,
  flags that `sync` — the only command that reaches the server in bulk — never
  had. It now says to sync one `--calendar` at a time.
- **The config file is replaced atomically**, so a write that fails no longer
  truncates an existing configuration.
- **The plain `config init` wizard at end of input.** A required prompt with no
  default re-prompted forever once its input had ended; it now fails with the
  read error. A prompt with a default still takes it, so scripted setups are
  unaffected. Text prompts on a terminal no longer read ahead of the hidden
  password prompt.
- **`config init` and `config delete-context` no longer delete a credential
  that is still in use.** Editing a context so that only the spelling of its
  server URL changed deleted the password just saved, and deleting a context
  removed the password of every other context on the same server. A stored
  credential is now forgotten only when no remaining context uses it, and
  `delete-context` forgets it after the config file is written.
- **Skill: contexts and the store.** The Skill said each context has its own
  store. Every context in a config directory shares its `calendar.db`, and
  contexts on the same server share one stored password.

## [0.2.3] - 2026-10-05

Version 0.2.2 was prepared on 2026-08-12 but never published; the addition
below was written for it and ships here for the first time.

### Added

- **Broader skill install agent matrix.** `skill install` now treats Cursor,
  the shared Agents tree, Gemini CLI, GitHub Copilot, OpenCode, Continue,
  Windsurf, Kilo Code, and Roo Code as first-class targets alongside Claude
  Code, Codex, Grok Build, and Pi (13 agents total). Auto-detection probes
  each product's home and project markers; `--agent` accepts the full id
  list. Installation guides, generated CLI docs, and help text stay in sync.

### Changed

- **Active maintenance resumed.** The project was archived on 2026-08-19, when
  Tencent's official [`wecom-cli`](https://github.com/WecomTeam/wecom-cli) looked
  like a complete replacement, and reinstated on 2026-10-05. As of version
  1.3.2 the official CLI lists schedules only within 30 days before or after
  today, while this CLI mirrors the whole calendar history over CalDAV and
  queries it offline. The two are documented as complements — `wecom-cli` to
  create and change events, this CLI to read at depth and annotate. No release
  shipped while the project was archived, so existing installs are unaffected.
- **Documentation and the companion Skill explain when to use which tool.** The
  README and landing page compare the two CLIs, and the Skill routes history,
  offline and annotation requests here and calendar changes to `wecom-cli`.


## [0.2.1] - 2026-08-11

### Added

- **Pi skill install target.** `skill install` now deploys the companion Skill
  for Pi (`--agent pi`) to `~/.pi/agent/skills/<name>` globally and
  `./.pi/skills/<name>` with `--project`. Auto-detection probes `~/.pi` /
  `./.pi` alongside Claude Code, Codex, and Grok Build. Installation guides,
  generated CLI docs, and agent help text list the new target.


## [0.2.0] - 2026-08-11

### Added

- **Grok Build skill install target.** `skill install` now deploys the companion
  Skill for Grok Build (`--agent grok`) to `~/.grok/skills/<name>` globally and
  `./.grok/skills/<name>` with `--project`. Auto-detection probes `~/.grok` /
  `./.grok` alongside Claude Code and Codex. Installation guides, generated CLI
  docs, and agent help text list the new target.


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

[Unreleased]: https://github.com/AngelMsger/wecom-calendar-cli/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/AngelMsger/wecom-calendar-cli/compare/v0.2.3...v0.3.0
[0.2.3]: https://github.com/AngelMsger/wecom-calendar-cli/compare/v0.2.1...v0.2.3
[0.2.1]: https://github.com/AngelMsger/wecom-calendar-cli/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/AngelMsger/wecom-calendar-cli/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/AngelMsger/wecom-calendar-cli/releases/tag/v0.1.0
