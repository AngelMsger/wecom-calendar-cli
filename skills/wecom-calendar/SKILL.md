---
name: wecom-calendar
version: 0.5.0
description: "Sync WeCom (Enterprise WeChat) calendars over CalDAV into a local SQLite store, then query it and maintain an agent-owned metadata layer (event classification, external task links). Use when the user mentions a WeCom / 企业微信 calendar, schedule or 日程 and asks to sync or refresh calendar data, list calendars, see or find events in a date range — including history more than 30 days from today, which the official wecom-cli cannot list — read one event's full detail (description, location, organizer, attendees), or annotate, classify, tag or link an event to a task (e.g. a Feishu project item) and read those annotations. It never changes the calendar; creating, editing or cancelling events belongs to wecom-cli. Queries read the local store, so run `sync` first and re-sync when a read prints `_notice.stale`. `meta set` / `meta delete` are the only writes; they honor read-only mode (WECOM_CALENDAR_CLI_READ_ONLY=1 / defaults.read_only; --allow-writes overrides) and accept --dry-run."
metadata:
  requires:
    bins: ["wecom-calendar-cli"]
  cliHelp: "wecom-calendar-cli --help; wecom-calendar-cli sync --help; wecom-calendar-cli event list --help; wecom-calendar-cli meta --help"
---

# wecom-calendar

`wecom-calendar-cli` keeps a **local SQLite mirror** of a user's WeCom
(Enterprise WeChat) calendars, synced over CalDAV, and serves fast queries plus
a free-form metadata layer over it. Output is JSON by default; errors are JSON
on stderr with a `category`, a `code`, a `hint` and `next_steps`.

## This CLI or the official `wecom-cli`

Tencent's official `wecom-cli` (`@wecom/cli`) also reads WeCom calendars, and
the user may have both installed. They are complements — route by the request:

- **Use this CLI** when the window reaches more than 30 days before or after
  today, when the user wants offline or repeated queries, and for annotations
  (`meta`). `wecom-cli` cannot list schedules outside that range, even for
  events the user can see in the WeCom client; CalDAV has no such limit. Pass
  `--from`/`--to` explicitly, because `event list` itself defaults to 30
  days either side of today. For recurring events more than two years back or
  one year ahead, widen coverage first with
  `expand --from <date> --to <date>` (see
  [querying.md](references/querying.md)).
- **Use `wecom-cli`** to change the calendar: create, update or cancel an
  event, manage attendees, or find when several members are free. This CLI is
  read-only towards WeCom and has no command for any of that — say so and hand
  off rather than improvising.
- **Either works** for a read inside that range. Prefer this CLI when its store
  is already synced: the answer comes with recurring events expanded and
  annotations attached.

An empty, truncated or rejected result from `wecom-cli` for a window outside
that range is its limit, not missing data — sync and query here instead.

Checked against wecom-cli 1.3.2 on 2026-10-05.

## Golden rule — sync first, then read the store

Every query command (`calendar list`, `event list`, `meta get/list`) reads the
**local store**, not the WeCom server. So:

1. Run `wecom-calendar-cli sync` first (or when data may be stale). It pulls
   CalDAV changes into SQLite incrementally and idempotently; it **never**
   touches the metadata layer. A first/`--full` sync can run a while and emits
   bounded `{"_notice":{"progress":…}}` liveness on **stderr** — that is not the
   result (the result is the final JSON on stdout); silence it with
   `--progress none`.
2. Then query. If a read prints a `{"_notice":{"stale":…}}` line on **stderr**,
   the store is behind the server — re-run `sync` and query again.

Do not reach for a non-existent "live query" flag: the freshness contract is
`sync` → read. Only `calendar list --refresh` hits the server directly.

## Decision tree

- User wants to **refresh / pull the latest** calendar data → `sync`
  (`--full` to reconcile everything, `--calendar <id>` to scope one calendar,
  `--dry-run` to preview). See [syncing.md](references/syncing.md).
- User wants to **see which calendars exist** → `calendar list`
  (`--refresh` to re-list from the server first).
- User wants to **find events in a date range** → `event list --from
  YYYY-MM-DD --to YYYY-MM-DD` (`--calendar`, `--status`, `--include-meta`);
  "the last week" → `--since 7d`; "the next two weeks" →
  `--from now --to now+14d`. See [querying.md](references/querying.md).
- User wants **one event's full detail — description, location, organizer,
  attendees, or who they meet with** → find the uid with `event list`, then
  `event get <uid>` (attendees are flagged `is_self`; `--include-meta` attaches
  annotations). "Who am I" → `whoami`.
- User wants to **annotate, classify, tag, or link an event to a task**
  (e.g. a Feishu project item) → `meta set <uid> <namespace> <key> <value>`.
  It replaces the stored value; before overwriting or removing an annotation,
  follow [working-with-the-user.md](references/working-with-the-user.md).
- User wants to **read annotations** on an event → `meta get <uid> [ns] [key]`;
  across events → `meta list [--uid --namespace --key --value]` (`--value` is a
  reverse lookup: which events link to a task); to remove one →
  `meta delete <uid> <ns> <key>`. See [metadata.md](references/metadata.md).
- User asks **is it set up / why is a command failing** → `doctor`, then read
  the JSON error's `next_steps`. See
  [errors-and-exit-codes.md](references/errors-and-exit-codes.md).
- Nothing is configured yet → [getting-started.md](references/getting-started.md).
- A context **has no WeCom email, but another context on the same server is
  signed in** — typically a team preset beside a personal context, failing
  with `AUTH_NO_BASIC` → `--use-context <name> auth reuse --dry-run`, then the
  same without `--dry-run`. No password is involved; never ask for or issue
  one for this. See
  [team-setup.md](references/team-setup.md#reuse-an-existing-login--auth-reuse).
- User asks **where the CalDAV password comes from, or how to sign in** →
  `auth guide` (offline), then they run `auth login` in their own terminal.
  Someone is **rolling the CLI out to a team** → `config set-context`. See
  [team-setup.md](references/team-setup.md).

## Commands

```
wecom-calendar-cli sync [--full] [--calendar id] [--dry-run] [--progress auto|none|json]
                                       # CalDAV -> local SQLite (incremental); bounded progress on stderr
wecom-calendar-cli calendar list [--refresh]
                                       # calendars from the store (--refresh: server)
wecom-calendar-cli event list [--since 7d | --from <instant> [--to <instant>]] \
    [--calendar id] [--status s] [--include-meta] [--limit N] [--cursor c|--all]
                                       # events from the store, in a window [from, to)
wecom-calendar-cli event get <uid> [--occurrence key] [--include-meta]
                                       # one event in full: description, organizer, attendees(is_self)
wecom-calendar-cli expand [--from YYYY-MM-DD] [--to YYYY-MM-DD]
                                       # rebuild recurring occurrences; flags widen and pin the window
wecom-calendar-cli whoami              # the configured account (your identity)
wecom-calendar-cli meta set <uid> <ns> <key> <value> [--source s] [--dry-run]
wecom-calendar-cli meta get <uid> [ns] [key]        # read annotations
wecom-calendar-cli meta list [--uid u --namespace ns --key k --value v]
wecom-calendar-cli meta delete <uid> <ns> <key> [--dry-run] [--yes]  # remove one
wecom-calendar-cli config init|show|path|get-contexts|use-context|delete-context
wecom-calendar-cli config set-context <name> [--base-url u] [--credential-url u] \
    [--activate] [--overwrite] [--dry-run]          # offline service preset, no credentials
wecom-calendar-cli auth guide                       # offline: where the CalDAV password comes from
wecom-calendar-cli auth reuse [--from-context name] [--dry-run]
                                       # adopt the verified WeCom email of another context on the same server
wecom-calendar-cli auth login|status|logout         # Basic (email + CalDAV pw); login needs a terminal
wecom-calendar-cli doctor                           # config / creds / connectivity / Skill state
wecom-calendar-cli skill status|install|path|show|uninstall   # manage this Skill
wecom-calendar-cli version | completion
```

`sync` and the query commands are reads against the store; only `meta set` and
`meta delete` write. Both accept `--dry-run` and honor read-only mode;
`meta delete` is destructive, so it also needs `--yes` (or an interactive
confirmation) to apply. See [safety-modes.md](references/safety-modes.md).

## Event UIDs — the metadata key

Metadata attaches to an **event UID**, the stable identifier carried in every
`event list` item (`uid`). It survives re-sync, so a `meta set` you make today
still resolves after tomorrow's `sync`. Always take the `uid` from an
`event list` result — never invent one.

## Output & pagination conventions

- **stdout is data, stderr is notices/errors.** A successful pipeline parses
  stdout cleanly; `_notice` lines (stale store, update available, flag
  corrections, deprecated flags, NDJSON pagination) and errors go to stderr
  only.
- **List envelope.** `calendar list`, `event list` and `meta list` return
  `{items, next, has_more}`. One page per call; when `has_more` is true, pass
  `--cursor` with the `next` value for the following page, or `--all` to walk
  every page, or `--limit N` to size each request.
- `--format json|table|ndjson`; `--fields a,b.c` projects output down to the
  fields you need. `ndjson` prints the items only on stdout, one JSON object
  per line. When more pages exist it adds one stderr line,
  `{"_notice":{"next_steps":[…],"pagination":{"has_more":true,"next":"…"}}}`;
  pass that `next` as `--cursor`. Keep stderr when you page NDJSON — no such
  line means the result is complete.
- **A cursor needs a fixed window.** Page with absolute `--from`/`--to`,
  repeated on every call. `--since` and `now±duration` move with the clock and
  are rejected with `--cursor` (`CURSOR_RELATIVE_WINDOW`); read such a window
  in one call with `--all`.
- **Read what the question needs.** Pass a window sized to the request and
  stop paging once you have enough. `--limit` is a page size, not a total;
  `--all` is for a complete inventory of the window.

## Safety & read-only posture

Read-only mode blocks the two writes (`meta set`, `meta delete`) before they
touch the store, returning `READONLY_BLOCKED` (exit 5). Enable it with
`defaults.read_only: true` or `WECOM_CALENDAR_CLI_READ_ONLY=1`; the root
`--allow-writes` flag overrides it for one invocation whose write the current
task authorizes. `sync` is a read against the WeCom server and a write to the
*raw-fact* tables only — it is not blocked by read-only mode, and it never
writes or deletes metadata. Preview any write with `--dry-run`. Details in
[safety-modes.md](references/safety-modes.md).

## Working with the user

Carry out explicitly authorized work without repeatedly asking permission.
Reuse the `uid`, calendar id and window already supplied or returned in this
task instead of searching again. Resolve an ambiguous event before writing; a
dry run checks the request, not the user's intent. Respect an explicit
read-only scope, and hand calendar changes to `wecom-cli`.

Sync when the task starts or a read reports stale data — do not poll. Watch
for a change only when the user asks, with a deadline or stopping condition.

Report the answer first, then the events and window that support it. Say when
stale data, partial coverage, truncation or unread pages limit the conclusion,
and keep what the store shows apart from what you infer. Omit raw JSON and
event details the question did not need.

The full rules, including how to preserve existing annotations, are in
[working-with-the-user.md](references/working-with-the-user.md).

## Agent handshake — set `WECOM_CALENDAR_CLI_SKILL=0.5.0`

Once you have loaded this Skill, export that exact value in the environment
used to run the CLI. The CLI compares it with the embedded Skill version and
emits a structured `{"_notice":{"skill":{…}}}` line on **stderr** when the
Skill is missing, old, or uses the legacy unversioned handshake.
`wecom-calendar-cli skill status` reports loaded, installed, and embedded
versions. To suppress the notice without loading the Skill, set
`WECOM_CALENDAR_CLI_NO_SKILL_HINT=1`.

When a newer release exists, commands print a one-line
`{"_notice":{"update":{…}}}` to **stderr** (never stdout, so parsing the data
is unaffected). Follow every `next_steps` entry: upgrade the CLI, run
`wecom-calendar-cli skill install`, then reload the agent context. `doctor`
reports CLI and Skill status too. Silence update notices with
`WECOM_CALENDAR_CLI_NO_UPDATE_NOTIFIER=1`.

## Credentials (agents)

Auth is HTTP **Basic**: the user's WeCom email as username and an
**app-specific CalDAV password** as the secret. The password is issued in the
WeCom mobile app (Workbench → Calendar → settings → Sync to other calendars);
`auth guide` prints those steps offline. The user has normally already
configured this. **Reuse their existing config and credentials** from
`~/.angelmsger/wecom-calendar/config.yaml` + the OS keychain — do not run
`config init` to create a fresh setup, and **never print or echo the CalDAV
password** (not in logs, not in shell history, not in output).

**Issuing a new CalDAV password invalidates the previous one**, and every
calendar client still using the old one stops syncing. So keep three
situations apart, and check them in this order:

- **The credential store is inaccessible** (`CREDENTIAL_STORE_INACCESSIBLE`,
  or any error with `recovery.scope=host`, which includes
  `CREDENTIAL_NOT_VISIBLE_OR_MISSING` from a sandbox): request elevated / host
  access and retry the same command once. Do not re-initialize config, run
  `auth login` or `auth reuse`, or suggest a new password.
- **Only the identity is missing**: the context has no WeCom email, and another
  context on the same server has one (`AUTH_NO_BASIC`; the error, `auth guide`,
  `doctor` and `config set-context` then list `auth reuse --dry-run` ahead of
  any login step). Contexts on one server share one stored password, so run
  `--use-context <name> auth reuse --dry-run` and, when it reports
  `available`, the same without `--dry-run`. Nobody enters a password.
- **A credential has to be acquired**: the host retry also reports it missing
  and there is no login to reuse, or the server rejected the stored password
  (`CALDAV_AUTH`, HTTP 401). Only then point the user at `auth guide` and ask
  them to run `auth login` in their own terminal.

See [getting-started.md](references/getting-started.md).

## Team service presets and authentication

- Inspect existing configuration and reuse it. `config set-context <name>` is
  the offline installer entrypoint; it accepts `--base-url`, `--auth-scheme`,
  `--credential-url`, `--activate`, `--overwrite` and `--dry-run`. The server
  defaults to the public WeCom endpoint, and `basic` is the only scheme.
- `WECOM_CALENDAR_AUTH_SCHEME` and `WECOM_CALENDAR_CREDENTIAL_URL` complement
  `WECOM_CALENDAR_SERVER`. Presets never copy the WeCom email or the CalDAV
  password from the environment. Conflicts preserve existing values unless
  explicitly overwritten.
- Run `auth guide` for the acquisition steps and their source. WeCom has no web
  credential page, so `credential_url` is empty unless the team configured a
  page of its own. Never invent a URL, and never send a credential to one.
- Once a service is preset, look for an existing login before asking for one.
  When `next_steps` starts with `auth reuse --dry-run`, another context on
  that server already has a WeCom email: preview, and when the preview reports
  `available`, run `auth reuse`. It verifies the stored password with the
  other context's email and records that email on the preset. It copies no
  secret, replaces no email, activates nothing, and reads no credential from
  the environment. Its result has `context`, `state`
  (`available`, `reused`, `unchanged`, `unavailable`), `changed`, `verified`,
  `dry_run`, and `source_context` or `reason`. `unchanged` and `unavailable`
  do not prove the context can authenticate — keep the `auth status` check.
- Otherwise direct the member to `auth login` in their own terminal; it
  verifies the password and saves it with their email. Do not ask for the
  password in chat or pass it as an argument. In non-interactive environments
  use `WECOM_CALENDAR_USERNAME` and `WECOM_CALENDAR_PASSWORD`.
- A server/context mismatch needs a matching context, not a new password. A
  partial login error names what was stored; the recovery is `auth login` again
  with the same password. `AUTH_REUSE_AMBIGUOUS` needs `--from-context <name>`
  with a name from `config get-contexts`, not a login.

See [team-setup.md](references/team-setup.md) for the output fields, conflict
semantics, the credential page override, login reuse and failure recovery.

## Global flags

`--format json|table|ndjson` · `--fields a,b.c` · `--config <dir>` ·
`--use-context <name>` (pick a named server) · `--base-url <url>` ·
`--auth-scheme basic` · `--credential-url <page>` (display only) ·
`--allow-writes` · `--verbose`
