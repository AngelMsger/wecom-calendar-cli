# wecom-calendar-cli command reference

This index is generated from the CLI command tree — do not edit it by
hand; run `make docs`. The full reference, with every flag and example,
is published at <https://angelmsger.github.io/wecom-calendar-cli/cli/>.

## auth

| Command | Description |
| --- | --- |
| [`wecom-calendar-cli auth`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-auth) | Inspect and manage stored credentials |
| [`wecom-calendar-cli auth login`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-auth-login) | Store a credential for the configured server |
| [`wecom-calendar-cli auth logout`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-auth-logout) | Remove the stored credential for the configured server |
| [`wecom-calendar-cli auth status`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-auth-status) | Show whether a usable credential is configured |

## calendar

| Command | Description |
| --- | --- |
| [`wecom-calendar-cli calendar`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-calendar) | List calendars |
| [`wecom-calendar-cli calendar list`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-calendar-list) | List the calendars in the local store |

## config

| Command | Description |
| --- | --- |
| [`wecom-calendar-cli config`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-config) | Manage wecom-calendar-cli configuration |
| [`wecom-calendar-cli config delete-context`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-config-delete-context) | Delete a context and its stored credential |
| [`wecom-calendar-cli config get-contexts`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-config-get-contexts) | List the configured contexts |
| [`wecom-calendar-cli config init`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-config-init) | Interactively set up the CalDAV server URL and credentials |
| [`wecom-calendar-cli config path`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-config-path) | Print the config file path |
| [`wecom-calendar-cli config show`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-config-show) | Show the resolved configuration |
| [`wecom-calendar-cli config use-context`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-config-use-context) | Switch the current context |

## doctor

| Command | Description |
| --- | --- |
| [`wecom-calendar-cli doctor`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-doctor) | Diagnose configuration, credentials and connectivity |

## event

| Command | Description |
| --- | --- |
| [`wecom-calendar-cli event`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-event) | Query calendar events |
| [`wecom-calendar-cli event list`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-event-list) | List events in a time window from the local store |

## expand

| Command | Description |
| --- | --- |
| [`wecom-calendar-cli expand`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-expand) | Rebuild the expanded event-instances view |

## meta

| Command | Description |
| --- | --- |
| [`wecom-calendar-cli meta`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-meta) | Read and write custom, agent-maintained event metadata |
| [`wecom-calendar-cli meta delete`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-meta-delete) | Delete a metadata entry |
| [`wecom-calendar-cli meta get`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-meta-get) | Get metadata for one event |
| [`wecom-calendar-cli meta list`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-meta-list) | List metadata across events, filtered by uid/namespace/key |
| [`wecom-calendar-cli meta set`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-meta-set) | Set a metadata value on an event |

## skill

| Command | Description |
| --- | --- |
| [`wecom-calendar-cli skill`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-skill) | Install the companion Skill for coding agents (Claude Code, Codex) |
| [`wecom-calendar-cli skill install`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-skill-install) | Deploy the embedded Skill into a coding agent's skills directory |
| [`wecom-calendar-cli skill path`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-skill-path) | Print where the Skill would be installed, and whether it is |
| [`wecom-calendar-cli skill show`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-skill-show) | Print the embedded SKILL.md to stdout |
| [`wecom-calendar-cli skill status`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-skill-status) | Report whether the companion Skill is loaded and installed |
| [`wecom-calendar-cli skill uninstall`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-skill-uninstall) | Remove the companion Skill from a coding agent's skills directory |

## sync

| Command | Description |
| --- | --- |
| [`wecom-calendar-cli sync`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-sync) | Sync WeCom calendars into the local store |

## version

| Command | Description |
| --- | --- |
| [`wecom-calendar-cli version`](https://angelmsger.github.io/wecom-calendar-cli/cli/#wecom-calendar-cli-version) | Print version information |

