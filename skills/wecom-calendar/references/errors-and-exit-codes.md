# Errors and exit codes

On failure `wecom-calendar-cli` writes a JSON object to **stderr** and exits
with a category-specific code. stdout stays empty, so a successful pipeline
never has to parse errors.

## Error shape

```json
{
  "error": {
    "category": "auth",
    "code": "HTTP_Unauthorized",
    "message": "CalDAV server returned HTTP 401",
    "hint": "The server rejected the credentials. The app-specific CalDAV password may have been re-issued.",
    "next_steps": ["wecom-calendar-cli auth status", "wecom-calendar-cli config init"],
    "retryable": false,
    "http_status": 401
  }
}
```

Always read `hint` and `next_steps` — they tell you how to recover.
`retryable` indicates whether retrying in the same environment can succeed.
Environment changes such as a host retry use the optional `recovery` object
instead.

## Exit codes

| Code | Category | Meaning & recovery |
|------|----------|--------------------|
| 0 | — | success |
| 1 | internal | unexpected bug; re-run with `--verbose` |
| 2 | usage | bad flags/arguments (e.g. missing `--since`/`--until`); check `--help` |
| 3 | config | config/credential resolution failed; inspect `code` and `recovery` before reconfiguring |
| 4 | auth | credentials rejected (401); run `auth status`, re-`config init` |
| 5 | permission | valid login, no access (403), or local `READONLY_BLOCKED` |
| 6 | not_found | calendar / event / metadata row does not exist (404 or empty store) |
| 7 | rate_limit | server throttling (429); wait, then retry; avoid `sync --full` in a tight loop |
| 8 | network | DNS/TLS/timeout; check `WECOM_CALENDAR_SERVER`, run `doctor` |
| 9 | server | CalDAV 5xx; retry later |
| 10 | parse | a response (or a `.ics` body) could not be decoded; likely a client bug — re-run with `--verbose` |
| 11 | conflict | a write hit a conflict; re-read with `meta get`, then retry |

## Common codes

- **`CREDENTIAL_STORE_INACCESSIBLE`** (config, 3) → the OS keychain could not be
  opened (common in a sandbox). When `recovery.scope` is `host`, request host
  access and retry the **same** command once; do not re-initialize config in the
  sandbox.
- **`CREDENTIAL_NOT_VISIBLE_OR_MISSING`** (config, 3) → no credential resolved.
  On the host this means the user has not configured one — ask them to run
  `config init` or export `WECOM_CALENDAR_*`. In a sandbox it usually means the
  user's credential is just unreadable from here: request elevation and retry.
- **`HTTP_Unauthorized`** (auth, 4) → the server rejected the CalDAV password.
  The most common cause is that a **new app-specific password was fetched in the
  WeCom app, invalidating the old one** — get a fresh one (Workbench → Calendar →
  settings → Sync to other calendars) and re-run `config init` / `auth login`.
- **`READONLY_BLOCKED`** (permission, 5) → a `meta set` / `meta delete` was
  blocked by read-only mode. Add `--allow-writes` to send it, or `--dry-run` to
  preview. See [safety-modes.md](safety-modes.md).
- **`STORE_STALE`** notice (not an error) → printed on **stderr** as
  `{"_notice":{"stale":…}}` when a read runs against a store that is behind the
  server. It does not fail the command; re-run `sync` and query again for
  current data.
- **`UNKNOWN_COMMAND`** (usage, 2) → a typo'd subcommand; the message carries a
  "Did you mean" suggestion.

## Recovery patterns

- **auth (4)** → `wecom-calendar-cli auth status`; if the password was
  re-issued, get a new one and re-`config init`. Agents in a sandbox: the
  credential is usually the user's, just unreadable from the sandbox — request
  elevation and retry rather than re-initializing.
- **not_found (6)** → verify the calendar `id` or event `uid` from a fresh
  `calendar list` / `event list`; if the store looks empty, you probably have
  not synced — run `sync` first.
- **permission (5)** → either a 403 from the server (the credential works but
  lacks rights — not fixable by retrying) **or** `READONLY_BLOCKED` from local
  read-only mode. For the latter, add `--allow-writes` (send) or `--dry-run`
  (preview). See [safety-modes.md](safety-modes.md).
- **rate_limit (7) / server (9) / network (8)** → `retryable: true`; wait and
  retry, and prefer an incremental `sync` over `sync --full` when throttled.
