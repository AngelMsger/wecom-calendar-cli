# Releasing (maintainer guide)

`wecom-calendar-cli` is distributed from **GitHub Releases**. Everything else —
the npm package, `go install`, the `doctor` update check — points back to the
release assets, so a release is the single source of truth.

## Publishing setup

- The repository is public at `github.com/AngelMsger/wecom-calendar-cli`, like
  every other CLI in the family. **It must stay public**: GitHub Pages on a
  private repository requires a paid plan, and on the current plan the API
  rejects enabling it with `422 Your current plan does not support GitHub Pages
  for this repository` while `.github/workflows/pages.yml` fails at
  `actions/configure-pages` with a 404. Flipping the repository back to private
  therefore silently breaks the docs site at
  <https://angelmsger.github.io/wecom-calendar-cli/>, which the README badges,
  `build/npm/package.json`'s `homepage` and the landing page's own canonical URL
  all point at.
- The npm account owns the `@angelmsger` scope; the package is published as
  `@angelmsger/wecom-calendar-cli`.
- **npm publishing uses trusted publishing (OIDC)** — no long-lived token, no
  repository secret. The release workflow grants `id-token: write` and the npm
  CLI authenticates automatically, attesting provenance. Configure the trusted
  publisher on npmjs.com → the `@angelmsger/wecom-calendar-cli` **package** →
  Settings → Trusted Publisher → **GitHub Actions**:

  | Field | Value |
  |-------|-------|
  | Organization or user | `AngelMsger` |
  | Repository | `wecom-calendar-cli` |
  | Workflow filename | `release.yml` |
  | Environment | *(leave blank)* |

  If you see *"There are security risks with this option"* while creating a
  classic automation token — that prompt is steering you here; you do not need
  a token.

### Constraints that must hold for OIDC publishing

These are subtle and each one silently breaks `npm publish`:

1. **No `registry-url` on `actions/setup-node`.** It writes an `.npmrc` with
   `_authToken=${NODE_AUTH_TOKEN}`; with no token that is an empty string, and
   npm then takes the token-auth path and skips the OIDC exchange entirely.
2. **npm ≥ 11.5.1.** Trusted publishing needs it. The workflow gets it from
   Node 24's bundled npm — *not* from an `npm install -g npm@latest` self-
   upgrade, which intermittently corrupts the install.
3. **`repository.url` casing must match the GitHub repo exactly.** Provenance
   verification compares `build/npm/package.json`'s `repository.url` against the
   repo reported by the OIDC provenance (`AngelMsger/wecom-calendar-cli`); a
   casing mismatch fails the publish with `E422`.
4. The trusted publisher must be configured on the **package**, not the
   account — npm's OIDC token exchange returns `404 package not found` when no
   per-package trusted publisher exists. For the very first release the package
   does not exist yet, so either publish `0.1.0` once manually (`npm publish
   --access public` from `build/npm/` with the binaries staged) and then
   configure the trusted publisher, or create the package placeholder first.

## Cutting a release

Before tagging, update [`CHANGELOG.md`](../CHANGELOG.md): rename the
`[Unreleased]` section to the new version with today's date, add a fresh empty
`[Unreleased]` heading, and update the comparison links at the bottom. Bump the
`version` field in `build/npm/package.json` to match. Commit both, then tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which:

1. runs the unit tests;
2. cross-compiles every platform via `make cross` → `dist/` (the binary version
   is taken from the git tag through `-ldflags`);
3. writes `dist/checksums.txt` (SHA-256 of every binary);
4. creates the GitHub Release for the tag with all `dist/` assets attached, or
   re-uploads the assets if the release already exists;
5. sets the npm package version to the tag (minus the `v`) and runs
   `npm publish --access public` for `@angelmsger/wecom-calendar-cli`, skipping
   the publish when that version is already on the registry.

Use semantic versioning (`vMAJOR.MINOR.PATCH`).

The workflow is **idempotent**: steps 4 and 5 tolerate a partial previous run,
so if a release fails halfway you can fix the cause and re-run it — either
re-run the failed run from the Actions tab, or move the tag to the fixed commit
(`git tag -f` / delete and re-push) to trigger a fresh run.

## Continuous integration

`.github/workflows/ci.yml` runs on every push to `main` and every pull request:
`gofmt` check, `go vet`, a `docs/cli/` drift check (`go run ./cmd/gen-docs`,
then fail if the committed reference differs), `go test ./...`, and the
end-to-end contract suite (`scripts/e2e.sh`). A second job runs the unit tests,
the npm installer mapping tests and a PowerShell/npm launcher smoke test on
Windows. The live e2e checks are not run in CI — they require a real WeCom
account and an app-specific CalDAV password.

The CLI reference under `docs/cli/` is generated from the cobra command tree
(`cmd/gen-docs`); run `make docs` after any command or flag change and commit
the result, or CI will fail.

`.github/workflows/pages.yml` publishes `docs/` to GitHub Pages on every push to
`main` that touches `docs/`. Enable it once: repository Settings → Pages →
Source → **GitHub Actions**. The site is served at
<https://angelmsger.github.io/wecom-calendar-cli/>.

## Release artifact contract

The release asset names are **stable** and must not change — the npm installer
and the `doctor` release-update check both depend on them:

```
wecom-calendar-cli-darwin-amd64   wecom-calendar-cli-linux-amd64
wecom-calendar-cli-darwin-arm64   wecom-calendar-cli-linux-arm64
wecom-calendar-cli-windows-amd64.exe
wecom-calendar-cli-windows-arm64.exe
checksums.txt
```

Download URL pattern:
`https://github.com/angelmsger/wecom-calendar-cli/releases/download/v<version>/<asset>`

## Companion Skill

The `wecom-calendar` Skill is **embedded into the binary** at build time
(`//go:embed skills/wecom-calendar`, see [`assets.go`](../assets.go)), so every
release ships a Skill that matches the CLI version; users deploy it with
`wecom-calendar-cli skill install`. The Skill is also published in the git
repository for the `npx skills` workflow.

The Skill is versioned independently via the `version:` field in
`skills/wecom-calendar/SKILL.md`. Bump it whenever the Skill or its
`references/` change.

## Local data caveat

Unlike the stateless sibling CLIs, this one keeps a local SQLite store at
`~/.angelmsger/wecom-calendar/calendar.db` containing the user's real calendar
data. Never attach a store file to a release, an issue, or a test fixture; the
repository's `.gitignore` excludes `*.db` and its WAL/journal siblings for this
reason.
