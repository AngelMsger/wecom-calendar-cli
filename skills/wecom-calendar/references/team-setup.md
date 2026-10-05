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

# The member completes personal authentication in a terminal.
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

## A new password invalidates the previous one

Issuing a new CalDAV password in the WeCom app invalidates the one before it.
Every calendar client still using the old password — a phone, a desktop
calendar, this CLI on another machine — stops syncing until it is updated. So
a new password is the answer to only two situations, and never to the third:

| Situation | How it shows | What to do |
|-----------|--------------|------------|
| No credential exists | `CREDENTIAL_NOT_VISIBLE_OR_MISSING` on the host too, or `auth status` reports `"configured": false` there | The user runs `auth login`. They reuse the current password if they have it, and issue one only if they do not. |
| The stored password was rejected | `CALDAV_AUTH` (auth, exit 4, HTTP 401) | The user issues a new password, runs `auth login`, and updates their other calendar clients. |
| The credential store cannot be read | `CREDENTIAL_STORE_INACCESSIBLE`, or any error with `recovery.scope=host` | Retry the same command with access to the user's home directory and OS keychain. **Do not** issue a password, run `auth login` or `config init`. |

A sandbox that cannot see the keychain produces the third situation, and the
first two look similar from inside it. Retry on the host before concluding that
a credential is missing.

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
