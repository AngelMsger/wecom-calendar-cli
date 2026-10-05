# Contributing

Thanks for working on `wecom-calendar-cli`. This guide covers the repository
layout, the build and test workflow, and the conventions a change is expected to
follow. For the architecture and the workspace-wide conventions this project
shares with its sibling CLIs, read [`AGENTS.md`](AGENTS.md) first.

This project complements Tencent's official
[`wecom-cli`](https://github.com/WecomTeam/wecom-cli). Contributions that deepen
reading, history, offline querying or annotation fit here. Features that change
the calendar itself — creating, editing or cancelling events — are out of
scope, because the official tool covers them. [`AGENTS.md`](AGENTS.md) records
the boundary.

## Project structure

A Go CLI that syncs WeCom (Enterprise WeChat) calendars over CalDAV into a local
SQLite store and queries them. The entrypoint is `cmd/wecom-calendar-cli/`;
`cmd/gen-docs/` regenerates the CLI reference. The layering is strict
`cmd → internal → pkg` (pkg never imports internal):

- `pkg/` — the importable, stateless client layer: `caldav` (the purpose-built
  CalDAV client for the non-standard Tencent Exmail backend), `transport` (HTTP
  retry, same-origin redirect guard, decorators), `errors` (structured error
  model + exit codes), `timeutil` (the `--since`/`--from`/`--to` window parser),
  `constants`.
- `internal/app` — the Cobra command tree, one noun per file.
- `internal/{config,auth,output,update,cliflags}` — setup, credentials,
  presentation, and agent-facing plumbing, mirrored from the sibling CLIs.
- `internal/{store,sync,ical,expand}` — the **local-store layer**, this
  project's one documented divergence from the stateless siblings: SQLite
  persistence, the CalDAV→store sync orchestrator, iCalendar parsing (including
  embedded-VTIMEZONE / non-IANA TZID resolution), and recurrence expansion.

Tests sit beside the code as `*_test.go`. `test/seedstore/` is a helper the
end-to-end script runs to fill a scratch store. The companion agent Skill is in
`skills/wecom-calendar/`, generated docs in `docs/cli/`, and npm release assets
in `build/npm/`.

## Build, test, and development commands

- `make build` — build `bin/wecom-calendar-cli` with version metadata.
- `make test` — `go test ./...` across all packages.
- `make e2e` — build, then run `scripts/e2e.sh` offline-contract checks
  (read-only, `--dry-run`, the destructive-confirmation gate, the time-window
  flags and their deprecated aliases, cursor handling, NDJSON continuation,
  exit codes, write outcomes, the metadata sequences the Skill documents, and
  the CLI/Skill upgrade loop). No network or credentials required; it needs the
  Go toolchain to build one version-pinned binary and to seed a scratch store
  with `test/seedstore`.
- `make e2e-live` — additionally exercise a real sync; set
  `WECOM_CALENDAR_SERVER` / `WECOM_CALENDAR_USERNAME` / `WECOM_CALENDAR_PASSWORD`
  first.
- `make lint` — `make fmt` (gofmt) and `make vet` (`go vet ./...`).
- `make docs` — regenerate `docs/cli/` from the command tree; CI fails if the
  committed output is stale.
- `make cross` — build release binaries.
- `make tidy` — update `go.mod` / `go.sum`.

## Coding style & conventions

Standard Go formatting; CI requires `gofmt` cleanliness and a clean
`go vet ./...`. New source files use PascalCase for `.go`/`.tsx` basenames where
the sibling projects do. Command behavior stays in `internal/app`; CalDAV
mapping in `pkg/caldav`; user-facing constants in `pkg/constants`. Export
identifiers only when consumed across package boundaries.

Preserve the agent-facing contracts: stdout is machine-readable data, notices
and errors go to stderr, lists use the `{items, next, has_more}` envelope,
errors are structured with documented exit codes, writes support `--dry-run`,
and destructive writes require `--yes` or an interactive confirmation. The
`sync`/`expand` rebuild must never touch the agent-owned `event_metadata` layer.

## Testing guidelines

Use Go's standard `testing` package; name files `*_test.go` and functions
`TestXxx`, beside the package under test. The sync path is covered with a fake
`caldav.Client`; the CalDAV client and transport are covered with fixtures and
`httptest`. Before opening a PR, run `make test` and `make e2e` (and
`make e2e-live` only when real credentials are available).

For a command that filters by time, resolve the window through `timeFlags`
(`internal/app/timeflags.go`) so the flags, defaults and `BAD_TIME_RANGE`
errors stay uniform, and add a case for every way the window differs from the
family contract. For a command that paginates, pass its page descriptor to
`emitList` and extend the seeded pagination block of `scripts/e2e.sh`. Both
contracts are in [`AGENTS.md`](AGENTS.md#time-windows).

For changes to the metadata writes, keep every check and read ahead of the
mutating statement, and report a failure after it through `emitAfterWrite` so
the entry identity and a read-only recovery step survive. Test the
committed-then-failed path together with the resulting store state; never
describe a write that did not happen as successful. The contract is in
[`AGENTS.md`](AGENTS.md#write-outcomes).

## Commits, changelog & versioning

Keep commits scoped to one logical change with concise, imperative messages.
Record any user-facing change under the `[Unreleased]` section of
[`CHANGELOG.md`](CHANGELOG.md) in the same commit. The CLI version is derived
from the git tag via `-ldflags`; bumping is only complete once the commit is
tagged (rename `[Unreleased]` to the new version with today's date, add a fresh
`[Unreleased]`, bump `build/npm/package.json`, commit, then tag).

## Documentation

Treat documentation as part of the change. When a change affects the
architecture, commands, flags, or output model, update the relevant file
(`AGENTS.md`, `README.md`, the companion Skill under `skills/wecom-calendar/`,
and the generated `docs/cli/`) in the same commit. The companion Skill is the
agent's source of truth — keep it exactly in step with the real command tree and
output fields. When it changes, bump its `version:` and the handshake value in
its "Agent handshake" section together.
