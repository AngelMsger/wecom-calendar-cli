---
name: wecom-calendar
version: 0.1.0
description: "Sync a user's WeCom (Enterprise WeChat) calendars over CalDAV into a local SQLite store, then query events and calendars from it and maintain a free-form, agent-owned metadata layer (event classification, external task links). Use this skill when the user mentions a WeCom / 企业微信 calendar, schedule or 日程; asks to sync a calendar, refresh calendar data, list calendars, query or find events in a date range, or see calendar events; or wants to annotate, classify or tag an event, link an event to a task (e.g. a Feishu project item), or read those annotations. Queries read the local store, so run `sync` first and re-sync whenever a read prints a `_notice.stale`. `meta set` / `meta delete` are the only writes; they honor a session read-only posture (WECOM_CALENDAR_CLI_READ_ONLY=1 / defaults.read_only, overridable with --allow-writes) and every write also accepts --dry-run to preview before applying."
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

## Golden rule — sync first, then read the store

Every query command (`calendar list`, `event list`, `meta get/list`) reads the
**local store**, not the WeCom server. So:

1. Run `wecom-calendar-cli sync` first (or when data may be stale). It pulls
   CalDAV changes into SQLite incrementally and idempotently; it **never**
   touches the metadata layer.
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
- User wants to **find events in a date range** → `event list --since
  YYYY-MM-DD --until YYYY-MM-DD` (`--calendar`, `--limit`). See
  [querying.md](references/querying.md).
- User wants to **annotate, classify, tag, or link an event to a task**
  (e.g. a Feishu project item) → `meta set <uid> <namespace> <key> <value>`.
- User wants to **read annotations** on an event → `meta get <uid> [ns] [key]`;
  across events → `meta list [--uid --namespace --key]`; to remove one →
  `meta delete <uid> <ns> <key>`. See [metadata.md](references/metadata.md).
- User asks **is it set up / why is a command failing** → `doctor`, then read
  the JSON error's `next_steps`. See
  [errors-and-exit-codes.md](references/errors-and-exit-codes.md).
- Nothing is configured yet → [getting-started.md](references/getting-started.md).

## Commands

```
wecom-calendar-cli sync [--full] [--calendar id] [--dry-run]
                                       # CalDAV -> local SQLite (incremental)
wecom-calendar-cli calendar list [--refresh]
                                       # calendars from the store (--refresh: server)
wecom-calendar-cli event list --since YYYY-MM-DD --until YYYY-MM-DD \
    [--calendar id] [--limit N]        # events from the store, in a window
wecom-calendar-cli meta set <uid> <ns> <key> <value> [--source s] [--dry-run]
wecom-calendar-cli meta get <uid> [ns] [key]        # read annotations
wecom-calendar-cli meta list [--uid u --namespace ns --key k]
wecom-calendar-cli meta delete <uid> <ns> <key>     # remove one annotation
wecom-calendar-cli config init|show|path|get-contexts|use-context|delete-context
wecom-calendar-cli auth login|status|logout         # Basic (email + CalDAV pw)
wecom-calendar-cli doctor                           # config / creds / connectivity
wecom-calendar-cli version | completion | skill install|status
```

`sync` and the query commands are reads against the store; only `meta set` and
`meta delete` write. Both writes accept `--dry-run` and honor read-only mode.
See [safety-modes.md](references/safety-modes.md).

## Event UIDs — the metadata key

Metadata attaches to an **event UID**, the stable identifier carried in every
`event list` item (`uid`). It survives re-sync, so a `meta set` you make today
still resolves after tomorrow's `sync`. Always take the `uid` from an
`event list` result — never invent one.

## Output & pagination conventions

- **stdout is data, stderr is notices/errors.** A successful pipeline parses
  stdout cleanly; `_notice` lines (stale store, update available, flag
  corrections) and errors go to stderr only.
- **List envelope.** `calendar list`, `event list` and `meta list` return
  `{items, next, has_more}`. One page per call; when `has_more` is true, pass
  `--cursor` with the `next` value for the following page, or `--all` to walk
  every page, or `--limit N` to size each request.
- `--format json|table|ndjson` (`ndjson` streams items, one JSON object per
  line); `--fields a,b.c` projects output down to the fields you need.

## Safety & read-only posture

Read-only mode blocks the two writes (`meta set`, `meta delete`) before they
touch the store, returning `READONLY_BLOCKED` (exit 5). Enable it with
`defaults.read_only: true` or `WECOM_CALENDAR_CLI_READ_ONLY=1`; override for a
single invocation with the root `--allow-writes` flag. `sync` is a read against
the WeCom server and a write to the *raw-fact* tables only — it is not blocked
by read-only mode, and it never writes or deletes metadata. Preview any write
with `--dry-run`. Details in [safety-modes.md](references/safety-modes.md).

## Agent handshake — set `WECOM_CALENDAR_CLI_SKILL=1`

Once you have loaded this Skill, export `WECOM_CALENDAR_CLI_SKILL=1` in the
environment you run the CLI from. When the variable is absent the CLI assumes
you may be inferring commands blindly and prints a one-line
`{"_notice":{"skill":{…}}}` discovery hint on **stderr** (non-interactive
sessions only). Setting it silences the hint; `wecom-calendar-cli skill status`
reports whether it is set. Update notices are also one-line
`{"_notice":{"update":{…}}}` on stderr — they never pollute stdout.

## Credentials (agents)

Auth is HTTP **Basic**: the user's WeCom email as username and an
**app-specific CalDAV password** as the secret (obtained in the WeCom mobile
app: Workbench → Calendar → settings → Sync to other calendars — fetching a new
one invalidates the old). The user has normally already configured this.
**Reuse their existing config and credentials** from
`~/.angelmsger/wecom-calendar/config.yaml` + the OS keychain — do not run
`config init` to create a fresh setup, and **never print or echo the CalDAV
password** (not in logs, not in shell history, not in output). If credentials
are missing or unreadable (`CREDENTIAL_STORE_INACCESSIBLE` /
`CREDENTIAL_NOT_VISIBLE_OR_MISSING`, or `recovery.scope=host`), request
elevated / host access and retry the same command once — do not re-initialize
config inside a sandbox. See [getting-started.md](references/getting-started.md).

## Global flags

`--format json|table|ndjson` · `--fields a,b.c` · `--config <dir>` ·
`--use-context <name>` (pick a named server) · `--allow-writes` · `--verbose`
