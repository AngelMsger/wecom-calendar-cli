# Team setup reference

Service settings are shared; the WeCom email and the CalDAV password belong to
one person. The CLI keeps them apart: an installer presets the service, and
each member signs in.

Before any of this, look at what exists — `config get-contexts`, `auth status`
— and reuse it. A user with a working setup needs no new context.

## Distribution and login

An installer can write a named context without a network connection or access
to the keychain:

```bash
wecom-calendar-cli config set-context team \
  --credential-url https://wiki.example.com/wecom-caldav --activate

# A member who is already signed in on that server needs no password.
wecom-calendar-cli --use-context team auth reuse --dry-run
wecom-calendar-cli --use-context team auth reuse

# Otherwise the member completes personal authentication in a terminal.
wecom-calendar-cli --use-context team auth guide
wecom-calendar-cli --use-context team auth login
```

`--base-url` is optional here: the server defaults to the public WeCom CalDAV
endpoint, `https://caldav.wecom.work`. `--auth-scheme` accepts only `basic`,
the one scheme WeCom CalDAV speaks; any other value is `AUTH_BAD_SCHEME`.
`--credential-url` is an optional page of the team's own, such as an internal
how-to.

The result names the context, what changed and what to run next:

```json
{
  "changed": true,
  "changes": {
    "auth.credential_url": {
      "after": "https://wiki.example.com/wecom-caldav",
      "before": ""
    },
    "auth.scheme": {
      "after": "basic",
      "before": ""
    },
    "current_context": {
      "after": "team",
      "before": ""
    },
    "server": {
      "after": "https://caldav.wecom.work",
      "before": ""
    }
  },
  "config_file": "<config_dir>/config.yaml",
  "context": "team",
  "current_context": "team",
  "dry_run": false,
  "next_steps": [
    "wecom-calendar-cli --use-context 'team' auth guide",
    "wecom-calendar-cli --use-context 'team' auth login"
  ]
}
```

When another context on the same server already has a WeCom email,
`next_steps` starts with a third entry,
`wecom-calendar-cli --use-context 'team' auth reuse --dry-run`: the member is
signed in there, and [reuse](#reuse-an-existing-login--auth-reuse) completes
the preset without a password. `config set-context` reads only the config file
to decide that.

`config set-context <name>` resolves **flags > environment > `.env` > the named
target context > defaults**. It ignores personal environment fields and secrets
— `WECOM_CALENDAR_USERNAME` and `WECOM_CALENDAR_PASSWORD`, including the scheme
a password would otherwise imply — and it ignores `--use-context` and
`WECOM_CALENDAR_CONTEXT`, because the name argument is the target. It never
verifies connectivity, reads or writes the keychain, or changes another
context's values or the shared `defaults`. An existing username stays as it is.
The first context becomes current; later calls change the current context only
with `--activate`.

Identical presets do not rewrite the file (`"changed": false`). Conflicting
non-empty service fields return `CONFIG_CONTEXT_CONFLICT` (exit 11) with a
`details` object holding each field's `before` and `after` values:

```json
{
  "error": {
    "details": {
      "server": {
        "before": "https://caldav.wecom.work",
        "after": "https://caldav.example.com/dav"
      }
    },
    "category": "conflict",
    "code": "CONFIG_CONTEXT_CONFLICT",
    "message": "team presets conflict with the existing context",
    "hint": "Inspect the field differences; use --overwrite to update the supplied service fields, or choose another context name.",
    "next_steps": [
      "wecom-calendar-cli config set-context --help"
    ],
    "retryable": false
  }
}
```

Inspect those differences, then pass `--overwrite` to update the supplied
service fields, or use another context name. Overwriting a context someone
else set up is the user's decision, not a retry. Fields you do not supply are
retained. `--dry-run` uses the same merge and conflict checks and returns the
proposed changes without writing anything; `--overwrite --dry-run` previews a
deliberate conflicting update.

## Where the password comes from — `auth guide`

`auth guide` works offline and never touches the credential store. It returns
`server`, `scheme`, `credential_url`, `source`, `instructions`,
`documentation_url` and `next_steps`.

WeCom has no web page for the CalDAV password. It is issued in the **mobile
app** — Workbench → Calendar → settings → Sync to other calendars — and
`instructions` carries those steps. So, unlike the sibling CLIs, the guide has
no URL of its own:

```bash
wecom-calendar-cli auth guide --fields credential_url,source
# {
#   "credential_url": "",
#   "source": "builtin"
# }
```

When a team configured a page, `credential_url` is that page and `source` says
where it came from — `flag`, `env`, `dotenv` or `file`:

```bash
wecom-calendar-cli --use-context team auth guide \
  --fields server,scheme,credential_url,source,next_steps
# {
#   "credential_url": "https://wiki.example.com/wecom-caldav",
#   "next_steps": [
#     "wecom-calendar-cli --use-context 'team' auth login"
#   ],
#   "scheme": "basic",
#   "server": "https://caldav.wecom.work",
#   "source": "file"
# }
```

The page is **display-only**: something for the user to open and read. The CLI
never requests it. Do not fetch it with credentials, do not send the password
to it, and do not invent a URL when the field is empty — give the user the
`instructions` instead.

When the selected context has no WeCom email and another context on the same
server has one, the guide leads with reuse: `instructions` starts with a line
that says so, and `next_steps` puts it ahead of `auth login`. Follow that order.

```bash
wecom-calendar-cli --use-context team auth guide --fields next_steps
# {
#   "next_steps": [
#     "wecom-calendar-cli --use-context 'team' auth reuse --dry-run",
#     "wecom-calendar-cli --use-context 'team' auth login"
#   ]
# }
```

## A new password invalidates the previous one

Issuing a new CalDAV password in the WeCom app invalidates the one before it.
Every calendar client still using the old password — a phone, a desktop
calendar, this CLI on another machine — stops syncing until it is updated. So
a new password is the answer to only two situations, and never to the other
two:

| Situation | How it shows | What to do |
|-----------|--------------|------------|
| The credential store cannot be read | `CREDENTIAL_STORE_INACCESSIBLE`, or any error with `recovery.scope=host` | Retry the same command with access to the user's home directory and OS keychain. **Do not** issue a password, run `auth login`, `auth reuse` or `config init`. |
| Only the WeCom email is missing | `AUTH_NO_BASIC` on a context beside one that is signed in on the same server; `next_steps` starts with `auth reuse --dry-run` | Run `auth reuse --dry-run`, then `auth reuse`. **Do not** issue a password or run `auth login`. |
| No credential exists | `CREDENTIAL_NOT_VISIBLE_OR_MISSING` on the host too, or `auth status` reports `"configured": false` there, and `auth reuse` has nothing to offer | The user runs `auth login`. They reuse the current password if they have it, and issue one only if they do not. |
| The stored password was rejected | `CALDAV_AUTH` (auth, exit 4, HTTP 401) | The user issues a new password, runs `auth login`, and updates their other calendar clients. |

A sandbox that cannot see the keychain produces the first situation, and the
last two look similar from inside it. Retry on the host before concluding that
a credential is missing.

## Reuse an existing login — `auth reuse`

A team preset usually sits beside a personal context on the same server. The
stored password is keyed by the server's host and the scheme, so the two
contexts **already share it**. The preset lacks only the WeCom email, and
every command there fails with `AUTH_NO_BASIC`. `auth reuse` records the email
without anyone entering a password:

```bash
wecom-calendar-cli --use-context team auth reuse --dry-run   # verify and preview
wecom-calendar-cli --use-context team auth reuse             # record the email
wecom-calendar-cli --use-context team auth status            # the separate check
```

It considers every other context with the same complete server URL and scheme
that has an email, before it reads any credential. For each it verifies the
stored password with that email — the authenticated calendar-home request
`auth login` and `doctor` send — and records the one that verifies on the
selected context. `--dry-run` runs the same verification and writes nothing:

```json
{
  "changed": true,
  "context": "team",
  "dry_run": true,
  "source_context": "personal",
  "state": "available",
  "verified": true
}
```

| `state` | Meaning | `changed` | `verified` |
|---------|---------|-----------|------------|
| `available` | `--dry-run` verified a source; without `--dry-run` its email is recorded | `true` | `true` |
| `reused` | the email was recorded on the selected context | `true` | `true` |
| `unchanged` | the selected context already has an email; nothing was read | `false` | `false` |
| `unavailable` | no other context on this server has an email the stored password authenticates | `false` | `false` |

`source_context` names the context the email comes from and appears only when
one was selected. `reason` explains `unchanged` and `unavailable`:

```json
{
  "changed": false,
  "context": "plain",
  "dry_run": true,
  "reason": "no matching stored identity can be reused",
  "state": "unavailable",
  "verified": false
}
```

Both are ordinary results with exit 0, and neither is evidence that the
context can authenticate — keep the `auth status` check. After `unavailable`,
continue with `auth guide` and `auth login`.

What reuse does not do:

- It copies, moves and re-saves no password. There is still one stored
  password per server.
- It never replaces an email a context already has, and it changes no other
  context and not the current context.
- It reads nothing from `WECOM_CALENDAR_USERNAME` or `WECOM_CALENDAR_PASSWORD`.
- It does not switch the scheme. It may adopt the source's spelling of the
  server URL, when the two spellings are stored under different keys.

A source whose email the server rejects (HTTP 401) is skipped, not reported.
One stored password belongs to one WeCom account, so with two personal
contexts on a server only one of them verifies, and reuse picks that one.

The preview is read-only. Applying it changes one field of the user's config,
the selected context's email, so apply it when the task needs that context to
authenticate, and say that you did.

Failures leave the config as it was, and none of them calls for a password:

- **`AUTH_REUSE_AMBIGUOUS`** (conflict, 11) — more than one email verified.
  `details.contexts` names the candidates. List them with
  `config get-contexts`, ask the user which account the context should use
  when it is not evident, and repeat with `--from-context <name>`.
- **`AUTH_REUSE_SOURCE_NOT_FOUND`** (not_found, 6) /
  **`AUTH_REUSE_SOURCE_MISMATCH`** (conflict, 11) — `--from-context` names a
  context that does not exist, or one on another server or scheme. Pick a name
  from `config get-contexts`.
- **`AUTH_REUSE_TARGET_MISSING`** (config, 3) — the selected context is not in
  the config file. Create it with `config set-context <name>` and select it
  with `--use-context`.
- **`AUTH_REUSE_TARGET_MISMATCH`** (conflict, 11) — `--base-url`,
  `--auth-scheme` or their environment variables select another service than
  the context stores. Drop the override.
- **`AUTH_REUSE_CONFIG_CHANGED`** / **`AUTH_REUSE_CREDENTIAL_CHANGED`**
  (conflict, 11) — the config file or the stored password changed while the
  login was being verified. Nothing was saved; preview again.
- **`AUTH_REUSE_WRITE_FAILED`** (config, 3) — the login verified, but the
  config file could not be written. Fix access to the config directory and run
  `auth reuse` again.
- Network, permission and credential-store failures keep their own errors.
  `CREDENTIAL_STORE_INACCESSIBLE` and `CREDENTIAL_NOT_VISIBLE_OR_MISSING` are a
  host retry here as everywhere; do not move on to `auth login`.

## Personal login — `auth login`

`auth login` reuses the resolved service, prints the guide on stderr, asks for
the WeCom email only when none is configured, and reads the password without
echoing it. It verifies the credential with an authenticated request to the
calendar home — the check `doctor` runs — before saving anything, then stores
the password in the secure store and the email and scheme in the config file.
A later process resolves the same credential without another prompt. A service
that came only from the environment becomes a `default` context if none exists.

```json
{
  "credential_backend": "keychain",
  "scheme": "basic",
  "server": "https://caldav.wecom.work",
  "status": "stored"
}
```

`credential_backend` is `keychain`, or `file` when no OS keychain is available.

It needs a terminal. Without one it returns `AUTH_LOGIN_NEEDS_TTY` (config,
exit 3), so an agent asks the user to run it in their own terminal. Do not
collect the password through chat, and do not pass it in command arguments. A
non-interactive environment supplies `WECOM_CALENDAR_USERNAME` and
`WECOM_CALENDAR_PASSWORD` instead; they stay transient and are never copied
into a preset.

Failures say what was stored:

- **`CONTEXT_BASE_URL_MISMATCH`** (config, 3) — the service URL in effect
  differs from the selected context's, usually through `--base-url` or
  `WECOM_CALENDAR_SERVER`. The complete URL is compared, path included, before
  anything is verified or stored. Select or create a matching context; this is
  not a password problem.
- **`CALDAV_AUTH`** (auth, 4) — the server rejected the email and password.
  Nothing was stored. Check the email and re-enter the password before
  concluding that a new one is needed.
- **`CREDENTIAL_SAVE_FAILED`** (config, 3) — the server accepted the password,
  but it could not be stored. Fix access to the keychain or the config
  directory and run `auth login` again with the **same** password.
- **`LOGIN_CONFIG_WRITE_FAILED`** (config, 3) — the password was stored, but
  the email and scheme could not be written. `details` holds `context`,
  `server`, `scheme` and `credential_stored: true`. Fix config-directory access
  and run `auth login` again with the **same** password.

The config file is replaced atomically, so a failed write leaves the previous
configuration intact.

## Environment-only presets

Service variables can be injected by the user's shell, a launcher, or CI
instead of a config file:

```bash
export WECOM_CALENDAR_SERVER=https://caldav.wecom.work/
export WECOM_CALENDAR_AUTH_SCHEME=basic
export WECOM_CALENDAR_CREDENTIAL_URL=https://wiki.example.com/wecom-caldav
wecom-calendar-cli auth guide
```

`--auth-scheme` and `--credential-url` override these variables. A page set
this way is reported with `"source": "env"`. See the
[README](https://github.com/AngelMsger/wecom-calendar-cli#team-setup-and-personal-login)
for the distribution examples and the full recovery contract.
