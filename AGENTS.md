# Agent Guide — wecom-calendar-cli

This project is a member of the `agent-clis` agent-facing CLI family and mirrors its
siblings (`jira-cli`, `confluence-cli`, …). Read the workspace guide
(`../../AGENTS.md`) and the shared standards under `../../docs/` first. Port the
established shape; do not reinvent cross-cutting contracts.

## What it does

Sync a user's WeCom (Enterprise WeChat) calendars over CalDAV into a local
SQLite store, then serve fast agent-friendly queries over that data, plus a
free-form metadata layer for agent-maintained annotations.

## Relationship to the official wecom-cli

Tencent's official [`wecom-cli`](https://github.com/WecomTeam/wecom-cli)
(`@wecom/cli`) also covers calendars. The two tools are complements, and that
division shapes what belongs here:

- **This CLI owns reading at depth.** CalDAV has no date-range limit, whereas
  the official CLI lists schedules only in a short window around today. Full
  history, offline queries, recurrence expansion and the agent-owned metadata
  layer are the reason this project exists; protect them first.
- **The official CLI owns acting on the calendar.** Creating, updating and
  cancelling events, attendee management and free/busy lookup are done there.
  Do not add calendar writes here: this CLI stays read-only towards WeCom, and
  its only writes are local metadata.
- **Keep the comparison current.** The figures live in three user-facing
  places: the README (its introduction and the section "Alongside the official
  wecom-cli"), the landing page (`docs/index.html`), and the companion Skill.
  Each carries the same "Checked against wecom-cli <version> on <date>" line.
  Re-verify and update all three together when the official limits change;
  other documents link to the README instead of repeating the figures.

Overlap with the official CLI is not, by itself, a reason to stop maintaining
this one; the history limit above is why it exists.

## Where things are documented

Read, as the task needs them:

- [`CONTRIBUTING.md`](CONTRIBUTING.md) — layout, commands, and coding, testing
  and commit conventions.
- [`docs/technical-design.md`](docs/technical-design.md) — the local store, the
  WeCom CalDAV backend, and each contract as it works here, with every
  difference from the siblings and the tests that pin it. Read the section for
  the area you change. Its last section is history, not rules.
- [`README.md`](README.md) — the user-facing contract, including "Team setup
  and personal login" and "Reuse an existing login". The siblings keep this in
  `docs/installation.md`.
- [`docs/releasing.md`](docs/releasing.md) — versioning, tagging and the
  release workflows.

## Ground rules

- Run `make test` and `make e2e` before claiming a change is complete.
- Output rendering, argv correction, the update check, the credential store,
  credential resolution and the configuration engine come from
  [agent-cli-kit](https://github.com/AngelMsger/agent-cli-kit), shared by every
  CLI in the family. Fix bugs in those there, not here.
- The local SQLite store is this project's one structural departure from the
  stateless siblings. `sync` is the only writer to the raw-fact and derived
  tables; `expand` is a pure rebuild from stored data.
- **`event_metadata` is an agent-owned layer that `sync`/`expand` must never
  write or delete.** Re-syncing, soft-deleting, or rebuilding instances must
  leave every metadata row intact (covered by a store test).
- The database at `<config_dir>/calendar.db` may contain personal calendar
  data — never commit it (`.gitignore` covers `*.db*`).
- The WeCom CalDAV server is non-standard and generic CalDAV libraries fail
  against it. Read [the backend notes](docs/technical-design.md#the-wecom-caldav-backend)
  before touching `pkg/caldav`.

## Commands

`sync` · `expand` · `calendar list` · `event list|get` ·
`meta set|get|list|delete` · `whoami` ·
`config init|show|path|set-context|get-contexts|use-context|delete-context` ·
`auth guide|reuse|status|login|logout` · `doctor` ·
`skill status|install|path|show|uninstall` · `version` · `completion`.

Contracts (family baseline): stdout is machine-readable data, notices/errors go
to stderr; lists use the `{items,next,has_more}` envelope; `--format
json|table|ndjson`; errors are structured with `category`/`code`/`hint`. Writes
(`meta set/delete`) honor read-only posture and `--allow-writes`. Read commands
emit a `_notice.stale` on stderr when the store is out of date.

## Rules by area

Each area's design is in the [technical design](docs/technical-design.md).

### The CLI and Skill upgrade loop

Bump the Skill's `version:` frontmatter and the handshake value in its "Agent
handshake" section together; `scripts/e2e.sh` fails when they differ.
`postinstall` never writes into an agent directory.

### Write outcomes

Keep every check and read ahead of the mutating statement, and route any step
added after it through `emitAfterWrite`, so the entry identity and a read-only
recovery step survive. Never describe a write that did not happen as
successful.

### Time windows

When a command gains a time filter, wire it through `timeFlags` rather than
parsing dates locally, and decide explicitly whether its window can be paged.
Do not add an `--actor` filter without revisiting the identity decision
recorded in the design.

### List output and continuation

The renderer is agent-cli-kit's `output` package; change its behavior there. A
command that starts paginating must pass its `pageInfo` to `emitList`, or the
NDJSON continuation notice is lost before the renderer sees it.

### Team setup and personal login

Keep service presets separate from personal credentials: the WeCom email and
the CalDAV password never enter a preset. `config set-context` must resolve the
named destination, stay offline and credential-store-free, and preserve other
contexts, usernames and the shared defaults. Login must persist its identity as
well as its secret, and reject a different complete service URL before any
credential write. Use the one guide in both wizards, in login and in
missing-credential recovery; never request its URL. When changing this flow,
cover a fresh config reload, conflict and idempotent setup, and each partial
persistence failure.

A new CalDAV password invalidates the previous one, so recovery text must
never lead from an unreadable credential store to issuing a new password.

Contexts on the one public endpoint share a stored password. Forget a secret
only through `forgetUnusedCredential`, which keeps it while any remaining
context resolves the same account, and only after the config file is written.

### Reusing an existing login

Keep `auth reuse` separate from `config set-context`, which stays offline and
credential-free. Match the complete normalized URL and the scheme before any
credential access, preserve an email the destination already has, verify
through `verifyCredential` before recording anything, and return network,
permission and credential-store failures as they are. Never copy a secret, take
an identity from the environment, switch the scheme or activate a context.
Re-read the config file and re-resolve the source credential immediately
before the write, and stop when either changed. When changing this flow, cover
dry-run, ambiguity, scope mismatch, a preserved destination identity,
concurrent config edits, credential rotation, a fresh-load resolution and each
operational failure.

Never add a step to `scripts/e2e-setup.sh` that runs `auth reuse` where a
source has an email: it would read the OS keychain under the scratch `HOME`.

### Recovery steps

Recovery steps must name commands that exist in this CLI. The context listing
is `config get-contexts`; build it with `config.ListContextsStep()`, which the
schema's wording defines. `assertRunnableStep` resolves a step against the
command tree, flags and arguments included; run every advertised step of a new
error through it.

### Companion Skill rules

`skills/wecom-calendar/references/working-with-the-user.md` is the single home
for the collaboration rules — reuse verified context, continue within existing
authorization, bounded reads and no polling, preserving annotations, and
evidence-first reporting. `SKILL.md` summarizes them and the topical references
link there instead of restating them. The family's `replying-to-people.md` has
no subject here. Shell examples and the JSON shown beside them must match the
binary: `scripts/e2e.sh` runs the write and recovery sequences the Skill
describes, so extend it when an example changes.

## Build & test

```bash
make build        # -> bin/wecom-calendar-cli
make lint         # gofmt + go vet
make test         # go test ./...
make cross        # cross-compile dist/ for all platforms
```

Go 1.25 (`go.mod`), `CGO_ENABLED=0` (pure-Go SQLite via modernc.org/sqlite).
Version is injected via ldflags into `pkg/constants`.

## Status

Actively maintained. What is verified against the live WeCom server, and what
only against an `httptest` stub, is in
[Verification](docs/technical-design.md#verification).
