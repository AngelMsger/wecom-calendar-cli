# Getting started

Before any command works, `wecom-calendar-cli` needs the CalDAV server URL and
a credential, and — for queries — a synced local store.

## Check the current state

```bash
wecom-calendar-cli doctor
```

`doctor` runs the checks — configuration, credentials, connectivity, and store
freshness — and prints a JSON report. If `healthy` is `true`, you are ready.
Otherwise each failing check's `detail` explains what to fix.

```bash
wecom-calendar-cli auth status   # is a usable credential resolvable?
wecom-calendar-cli config show   # the resolved, non-secret configuration
wecom-calendar-cli config path   # where config.yaml and calendar.db live
```

## Configuration sources

Settings resolve in this precedence order (highest first):

1. CLI flags (`--config`, `--format`, `--use-context`)
2. Environment variables (`WECOM_CALENDAR_*`)
3. A `.env` file in the working directory
4. `~/.angelmsger/wecom-calendar/config.yaml`
5. Built-in defaults

Key environment variables:

| Variable | Meaning |
|----------|---------|
| `WECOM_CALENDAR_SERVER` | CalDAV server root (e.g. `https://caldav.wecom.work/`) |
| `WECOM_CALENDAR_USERNAME` | Your full WeCom email address |
| `WECOM_CALENDAR_PASSWORD` | The app-specific CalDAV password (see below) |
| `WECOM_CALENDAR_FORMAT` | Default output format: `json` / `table` / `ndjson` |
| `WECOM_CALENDAR_CLI_READ_ONLY` | `1` blocks every local write for the session |

## The credential — an app-specific CalDAV password

Auth is HTTP **Basic**: username is your full WeCom email; the secret is a
dedicated **app-specific CalDAV password**, not your normal login password. Get
it in the WeCom **mobile app**:

> Workbench → Calendar → settings → **Sync to other calendars**

That screen issues the CalDAV password. **Fetching a new one invalidates the
previous one**, so if a working setup suddenly returns 401, someone likely
re-issued it — get a fresh password and re-run `config init` / `auth login`.

Never print, log, or echo this password. Store it once; the CLI keeps it in the
OS keychain (with a `0600` file fallback on macOS/Linux, per-user DPAPI on
Windows) — never in `config.yaml`.

## Interactive setup (humans)

For a human at a terminal:

```bash
wecom-calendar-cli config init
```

It asks for the server URL, the WeCom email and the CalDAV password, validates
them live against the server, and stores the secret in the keychain. Non-secret
settings go to `~/.angelmsger/wecom-calendar/config.yaml`.

## First sync

The store starts empty; queries return nothing until you sync:

```bash
wecom-calendar-cli sync              # first sync: pulls all calendars + events
wecom-calendar-cli calendar list     # confirm the calendars landed
wecom-calendar-cli event list --since 2026-07-01 --until 2026-07-31
```

The first `sync` is a full pull; later runs are incremental (see
[syncing.md](syncing.md)). The SQLite database lives next to `config.yaml` at
`<config_dir>/calendar.db` and moves with `--config`; it may hold personal
calendar data, so it is never committed.

## For agents and sandboxes

If you are an AI agent driving the CLI, the user has normally already
configured it. **Reuse their existing config and credentials** — do not run
`config init` to create a fresh setup.

When you run inside a **sandbox** that cannot read the user's home directory or
keychain, credential resolution returns `CREDENTIAL_STORE_INACCESSIBLE` or
`CREDENTIAL_NOT_VISIBLE_OR_MISSING` with `recovery.scope=host`. Do **not** give
up, and do **not** re-initialize config inside the sandbox. Instead:

- **Request elevated permissions** (or otherwise re-run with access to the
  user's real environment), then retry the same command once.
- If the host retry also reports credentials missing, ask the user to run
  `wecom-calendar-cli config init` in their own terminal, or to export
  `WECOM_CALENDAR_*` env vars for the session.

## Multiple servers (contexts)

The config file holds named contexts (kubeconfig-style). Inspect and switch:

```bash
wecom-calendar-cli config get-contexts
wecom-calendar-cli config use-context work
wecom-calendar-cli --use-context personal calendar list   # one-off override
```

Each context has its own store, so calendars from different accounts never mix.
