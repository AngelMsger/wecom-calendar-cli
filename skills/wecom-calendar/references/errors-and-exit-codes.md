# Errors and exit codes

On failure `wecom-calendar-cli` writes a JSON object to **stderr** and exits
with a category-specific code. stdout stays empty, except that an unhealthy
`doctor` prints its report there before failing.

## Error shape

```json
{
  "error": {
    "category": "auth",
    "code": "CALDAV_AUTH",
    "message": "CalDAV server returned HTTP 401",
    "hint": "The server rejected the credentials. The app-specific password may have been refreshed.",
    "next_steps": ["wecom-calendar-cli auth status", "wecom-calendar-cli auth login"],
    "retryable": false,
    "http_status": 401
  }
}
```

Always read `hint` and `next_steps` — they tell you how to recover.
`retryable` indicates whether retrying the same invocation can help safely.
Environment changes such as a host retry use the optional `recovery` object
instead.

## Exit codes

| Code | Category | Meaning & recovery |
|------|----------|--------------------|
| 0 | — | success |
| 1 | internal | unexpected bug; re-run with `--verbose` |
| 2 | usage | bad flags/arguments (e.g. `--to` without `--from`); read `next_steps`, check `--help` |
| 3 | config | config/credential resolution failed; inspect `code` and `recovery` before reconfiguring |
| 4 | auth | credentials rejected (401); run `auth status`, re-`config init` |
| 5 | permission | valid login, no access (403), or local `READONLY_BLOCKED` |
| 6 | not_found | the event is not in the local store, or the server answered 404; a missing metadata entry is not an error |
| 7 | rate_limit | server throttling (429); wait, then retry; avoid `sync --full` in a tight loop |
| 8 | network | DNS/TLS/timeout; check `WECOM_CALENDAR_SERVER`, run `doctor` |
| 9 | server | CalDAV 5xx; retry later |
| 10 | parse | a response (or a `.ics` body) could not be decoded; likely a client bug — re-run with `--verbose` |
| 11 | conflict | the server answered 409; `meta` writes never conflict (see below) |

## Writes that succeeded

`meta set` and `meta delete` are single statements against the local store.
Nothing is read back afterwards and `(uid, namespace, key)` is your own input,
so the only step that can fail once the change is committed is printing the
result — an unknown `--format`, for example.

`WRITE_SUCCEEDED_OUTPUT_FAILED` reports exactly that. The message names the
entry and the status it reached (`set` or `deleted`), the error keeps the
category and exit code of the output failure, and `retryable` is `false`:

```json
{
  "error": {
    "category": "usage",
    "code": "WRITE_SUCCEEDED_OUTPUT_FAILED",
    "message": "Write succeeded for metadata entry <uid> / task / link (status set), but printing its result failed: unknown output format \"yaml\" (want json, table or ndjson)",
    "hint": "Do not repeat the write; the local store already holds the change. Confirm it with the read command. After a delete, an empty list is the confirmation.",
    "next_steps": ["wecom-calendar-cli meta get '<uid>' 'task' 'link'"],
    "retryable": false
  }
}
```

Run the `meta get` command from `next_steps` and do not repeat the write. A
non-zero exit is not proof that nothing changed.

Every other failure of a `meta` write happens before the change is applied:
read-only mode, a missing `--yes`, a store that cannot be opened, or a
statement the store rejects all leave the entry as it was. There is no
"outcome unknown" case, because no network sits between the CLI and the store.

Two results look like failures and are not:

- `meta delete` on an entry that does not exist exits 0 with
  `"status": "not_found"` — nothing changed.
- `meta set` never reports a conflict. It overwrites without a version check,
  so read first when the current value matters; see
  [working-with-the-user.md](working-with-the-user.md#preserve-annotations-you-were-not-asked-to-change).

## Common codes

- **`CREDENTIAL_STORE_INACCESSIBLE`** (config, 3) → the OS keychain could not be
  opened (common in a sandbox). When `recovery.scope` is `host`, request host
  access and retry the **same** command once; do not re-initialize config in the
  sandbox.
- **`CREDENTIAL_NOT_VISIBLE_OR_MISSING`** (config, 3) → no credential resolved.
  On the host this means the user has not configured one — ask them to run
  `config init` or export `WECOM_CALENDAR_*`. In a sandbox it usually means the
  user's credential is just unreadable from here: request elevation and retry.
- **`CALDAV_AUTH`** (auth, 4) → the server rejected the CalDAV password.
  The most common cause is that a **new app-specific password was fetched in the
  WeCom app, invalidating the old one** — get a fresh one (Workbench → Calendar →
  settings → Sync to other calendars) and re-run `config init` / `auth login`.
- **`READONLY_BLOCKED`** (permission, 5) → a `meta set` / `meta delete` was
  blocked by read-only mode. Use `--allow-writes` only when the current task
  authorizes that write, or `--dry-run` to preview. See
  [safety-modes.md](safety-modes.md).
- **`BAD_TIME_RANGE`** (usage, 2) → the window flags of `event list` or
  `expand` do not form a usable window: `--since` combined with `--from`/`--to`,
  `--to` without `--from`, an end that is not after the start, an unreadable
  value, or a look-back `--since` on `expand`. The `hint` restates the rules and
  `next_steps` are working examples. See
  [querying.md](querying.md#list-events-in-a-window).
- **`CURSOR_RELATIVE_WINDOW`** (usage, 2) → `--cursor` was passed with a window
  that moves with the clock (`--since`, `now±duration`). Restart from the first
  page with the absolute `--from`/`--to` in `next_steps`, or use `--all`.
- **`CURSOR_MISMATCH`** / **`BAD_CURSOR`** (usage, 2) → the cursor belongs to a
  different window or calendar, or is not a cursor. Restart without `--cursor`
  and keep `--from/--to/--calendar` identical across pages.
- **Deprecated-flag notice** (not an error) → `--until`, or `--since` with a
  date, printed `{"_notice":{"deprecated_flag":{…}}}` on **stderr**. The
  command still ran; switch to the `replacement` it names (`--to`, `--from`).
  `WECOM_CALENDAR_CLI_NO_DEPRECATION_NOTICE=1` silences it.
- **Pagination notice** (not an error) → with `--format ndjson`, a page that has
  more prints `{"_notice":{"pagination":{…}}}` on **stderr**; pass its `next`
  as `--cursor`.
- **Stale-store notice** (not an error) → printed on **stderr** as
  `{"_notice":{"stale":…}}` when a read runs against a store that is behind the
  server. It does not fail the command; re-run `sync` and query again for
  current data.
- **`UNKNOWN_COMMAND`** (usage, 2) → a typo'd subcommand; the message carries a
  "Did you mean" suggestion.

## Recovery patterns

- **auth (4)** → `wecom-calendar-cli auth status`; a server 401 means the
  stored password was rejected, usually because a new one was issued. Ask the
  user to fetch a fresh one and run `auth login` in their own terminal. Host
  access retries apply to the credential-resolution codes above, not to an
  ordinary 401. Do not initialize a replacement config in a sandbox.
- **not_found (6)** → verify the calendar `id` or event `uid` from a fresh
  `calendar list` / `event list`; if the store looks empty, you probably have
  not synced — run `sync` first.
- **permission (5)** → either a 403 from the server (the credential works but
  lacks rights — not fixable by retrying) **or** `READONLY_BLOCKED` from local
  read-only mode. For the latter, use `--allow-writes` only when the current
  task authorizes the concrete write despite that default; never override an
  explicit read-only instruction. To preview, add `--dry-run`. See
  [safety-modes.md](safety-modes.md).
- **rate_limit (7) / server (9) / network (8)** → these come from requests to
  the WeCom server, which are all reads. When `retryable` is `true`, retry
  with bounded backoff — a few attempts, not a loop — and prefer an
  incremental `sync` over `sync --full` when throttled.
