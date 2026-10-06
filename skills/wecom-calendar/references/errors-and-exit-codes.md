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
instead. Some errors add an optional `details` object with non-secret context:
the differing fields of a preset conflict, or what a partial login stored.

## Exit codes

| Code | Category | Meaning & recovery |
|------|----------|--------------------|
| 0 | — | success |
| 1 | internal | unexpected bug; re-run with `--verbose` |
| 2 | usage | bad flags/arguments (e.g. `--to` without `--from`); read `next_steps`, check `--help` |
| 3 | config | config/credential resolution failed; inspect `code` and `recovery` before reconfiguring |
| 4 | auth | credentials rejected (401); run `auth status`, then the user runs `auth login` |
| 5 | permission | valid login, no access (403), or local `READONLY_BLOCKED` |
| 6 | not_found | the event is not in the local store, the server answered 404, or `auth reuse --from-context` names an unknown context; a missing metadata entry is not an error |
| 7 | rate_limit | server throttling (429); wait, then retry; avoid `sync --full` in a tight loop |
| 8 | network | DNS/TLS/timeout; check `WECOM_CALENDAR_SERVER`, run `doctor` |
| 9 | server | CalDAV 5xx; retry later |
| 10 | parse | a response (or a `.ics` body) could not be decoded; likely a client bug — re-run with `--verbose` |
| 11 | conflict | a preset conflicts with an existing context (`CONFIG_CONTEXT_CONFLICT`), `auth reuse` needs a choice or saw a change (`AUTH_REUSE_*`), or the server answered 409; `meta` writes never conflict (see below) |

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
  sandbox. Its `next_steps` never point at acquiring a credential: do not
  suggest a new CalDAV password, which would invalidate the one the user's
  other calendar clients hold.
- **`CREDENTIAL_NOT_VISIBLE_OR_MISSING`** (config, 3) → no credential resolved.
  In a sandbox it usually means the user's credential is just unreadable from
  here: request elevation and retry. Only when the host retry also reports it
  missing has the user not configured one — then follow the later
  `next_steps` in order: `auth reuse --dry-run` when it is listed, then
  `auth guide`, and the user runs `auth login` (or `config init`) or exports
  `WECOM_CALENDAR_*`.
- **`AUTH_NO_BASIC`** (config, 3) → an email or a password is missing. The
  common case is a team preset beside a signed-in personal context: contexts
  on one server share the stored password, so only the email is missing. The
  `hint` then says so and `next_steps` starts with `auth reuse --dry-run` —
  run it and, when it reports `available`, `auth reuse`; no password is
  needed. Otherwise (for example `WECOM_CALENDAR_PASSWORD` without a username)
  `next_steps` end with `auth guide`.
- **`AUTH_REUSE_AMBIGUOUS`** (conflict, 11) → `auth reuse` verified more than
  one email for the server. `details.contexts` names the candidates; repeat
  with `--from-context <name>`, taking the name from `config get-contexts`.
- **`AUTH_REUSE_SOURCE_NOT_FOUND`** (not_found, 6) /
  **`AUTH_REUSE_SOURCE_MISMATCH`** (conflict, 11) → `--from-context` names a
  context that does not exist, or one on another server or scheme.
- **`AUTH_REUSE_TARGET_MISSING`** (config, 3) /
  **`AUTH_REUSE_TARGET_MISMATCH`** (conflict, 11) → the selected context is
  not in the config file, or a `--base-url` / `--auth-scheme` override selects
  another service than it stores.
- **`AUTH_REUSE_CONFIG_CHANGED`** / **`AUTH_REUSE_CREDENTIAL_CHANGED`**
  (conflict, 11) / **`AUTH_REUSE_WRITE_FAILED`** (config, 3) → the config file
  or the stored password changed during verification, or the result could not
  be written. Nothing was saved; run `auth reuse --dry-run` again. No
  `AUTH_REUSE_*` error is a reason to issue a CalDAV password. See
  [team-setup.md](team-setup.md#reuse-an-existing-login--auth-reuse).
- **`CREDENTIAL_SERVICE_MISMATCH`** (config, 3) → the service URL in effect is
  not the one the stored credential belongs to. Select the context for the
  intended service; this is not a password problem.
- **`CALDAV_AUTH`** (auth, 4) → the server rejected the CalDAV password.
  The most common cause is that a **new app-specific password was fetched in the
  WeCom app, invalidating the old one** — the user gets a fresh one (Workbench →
  Calendar → settings → Sync to other calendars; `auth guide` prints the steps)
  and runs `auth login` or `config init` again.
- **`CONFIG_CONTEXT_CONFLICT`** (conflict, 11) → `config set-context` would
  change a non-empty service field of an existing context. `details` lists each
  field's `before` and `after`. Use `--overwrite` only when the user wants that
  context changed, or pick another name. See [team-setup.md](team-setup.md).
- **`CONTEXT_BASE_URL_MISMATCH`** (config, 3) → `auth login` saw a service URL
  that differs from the selected context's. Nothing was verified or stored.
  Select or create a matching context; do not treat it as a password problem.
- **`AUTH_LOGIN_NEEDS_TTY`** (config, 3) → `auth login` has no terminal. Ask the
  user to run it themselves, or use `WECOM_CALENDAR_USERNAME` and
  `WECOM_CALENDAR_PASSWORD` for a non-interactive run.
- **`CREDENTIAL_SAVE_FAILED`** / **`LOGIN_CONFIG_WRITE_FAILED`** (config, 3) →
  `auth login` verified the password but could not finish saving. The first
  stored nothing; the second stored the password and reports
  `credential_stored: true` in `details`. Fix the access problem and run
  `auth login` again with the **same** password.
- **`AUTH_BAD_SCHEME`** (config, 3) → an auth scheme other than `basic` was
  supplied through `--auth-scheme`, `WECOM_CALENDAR_AUTH_SCHEME` or the config
  file. WeCom CalDAV accepts only `basic`.
- **`BAD_BASE_URL`** / **`BAD_CREDENTIAL_URL`** (config, 3) → the server URL or
  the credential page is not an absolute HTTP(S) URL, or it embeds credentials;
  a server URL may not carry a query or fragment either.
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
  user to fetch a fresh one and run `auth login` in their own terminal;
  `auth guide` prints where it comes from. Host access retries apply to the
  credential-resolution codes above, not to an ordinary 401. Do not initialize
  a replacement config in a sandbox.
- **config (3) from credential resolution** → keep "the store is inaccessible"
  apart from "a credential must be acquired". The first is a host retry and
  nothing else. Before the second, check for a login to reuse: when
  `next_steps` lists `auth reuse --dry-run`, run it first. See
  [team-setup.md](team-setup.md#a-new-password-invalidates-the-previous-one).
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
