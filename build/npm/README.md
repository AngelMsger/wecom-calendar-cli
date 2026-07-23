# @angelmsger/wecom-calendar-cli

npm distribution of
[`wecom-calendar-cli`](https://github.com/AngelMsger/wecom-calendar-cli)
— a command-line tool that syncs your WeCom (Enterprise WeChat) calendars over
CalDAV into a local SQLite store, then serves fast event queries and a
free-form, agent-owned metadata layer (classification, external task links).
Built for coding agents (Claude Code and others) and humans alike.

```bash
npm install -g @angelmsger/wecom-calendar-cli
wecom-calendar-cli config init       # CalDAV server URL + WeCom email + app password
wecom-calendar-cli skill install     # deploy the companion agent Skill
wecom-calendar-cli sync              # pull calendars + events into the local store
wecom-calendar-cli event list --since 2026-07-01 --until 2026-07-31
```

Installing this package downloads the prebuilt binary for your platform from the
matching GitHub Release and verifies its SHA-256 checksum. If your npm setup
disables install scripts, the binary is fetched on first run instead.

The companion `wecom-calendar` Skill for coding agents is embedded in the
binary; `wecom-calendar-cli skill install` deploys a copy that always matches
the installed CLI version.

> **Credential note.** Auth is HTTP Basic: your WeCom email plus an
> **app-specific CalDAV password** obtained in the WeCom mobile app (Workbench →
> Calendar → settings → Sync to other calendars). Fetching a new password there
> invalidates the previous one.

See the
[project README](https://github.com/AngelMsger/wecom-calendar-cli) for full
documentation.
