# @angelmsger/wecom-calendar-cli

npm distribution of
[`wecom-calendar-cli`](https://github.com/AngelMsger/wecom-calendar-cli)
— a command-line tool that syncs your WeCom (Enterprise WeChat) calendars over
CalDAV into a local SQLite store, then serves fast event queries and a
free-form, agent-owned metadata layer (classification, external task links).
Built for coding agents (Claude Code and others) and humans alike.

It complements Tencent's official
[`wecom-cli`](https://github.com/WecomTeam/wecom-cli): use that to create and
change events, and this to read your whole calendar history offline. The
[project README](https://github.com/AngelMsger/wecom-calendar-cli#alongside-the-official-wecom-cli)
compares the two.

```bash
npm install -g @angelmsger/wecom-calendar-cli
wecom-calendar-cli config init       # CalDAV server URL + WeCom email + app password
wecom-calendar-cli skill install     # deploy the companion agent Skill
wecom-calendar-cli sync              # pull calendars + events into the local store
wecom-calendar-cli event list --from 2026-07-01 --to 2026-08-01
```

Installing this package downloads the prebuilt binary for your platform from the
matching GitHub Release and verifies its SHA-256 checksum. If your npm setup
disables install scripts, the binary is fetched on first run instead.

The companion `wecom-calendar` Skill for coding agents is embedded in the
binary. After installing or upgrading the package, run
`wecom-calendar-cli skill install` and reload the agent context.
`wecom-calendar-cli skill status` reports version alignment.

> **Credential note.** Auth is HTTP Basic: your WeCom email plus an
> **app-specific CalDAV password** obtained in the WeCom mobile app (Workbench →
> Calendar → settings → Sync to other calendars). Fetching a new password there
> invalidates the previous one. `wecom-calendar-cli auth guide` prints these
> steps offline.

For a team, an installer can preset the service with
`wecom-calendar-cli config set-context <name>` — no credentials or network
access needed — and each member then runs `wecom-calendar-cli auth login`. See
[Team setup and personal login](https://github.com/AngelMsger/wecom-calendar-cli#team-setup-and-personal-login).

See the
[project README](https://github.com/AngelMsger/wecom-calendar-cli) for full
documentation.
