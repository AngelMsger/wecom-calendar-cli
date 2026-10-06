# Agent Guide — wecom-calendar-cli

This project is a member of the `oa-cli` agent-facing CLI family and mirrors its
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

The project was archived from 2026-08-19 to 2026-10-05 on the assumption that
the official CLI replaced it, and reinstated once daily use exposed the history
limit. Overlap with the official CLI is therefore not, by itself, a reason to
stop maintaining this one.

## Documented domain divergence — a local store

The sibling CLIs are stateless request/response wrappers over a remote API. This
one is **stateful**: it maintains a local SQLite store. That is the product
requirement (incremental, idempotent history you can query offline and annotate),
and it is the single intentional departure from the family shape.

Consequences and rules:

- Standard family layers still apply:
  `pkg/{constants,errors,transport,caldav,timeutil}` and
  `internal/{app,output,config,auth,update}` mirror jira-cli. `pkg/caldav` is
  this project's `apiclient` analog; `pkg/timeutil` is the window parser
  `prometheus-cli` carries under the same name.
- The extra layer lives in `internal/{store,sync,ical,expand,meta}`. `sync` is
  the only writer to raw-fact/derived tables; `expand` (recurrence) is a pure
  rebuild from stored data.
- **`event_metadata` is an agent-owned layer that `sync`/`expand` must never
  write or delete.** Re-syncing, soft-deleting, or rebuilding instances must
  leave every metadata row intact (covered by a store test). Classification and
  external-task links are just conventional namespaces/keys in this layer; the
  schema hard-codes no specific tool.
- The database lives next to `config.yaml` at `<config_dir>/calendar.db` and
  moves with `--config`. It may contain personal calendar data — never commit
  it (`.gitignore` covers `*.db*`).

## The WeCom / Tencent CalDAV backend (non-standard — read before touching pkg/caldav)

Empirically verified; generic CalDAV libraries fail here:

- Calendar-home is the **bare `/calendar/`** collection (Basic-auth identity
  selects whose calendars). Root `/`, `.well-known`, `/principals/` all 403/404.
- `PROPFIND /calendar/ Depth:1` lists calendars at `/calendar/<id>/` with a
  calendarserver `getctag`.
- `REPORT calendar-query` + time-range lists event `.ics` hrefs + etags.
  Inline `calendar-data` comes back empty; `calendar-multiget` returns 403.
- Each event body is fetched with a plain `GET` on its `.ics` href.
- TZIDs are non-IANA (e.g. `TZ08`) but every event embeds a VTIMEZONE defining
  them — `internal/ical` resolves the embedded offset itself because go-ical's
  `DateTime` hard-fails on such TZIDs.
- Incremental sync keys off the per-calendar `getctag`; within a scanned
  calendar it skips resources whose `getetag` (the one from `calendar-query`,
  **not** the GET `ETag` header — they differ here) is unchanged.
- The server lists resources it then 404s, and some bodies won't parse. These
  are recorded in `calendar_resource_failures` by `getetag` and skipped, so a
  permanently-broken resource does **not** withhold its calendar's `getctag` and
  force a perpetual full re-scan. `--full` re-attempts them.

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

### The CLI and Skill upgrade loop

Ported from `jira-cli` unchanged in shape. `WECOM_CALENDAR_CLI_SKILL` carries
the loaded Skill's version, not a boolean: the runtime notice classifies it as
`not_loaded`, `outdated`, `unknown` (the legacy value `1`) or `current`, and is
silent only for `current`. `skill status` compares the loaded, installed and
embedded versions and returns ordered `next_steps`; `skill path` and
`skill install` report each copy's `alignment`. `doctor` adds an informational
`companion-skill` check that never changes `healthy`. Update notices list three
ordered steps: upgrade the CLI, run `skill install`, reload the agent context.
The npm banner repeats them as a secondary hint only, and `postinstall` never
writes into an agent directory.

The Skill tells agents to export the literal version, so **bump the `version:`
frontmatter and the handshake value in the Skill's "Agent handshake" section
together**. `scripts/e2e.sh` reads the version from the frontmatter and fails
when the handshake sentence names another one.

### Write outcomes

The family rule — reconcile an uncertain write before replaying it, and keep
the resource identity when a read fails after a successful write — was written
for remote writes. `meta set` and `meta delete` are the only writes here, and
their path differs in three ways that decide what applies:

- **Nothing is read back.** The result is built from the caller's own
  `(uid, namespace, key)` and the statement's row count, so there is no
  post-write read to fail and no identity to lose. The one read on the path,
  `EventExists`, runs before the statement and aborts the command when it fails.
- **No outcome is uncertain.** Each write is one autocommit SQLite statement on
  a local file. A statement that returns an error did not apply, and no network
  sits between the CLI and the store, so the "outcome unknown, verify before
  retrying" class does not exist. `pkg/errors` therefore keeps the plain retry
  guidance for `rate_limit`, `server` and `network`: only requests to the WeCom
  server raise them, and every one of those is a read.
- **Printing the result is the step that can fail after the commit.** Output
  options are validated while rendering, as in the siblings, so an unknown
  `--format` used to commit the row and then exit as a bare usage error. That
  is this CLI's analogue of the rule: `emitAfterWrite`
  (`internal/app/write_errors.go`) turns it into
  `WRITE_SUCCEEDED_OUTPUT_FAILED`, which keeps the cause's category and exit
  code, names the entry, sets `retryable: false` and points at `meta get`. A
  delete that matched nothing changed nothing and keeps the plain error.

Keep every check and read ahead of the mutating statement, and route any step
added after it through `emitAfterWrite`. `meta set` is last-write-wins with no
version check, so there is no conflict to merge in code; the Skill carries the
read-merge-write rule instead. `READONLY_BLOCKED` lists the family's three
override steps, and the Skill tells agents not to act on them without
authorization.

### Time windows

`event list` uses the family's time-window vocabulary (shared standard §3.5):
`--since <duration>` is a look-back ending now, `--from` an inclusive lower
bound and `--to` an exclusive upper bound, so the interval is `[from, to)`.
`--since` excludes `--from`/`--to`, `--to` requires `--from`, and an unusable
window is `BAD_TIME_RANGE` (usage) with a hint that restates the rules and
runnable `next_steps`. Parsing lives in `pkg/timeutil`, the lean form of the
package `prometheus-cli` and `openobserve-cli` carry; the flag wiring is
`internal/app/timeflags.go`, after `prometheus-cli`'s file of that name.

A calendar looks ahead, which the siblings' event and history filters never
do. Four differences follow from that. Each is intentional, and each has a test
in `internal/app/timeflags_test.go`:

- **The default window is 30 days either side of today**, not a window ending
  now: from midnight 30 days ago to the end of the day 30 days ahead, in the
  display timezone (`TestDefaultEventWindowIsStableForTheDay`). The bounds sit
  on local midnight so that every call made on one day resolves the same
  window. They used to follow the current millisecond, which made the `next`
  cursor of every default-window page fail with `CURSOR_MISMATCH`
  (`TestEventListDefaultWindowCursorResumes`).
- **A window may reach into the future.** `--to` may be later than now,
  `now+14d` is a valid instant, and `--from` without `--to` ends at the default
  upper bound 30 days ahead instead of at now
  (`TestEventWindowReachesIntoTheFuture`). For the same reason a bare duration
  such as `--from 7d` is rejected instead of being read as "7 days ago", as the
  sibling parsers read it: the direction has to be written, `now-7d` or
  `now+7d` (`TestParseInstantRejectsABareDuration` in `pkg/timeutil`).
- **A date-only value is midnight in the display timezone**, not UTC, because
  calendar days are local. An RFC 3339 instant with an offset is exact
  (`TestEventWindowDateIsALocalDay`).
- **There is no `--actor`.** The store holds one account's calendars; `whoami`
  and each attendee's `is_self` cover identity
  (`TestEventListHasNoActorFlag`). Do not add an actor filter without
  revisiting this.

`pkg/timeutil` also leaves out what only a metrics backend needs: epoch
integers, the space-separated UTC timestamp, the `y` unit and the step and
rendering helpers. Epochs are omitted on purpose — this CLI prints RFC 3339
only, and a compact date such as `20260701` would otherwise be read as epoch
seconds and silently select 1970.

**Paging needs fixed bounds.** A cursor is bound to its resolved window by a
digest. A window relative to the current time — `--since <duration>`, or a
bound written as `now±duration` — resolves to different bounds on every call,
so `--cursor` with it is rejected with `CURSOR_RELATIVE_WINDOW`, and the error
restates the window as absolute `--from`/`--to`. `--all` resolves a relative
window once. This follows `bitbucket-cli`'s activity filter, which resolves a
windowed query once and reads it in a single invocation, and rejects a cursor
it cannot attribute instead of guessing. The default bounds are stable for the
day, so they remain pageable; `CURSOR_MISMATCH` tells a caller who pages past
midnight to pass absolute bounds (`TestEventListCursorNeedsFixedWindow`).

**`expand` is not an event filter.** It pins the window recurring events are
expanded over, so it takes `--from`/`--to` only, lets either bound default on
its own, and refuses a look-back `--since <duration>`: a window ending now
would silently drop every future occurrence from the store
(`TestExpandWindow`, `TestExpandRejectsLookBackSince`).

**Deprecated aliases.** Before this contract the flags were `--since
YYYY-MM-DD` and `--until YYYY-MM-DD`. Both remain, on `event list` and
`expand`, as aliases of `--from` and `--to`, so no existing invocation breaks —
including `--until` on its own, which keeps its default lower bound although
the canonical `--to` requires `--from`. Each use prints one
`{"_notice":{"deprecated_flag":{…}}}` line on stderr with the `flag`, its
`replacement`, a `message` and how to `silence` it
(`WECOM_CALENDAR_CLI_NO_DEPRECATION_NOTICE=1`). The family had no
deprecated-flag notice to reuse, so the shape follows the shared standard's
§4.3 (`TestEventWindowDeprecatedAliases`, `TestDeprecationNoticeShape`,
`TestDeprecatedFlagNoticesOnStderr`). `BAD_DATE` and `BAD_WINDOW`, the codes
the old flags raised, are replaced by `BAD_TIME_RANGE`.

When a command gains a time filter, wire it through `timeFlags` rather than
parsing dates locally, and decide explicitly whether its window can be paged.

### NDJSON continuation

Ported from `bitbucket-cli` unchanged: `internal/output/output.go` is identical
to the sibling's apart from the import path. With `--format ndjson`, stdout
carries item records only; after every row is written, a page with `has_more`
prints one compact stderr line,
`{"_notice":{"pagination":{"next":…,"has_more":true},"next_steps":[…]}}`.
`Options.NoticeWriter` makes the destination injectable and `Options.NextFlag`
names the continuation flag, defaulting to `--cursor`, which the table footer
uses too. Field projection does not drop the notice, an empty page that still
has a token keeps it, and a failed stdout write advertises nothing.

`event list` is the only paginated command and passes its page descriptor to
`emitList`. `calendar list`, `meta get` and `meta list` return the whole set
and pass an empty descriptor on purpose: there is no cursor to preserve, and
none is fabricated. A command that starts paginating must pass its `pageInfo`
through, or the notice is lost before the renderer sees it.

Covered by `internal/output/pagination_test.go` (projection, an empty page
with a token, completed and unpaginated results, write failures, a custom
flag) and by `TestEventListNDJSONContinuation`, which reads the notice and
resumes through the real command. `scripts/e2e.sh` does the same against the
binary. No command creates events offline, so it seeds synthetic occurrences
with `test/seedstore` — `event list` reads only the derived `event_instances`
table — instead of starting a mock server.

### Team service presets and personal login

Ported from `jira-cli` (`bebf7e0`) file by file, and cross-checked against
`bitbucket-cli` (`fd3607f`). `internal/config/setup.go` owns offline
service-field validation, the acquisition guide and the pure
`PlanServiceContext` merge that execution and `--dry-run` share.
`LoadOptions.Setup` selects the requested destination even when it is new,
ignores runtime context selection, and drops personal environment fields before
a password can imply the scheme. `AuthConfig.CredentialURL` is additive,
non-secret and serialized through every config shape; `WriteFile` replaces the
file through a temporary file; error payloads may carry non-secret `details`.
`internal/app/setup.go` wires `config set-context`, `auth guide` and the wizard
prefill. `internal/app/auth_login.go` checks the complete normalized service
URL, verifies, stores the secret and then persists the identity, naming what
was stored when a step fails. `internal/app/prompt.go` holds the terminal
prompts.

The siblings keep the user-facing contract in `docs/installation.md` and the
design notes in `docs/technical-design.md`. This project has neither file: the
canonical user-facing text is the README section "Team setup and personal
login", and the design notes are this section.

Keep service presets separate from personal credentials. The service fields
are `server`, `auth.scheme` and `auth.credential_url`; the WeCom email and the
CalDAV password are personal and never enter a preset. `config set-context`
must resolve the named destination, stay offline and credential-store-free, and
preserve other contexts, usernames and the shared defaults. Login must persist
its identity as well as its secret, and reject a different complete service URL
before any credential write. Use the one guide in both wizards, in login and in
missing-credential recovery; never request its URL. When changing this flow,
cover a fresh config reload, conflict and idempotent setup, and each partial
persistence failure.

The differences below are intentional. Each follows from the domain and has a
test:

- **There is no web credential page.** WeCom issues the CalDAV password in its
  mobile app: Workbench → Calendar → settings → "Sync to other calendars".
  `Guide` returns those steps as `instructions`, leaves `credential_url` empty
  and reports `source: builtin`. It has no built-in or fallback URL and no
  `flavor`. A team may configure a display-only page, which then fills
  `credential_url` and names its layer in `source`; the "Credential page" line
  is printed only then. `documentation_url` is the README section, because
  WeCom publishes no page about this password that the project has verified.
  `TestCredentialGuideHasNoURLUnlessATeamConfiguresOne`,
  `TestCredentialGuideJoinsAbsenceErrorsOnly`.
- **A new password invalidates the previous one.** Every calendar client still
  using the old password stops syncing, so "acquire a credential" is a costly
  recovery here. The guide, the login prompt and both partial-persistence
  errors say so; `CREDENTIAL_SAVE_FAILED` and `LOGIN_CONFIG_WRITE_FAILED` tell
  the user to enter the same password again. "The credential store is
  inaccessible" stays strictly apart from "acquire a new credential":
  `WithCredentialGuide` adds `auth guide` to `AUTH_NO_BASIC`, and to
  `CREDENTIAL_NOT_VISIBLE_OR_MISSING` after its host retry, and never to
  `CREDENTIAL_STORE_INACCESSIBLE`, whose steps forbid issuing a password. An
  agent that re-issues one because a sandbox could not read the keychain breaks
  the user's other calendar clients.
  `TestInaccessibleStoreNeverSuggestsAcquiringACredential`,
  `TestMissingCredentialPointsAtTheGuideAfterTheHostRetry`,
  `TestLoginPersistenceFailuresAreDistinguishable`.
- **There is one auth scheme.** The plumbing is kept so the shape matches —
  `--auth-scheme`, `WECOM_CALENDAR_AUTH_SCHEME`, `auth.scheme`,
  `ValidateService` — and `basic` is the only value it accepts.
  `resolveAuthDefaults` is the empty hook `prometheus-cli` also keeps; there is
  no scheme prompt, no `AUTH_NO_TOKEN` and no `promptChoice`. Because the value
  cannot show whether a password implied the scheme, the exclusion test asserts
  on its source. `TestOnlyTheBasicSchemeIsAccepted`,
  `TestSetupTargetsNamedContextAndExcludesPersonalEnvironment`.
- **The server URL has a default.** The default layer supplies the public
  endpoint, so `config set-context <name>` is valid without `--base-url` and
  writes it in normalized form, and `NO_BASE_URL` cannot be reached through the
  loader. `TestPresetDefaultsToThePublicEndpointAndCarriesNoIdentity`.
- **Verification has no identity to read back.** `verifyCredential` is the
  `Ping` that `doctor` and `config init` already run, one authenticated
  PROPFIND on the calendar-home. The server answers an anonymous PROPFIND with
  401 and serves no principal resource, so the 207 is the evidence and
  `AUTH_IDENTITY_UNAVAILABLE` is not ported. Any other answer fails.
  `TestLoginStoresNothingWhenTheServerRejectsThePassword`,
  `TestVerifyLoginRejectsResponsesThatAreNotCalDAV`,
  `TestLoginAgainstTheStubResolvesInAFreshProcess`.
- **The URL's path is compared but not routed.** `NormalizeServiceURL` and the
  stored context keep a path, and login compares complete URLs as the siblings
  do. `pkg/caldav` derives only the origin from the base URL and addresses the
  calendar-home at `/calendar/` on it, so the path never reaches a request. The
  comparison is therefore stricter than routing: a path-only difference is
  refused with `CONTEXT_BASE_URL_MISMATCH` instead of being treated as the same
  service. Routing by path would be a `pkg/caldav` change, outside this
  contract. `TestLoginRejectsChangedDeploymentPathBeforeAnyIO`,
  `TestLoginVerifiesAtTheCalendarHomeOfTheOrigin`.
- **The plain wizard keeps its scripted flow.** `config init` gains the prefill
  and the guide and is otherwise unchanged. Where the reference driver fails
  any prompt at end of input, this one still takes a prompt's default there, so
  a script that stops after the password keeps working; only a required prompt
  without a default returns the error, where it used to re-prompt forever. The
  section header is printed under the same conditions as before.
  `internal/config/wizard_test.go`.
- **Credential cleanup spares a secret that is still in use.** The keychain
  account is `<host>:<scheme>`, as in the siblings. There a context usually has
  a host of its own; here nearly every context points at the one public
  endpoint, so a preset and a personal context share a stored password, and a
  preset's normalized URL differs in spelling from the wizard default. The
  reference, at the commit this was ported from, forgot the old secret
  whenever an edited context's URL string changed, and on every
  `delete-context`. That would delete the password in use and force a new one,
  so `forgetUnusedCredential` forgets a secret only when no remaining context
  resolves the same account, and `delete-context` does so after its config
  write. The family has since adopted this rule (`jira-cli` `ec08d63`, shared
  standard §6.5), so it is no longer a difference.
  `internal/app/config_credentials_test.go`.

Contexts do not partition credentials or data, and this port did not change
that: the keychain account ignores the context and the username, and the store
belongs to the config directory. Two contexts for different WeCom accounts on
the same server overwrite each other's stored password.

`scripts/e2e-setup.sh`, run at the end of `scripts/e2e.sh`, is the reference
script with the WeCom checks added: the default endpoint, the guide without a
URL, the scheme, the two guards `auth login` applies before it prompts, and
the `auth reuse` paths described below. It runs with an empty environment and a
scratch `HOME`. `auth login` needs a terminal, so its verified path is covered
by unit tests against an `httptest` stub rather than by the shell suite.

### Reusing an existing login

Ported from `jira-cli` (`a918044`) with its follow-ups `ec08d63` and `3ad27a6`,
and cross-checked against `prometheus-cli`, the sibling without deployment
flavors. This is the shared standard's §6.5. `internal/app/auth_reuse.go` holds
the command and `reuseAuthentication`, whose `reuseServices` keep resolution,
verification and the config read and write independently testable.
`internal/config/stored_context.go` holds `StoredContext`, which resolves a
context from the file and the built-in defaults only, the service match
`SameService`, the candidate list `ReuseSources`, and the two loader hooks.
`auth.CredentialLookupURL` and `auth.ForgetForConfig` keep the stored lookup
key under an equivalent URL override.

**What reuse adds here.** The keychain account is `<host>:<scheme>`, so
contexts on one server already share one stored password; that limitation is
recorded above and this port does not change the key. A team preset beside a
personal context therefore finds the password and lacks only the WeCom email,
and every command there fails with `AUTH_NO_BASIC`. `auth reuse` closes exactly
that gap: it verifies the stored password with another context's email and
records that email on the destination. It never saves, copies or moves a
secret — `reuseServices` has no save. Besides the email it writes the scheme
the credential resolved with, which is always `basic`, and it re-spells the
destination's server URL like the source's when the two spellings resolve
different store keys. Say this plainly wherever the command is described;
"reuse a credential" would promise more than it does.

Keep `auth reuse` separate from `config set-context`, which stays offline and
credential-free. Match the complete normalized URL and the scheme before any
credential access, preserve an email the destination already has, verify
through `verifyCredential` — the `Ping` that `auth login`, `config init` and
`doctor` run — before recording anything, and return network, permission and
credential-store failures as they are. Never copy a secret, take an identity
from the environment, switch the scheme or activate a context. Re-read the
config file and re-resolve the source credential immediately before the write,
and stop when either changed. The result is `context`, `state` (`available`,
`reused`, `unchanged`, `unavailable`), `changed`, `verified`, `dry_run`, and
`source_context` or `reason`. Read-only mode blocks only the metadata writes,
so reuse, like login, is not gated by it. When changing this flow, cover
dry-run, ambiguity, scope mismatch, a preserved destination identity,
concurrent config edits, credential rotation, a fresh-load resolution and each
operational failure.

Recovery steps must name commands that exist in this CLI. The context listing
is `config get-contexts` here, not the `config contexts` some siblings use.
`assertRunnableStep` resolves a step against the command tree, flags and
arguments included; run every advertised step of a new error through it.

The differences below are intentional. Each follows from the domain and has a
test:

- **The provider scope is the service URL alone.** There is one scheme and no
  deployment flavor, organization or tenant, so `SameService` compares the
  complete normalized URL and the scheme and nothing else. `reuseFlavor`,
  `reuseVerificationConfig` and the flavor cases of the reference tests have no
  subject. The reference's branch for the `none` scheme has none either: a
  destination stored with any scheme but `basic` is a configuration error, and
  `ValidateService` reports it before a source is matched or a secret read.
  `TestSameServiceComparesTheCompleteURLAndTheScheme`,
  `TestAuthReuseMatchesCompleteServiceBeforeCredentialAccess`,
  `TestAuthReuseRefusesAnUnsupportedSchemeBeforeCredentialAccess`.
- **Only a context with an email can be a source.** Basic authentication needs
  the email, so a context without one has no identity to offer. The reference
  still resolves such a source when its store key differs, because a token
  identifies an account on its own; here `ReuseSources` drops it before any
  credential is read. For the same reason the reference's check for a
  destination that already works without a username is not ported, nor is
  `TestAuthReusePreservesWorkingTokenWithoutUsername`: a destination without an
  email cannot authenticate. Of the skippable "no credential" codes only
  `AUTH_NO_BASIC` exists. A match always changes the destination, so the
  reference's "matched but unchanged" branch is gone too.
  `TestAuthReuseIgnoresContextsWithoutAnIdentity`,
  `TestReuseSourcesAreSameServiceContextsThatCarryAnIdentity`.
- **Verification selects the identity.** One stored password belongs to one
  WeCom account. With two personal contexts on a server, the one whose email
  the server rejects (401) is skipped and the other is recorded, so there is
  normally nothing to choose. `AUTH_REUSE_AMBIGUOUS` remains for the cases
  that can still produce two verified identities: URL spellings that keep
  passwords under different keys, or a server that accepts one password for
  two emails. An email is compared the way `whoami` and `is_self` compare it,
  so two spellings of one address are one identity; the spelling recorded is
  the one that was verified.
  `TestAuthReusePicksTheIdentityTheSharedPasswordBelongsTo`,
  `TestAuthReuseAmbiguityAndExplicitSelection`,
  `TestAuthReuseTreatsEmailCaseAsOneIdentity`.
- **Verification has no identity to read back.** As with login, the 207 from
  the calendar home is the evidence, and `AUTH_IDENTITY_UNAVAILABLE` is not
  ported. The reference test for that code becomes its analogue: an answer
  that is not CalDAV is an error the caller sees, never an `unavailable`.
  `TestAuthReuseDoesNotHideAnAnswerThatIsNotCalDAV`,
  `TestAuthReuseCommandVerifiesNativeStoredCredential`.
- **The server URL has a default.** A destination or a source may store no
  URL and then resolves the public endpoint. `AUTH_REUSE_TARGET_MISSING`
  therefore means only that the context is not in the file, its recovery is
  `config set-context <name>` without `--base-url`, and store keys are compared
  on resolved URLs so a source without a stored URL cannot blank the
  destination's. `TestAuthReuseFollowsTheDefaultEndpoint`.
- **Reuse is offered before a password.** This is the one addition the
  reference does not have, and the reason is the cost of a new password. The
  loader sets the runtime-only `Config.MayReuse` when the context in effect
  has no email, the runtime supplies none, no service override differs from
  the stored context, and `ReuseSources` is not empty. It reads only the file.
  Where it is set, `auth reuse --dry-run` goes first in the `next_steps` and
  `instructions` of `auth guide`, first in `AUTH_NO_BASIC` with a hint that
  explains it, after the host retry and ahead of the login step in
  `CREDENTIAL_NOT_VISIBLE_OR_MISSING`, and ahead of "configure credentials" in
  an unhealthy `doctor`. `PlanServiceContext` applies the same test to the
  planned file. `CREDENTIAL_STORE_INACCESSIBLE` never offers it: reuse reads
  the same store. Be honest about the middle case: with one shared key, a
  password that is missing for the destination is missing for the source, and
  reuse then reports the same error; it helps there only when another spelling
  of the server keeps a password under a key of its own.
  `TestLoaderOffersReuseOnlyWhereItCouldCompleteTheContext`,
  `TestGuideLeadsWithReuseOnlyWhenOffered`,
  `TestPresetLeadsWithReuseWhenAnotherContextHasAnIdentity`,
  `TestMissingIdentityOffersReuseFirst`,
  `TestMissingCredentialOffersReuseAfterTheHostRetry`,
  `TestInaccessibleStoreNeverSuggestsAcquiringACredential`,
  `TestAuthReuseIsOfferedBeforeAcquiringAPassword`,
  `TestAuthReuseKeepsInaccessibleStoreRecoveryOnTheHost`.
- **Every reuse error carries its own hint and steps.** The category defaults
  in `pkg/errors` point at `config init`, `auth login` and `sync`, any of which
  is wrong for a selection or concurrency failure and two of which lead to a
  password. Each `AUTH_REUSE_*` error and `CREDENTIAL_SERVICE_MISMATCH`
  therefore sets both, and says that no password should be issued.
  `TestAuthReuseRecoveryNamesRealCommands`.
- **The shell suite stops at the credential store.** The reference added no
  end-to-end step for reuse. `scripts/e2e-setup.sh` covers what needs neither
  a server nor a credential: the no-change results, the four guards, the
  context listing they advertise, and the offer in a preset and in the guide.
  A verified preview and the write run against an `httptest` stub and the
  mocked keyring instead. Never add a shell step that runs `auth reuse` where
  a source has an email: it would read the OS keychain under the scratch
  `HOME`.

The siblings document this contract in `docs/installation.md` and
`docs/technical-design.md`. Here the user-facing text is the README section
"Reuse an existing login" and the design notes are this section.

### Companion Skill rules

`skills/wecom-calendar/references/working-with-the-user.md` is the single home
for the collaboration rules — reuse verified context, continue within existing
authorization, bounded reads and no polling, preserving annotations, and
evidence-first reporting. `SKILL.md` summarizes them and the topical references
link there instead of restating them. Shell examples and the JSON shown beside
them must match the binary: `scripts/e2e.sh` runs the write and recovery
sequences the Skill describes, so extend it when an example changes.

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

Actively maintained.

Implemented and verified against the live WeCom server: config/auth/doctor,
sync (incremental + idempotent, ctag + etag), recurrence expansion into
`event_instances` with cross-calendar dedup (`internal/expand`), the
agent-owned metadata layer, companion Skill, generated CLI docs, update-notice,
CI (gofmt/vet/unit tests/`scripts/e2e.sh`/docs-drift on Linux + Windows
runtime), and npm distribution.

The team presets, `auth guide`, `auth reuse` and the persistence steps of
`auth login` are verified with an `httptest` stub and the offline e2e suite,
not against the live server. The live check `auth login` and `auth reuse` run
is the same `Ping` as `doctor`.

**Alignment.** The shared contracts the family adopted while this project was
archived are all ported or recorded as intentional differences. The first pass
after the reinstatement overlooked one of them, `auth reuse`, which all six
siblings gained on 2026-09-24; it was ported on 2026-10-06. To check this
claim again, list each sibling's commits from 2026-08-19 to 2026-10-05 and
match every shared contract against a section of this file, rather than
starting from the sections already here. The rule on replying to human-authored
comments, adopted in the same period, has nothing to apply to: this CLI has no
comment surface.

**Ported, with intentional differences.** The CLI and Skill upgrade loop, the
Skill collaboration and write-recovery rules, the time-window contract, NDJSON
continuation metadata, team service presets with personal login, and reuse of
an existing login are in place; the sections above describe them. They differ
from the siblings only where the domain does:

- The time-window contract keeps the family vocabulary and rules, with the
  four calendar differences, the pageability rule, the `expand` exception and
  the deprecated aliases listed under "Time windows". Covered by
  `pkg/timeutil/parse_test.go`, `internal/app/timeflags_test.go` and the
  "time-window contract" block of `scripts/e2e.sh`.
- NDJSON continuation has no difference in the renderer. Only the e2e differs:
  it seeds the store with `test/seedstore` and asserts in the shell script,
  where `bitbucket-cli` drives a mock server from a Python helper.
- Team service presets and personal login keep the family's commands,
  precedence, merge and login persistence, with the eight differences listed
  under "Team service presets and personal login": no web credential page, a
  password that a new one invalidates, one scheme, a default server URL,
  verification without an identity read-back, a path that is compared but not
  routed, the scripted wizard flow, and guarded credential cleanup, which the
  family has since adopted. Covered by
  `internal/config/{setup,file,wizard}_test.go`,
  `internal/auth/resolver_test.go`,
  `internal/app/{auth_login,config_credentials}_test.go` and
  `scripts/e2e-setup.sh`.
- Reuse of an existing login keeps the family's command, flags, result fields,
  states, error codes and write guards, with the eight differences listed
  under "Reusing an existing login": a provider scope that is the service URL
  alone, sources that must carry an email, verification that selects the
  identity, no identity read-back, a default server URL, reuse offered before
  a password, error-specific recovery, and a shell suite that stops at the
  credential store. The flavor, `none`-scheme and token-only parts of the
  reference have no subject. Covered by `internal/app/auth_reuse_test.go`,
  `internal/config/stored_context_test.go`, `internal/auth/resolver_test.go`
  and `scripts/e2e-setup.sh`. The verified path has run only against an
  `httptest` stub, not against the live server.
- `WRITE_SUCCEEDED_READ_FAILED`, `WRITE_SUCCEEDED_RESPONSE_INVALID`, the
  "outcome unknown" wrapper for write failures and the partial-batch error are
  not ported, for the reasons under "Write outcomes".
  `WRITE_SUCCEEDED_OUTPUT_FAILED` takes their place and lives in `internal/app`
  because `pkg/caldav` has no write to wrap. Covered by
  `internal/app/write_errors_test.go` and the "committed writes" block of
  `scripts/e2e.sh`.
- The focused Skill reference is `working-with-the-user.md`, not
  `replying-to-people.md`: the reply protocol has no subject here.
- The e2e suite is offline and has no mock server, so the update-notice check
  builds a version-pinned binary and seeds the release cache instead of
  pointing the release API at a mock.
