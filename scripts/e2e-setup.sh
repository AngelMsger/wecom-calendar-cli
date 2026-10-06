#!/usr/bin/env bash
# Exercise team distribution without a running service or access to personal state.
#
# Every command runs with an empty environment, a scratch HOME and a scratch
# --config directory, from a scratch working directory, so neither a
# developer's configuration, keychain nor .env can be read, and nothing here
# contacts a server: the URLs below are configuration values only.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${WECOM_CALENDAR_BIN:-$ROOT/bin/wecom-calendar-cli}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cd "$WORK"
run() {
  env -i PATH="$PATH" HOME="$WORK" NO_COLOR=1 WECOM_CALENDAR_CLI_NO_UPDATE_NOTIFIER=1 \
    "$BIN" --config "$WORK/config" "$@"
}
# run_at <config-dir> <args...> is `run` against another scratch config directory.
run_at() {
  local dir="$1"; shift
  env -i PATH="$PATH" HOME="$WORK" NO_COLOR=1 WECOM_CALENDAR_CLI_NO_UPDATE_NOTIFIER=1 \
    "$BIN" --config "$dir" "$@"
}
fail() { echo "FAIL: $1" >&2; exit 1; }
require() { [[ "$1" == *"$2"* ]] || fail "expected $2"; }
refuse() { [[ "$1" != *"$2"* ]] || fail "unexpected $2"; }
absent() { [[ ! -e "$1" ]] || fail "$1 must not exist"; }
# refused <exit> <code> <cmd...>: the command fails with that exit code and
# error code and prints nothing on stdout. The error is left in error.json.
refused() {
  local want="$1" code="$2" got=0; shift 2
  "$@" >out.json 2>error.json || got=$?
  [[ "$got" == "$want" ]] || fail "$code: expected exit $want, got $got"
  [[ ! -s out.json ]] || fail "$code: a refused command wrote to stdout"
  require "$(cat error.json)" "\"code\": \"$code\""
}

out="$(run config set-context team --base-url https://offline.invalid/deploy --auth-scheme basic --dry-run)"
require "$out" '"dry_run": true'
absent "$WORK/config/config.yaml"
out="$(env -i PATH="$PATH" HOME="$WORK" NO_COLOR=1 WECOM_CALENDAR_CLI_NO_UPDATE_NOTIFIER=1 WECOM_CALENDAR_CONTEXT=unconfigured \
  WECOM_CALENDAR_PASSWORD=do-not-save WECOM_CALENDAR_USERNAME=do-not-save@example.invalid \
  "$BIN" --config "$WORK/config" config set-context team --base-url https://offline.invalid/deploy \
  --auth-scheme basic --credential-url https://help.invalid/team-caldav)"
require "$out" '"current_context": "team"'
refuse "$(cat "$WORK/config/config.yaml")" do-not-save
refuse "$(cat "$WORK/config/config.yaml")" username
absent "$WORK/config/credentials"
out="$(run config set-context team)"; require "$out" '"changed": false'
out="$(run auth guide)"; require "$out" 'https://help.invalid/team-caldav'; require "$out" '"source": "file"'; require "$out" "--use-context 'team' auth login"
# The guide keeps the deployment path, names the mobile-app steps, and carries
# every field the Skill documents.
require "$out" '"server": "https://offline.invalid/deploy"'
require "$out" 'Sync to other calendars'; require "$out" 'invalidates the previous one'
for field in server scheme credential_url source instructions documentation_url next_steps; do
  require "$out" "\"$field\":"
done
if run config set-context team --base-url https://other.invalid >out.json 2>error.json; then
  fail 'conflicting setup succeeded'
fi
[[ ! -s out.json ]] || fail 'a refused setup wrote to stdout'
require "$(cat error.json)" CONFIG_CONTEXT_CONFLICT
require "$(cat error.json)" '"details"'
run config set-context team --base-url https://other.invalid --overwrite >/dev/null
run config set-context second --base-url https://second.invalid --auth-scheme basic >/dev/null
out="$(run config show)"; require "$out" 'https://other.invalid'
run config set-context second --activate >/dev/null
out="$(run config show)"; require "$out" 'https://second.invalid'
# An environment-only consumer receives the same acquisition guidance without a config file.
out="$(env -i PATH="$PATH" HOME="$WORK" NO_COLOR=1 WECOM_CALENDAR_CLI_NO_UPDATE_NOTIFIER=1 WECOM_CALENDAR_SERVER=https://env.invalid/deploy \
  WECOM_CALENDAR_AUTH_SCHEME=basic WECOM_CALENDAR_CREDENTIAL_URL=https://help.invalid/env \
  "$BIN" --config "$WORK/empty" auth guide)"
require "$out" 'https://help.invalid/env'; require "$out" '"source": "env"'
absent "$WORK/empty/config.yaml"

# WeCom differences from the family. The server has a documented default, so a
# preset needs no --base-url; and there is no web credential page, so the guide
# has no URL until a team configures one.
out="$(run config set-context plain)"; require "$out" '"after": "https://caldav.wecom.work"'
out="$(run --use-context plain auth guide)"
require "$out" '"credential_url": ""'; require "$out" '"source": "builtin"'
# The projection the Skill shows for this case, byte for byte.
out="$(run --use-context plain auth guide --fields credential_url,source)"
[[ "$out" == $'{\n  "credential_url": "",\n  "source": "builtin"\n}' ]] || fail "guide projection changed: $out"
# basic is the only scheme the plumbing accepts.
if run config set-context rejected --auth-scheme pat >out.json 2>error.json; then
  fail 'an unsupported auth scheme was accepted'
fi
require "$(cat error.json)" AUTH_BAD_SCHEME
refuse "$(cat "$WORK/config/config.yaml")" rejected
# Login needs a terminal and says where the password comes from; a different
# complete service URL is refused first. Neither reaches the credential store.
if run auth login </dev/null >out.json 2>error.json; then
  fail 'auth login ran without a terminal'
fi
require "$(cat error.json)" AUTH_LOGIN_NEEDS_TTY; require "$(cat error.json)" 'wecom-calendar-cli auth guide'
if run auth login --base-url https://second.invalid/elsewhere </dev/null >out.json 2>error.json; then
  fail 'auth login accepted another service URL'
fi
require "$(cat error.json)" CONTEXT_BASE_URL_MISMATCH; require "$(cat error.json)" 'no credential was stored'

# auth reuse, on the paths that need neither a server nor the credential store.
# No context above carries a WeCom email, so none can be a source and no
# credential is read. A verified preview and the write are covered by the unit
# tests against an httptest stub and the mocked keyring. Never add a step that
# runs `auth reuse` where a source has an email: it would read the OS keychain.
before="$(cat "$WORK/config/config.yaml")"
out="$(run --use-context plain auth reuse --dry-run)"
# The no-change result the Skill shows, byte for byte.
[[ "$out" == $'{\n  "changed": false,\n  "context": "plain",\n  "dry_run": true,\n  "reason": "no matching stored identity can be reused",\n  "state": "unavailable",\n  "verified": false\n}' ]] \
  || fail "reuse preview changed: $out"
refused 6 AUTH_REUSE_SOURCE_NOT_FOUND run --use-context plain auth reuse --from-context absent
require "$(cat error.json)" '"wecom-calendar-cli config get-contexts"'
# A source on another server is refused before any credential is read.
refused 11 AUTH_REUSE_SOURCE_MISMATCH run --use-context plain auth reuse --from-context second
require "$(cat error.json)" '"wecom-calendar-cli config get-contexts"'
refused 11 AUTH_REUSE_TARGET_MISMATCH run --use-context plain --base-url https://second.invalid auth reuse
require "$(cat error.json)" "--use-context 'plain' config show --explain"
refused 3 AUTH_REUSE_TARGET_MISSING run_at "$WORK/empty" auth reuse
require "$(cat error.json)" '"wecom-calendar-cli config set-context <name>"'
# None of them is a password problem, and each says so.
require "$(cat error.json)" 'do not issue a new CalDAV password'
# The recovery those errors advertise is a command this CLI has. The context
# listing is `config get-contexts` here, not the `config contexts` of some siblings.
out="$(run config get-contexts)"; require "$out" '"name": "plain"'
refused 2 UNKNOWN_COMMAND run config contexts
[[ "$(cat "$WORK/config/config.yaml")" == "$before" ]] || fail 'a reuse preview or a refused reuse changed the config'

# A member who is already signed in on the public endpoint. No offline command
# records a WeCom email, so this file is written by hand; the address is a
# placeholder and no password exists anywhere.
mkdir -p "$WORK/member"
cat >"$WORK/member/config.yaml" <<'YAML'
current_context: personal
contexts:
  - name: personal
    server: https://caldav.wecom.work/
    auth:
      scheme: basic
      username: member@example.invalid
YAML
# A preset on that server, the guide and the guide's next steps all lead with
# reuse, ahead of anything that asks for a password. They read the file only.
out="$(run_at "$WORK/member" config set-context team)"
require "$out" "\"wecom-calendar-cli --use-context 'team' auth reuse --dry-run\","
out="$(run_at "$WORK/member" --use-context team auth guide)"
require "$out" 'No password is asked for and none is copied'
# The projection the Skill shows for this case, byte for byte.
out="$(run_at "$WORK/member" --use-context team auth guide --fields next_steps)"
[[ "$out" == $'{\n  "next_steps": [\n    "wecom-calendar-cli --use-context \'team\' auth reuse --dry-run",\n    "wecom-calendar-cli --use-context \'team\' auth login"\n  ]\n}' ]] \
  || fail "guide next steps changed: $out"
# A context that has its email is left as it is, and is not offered reuse.
before="$(cat "$WORK/member/config.yaml")"
out="$(run_at "$WORK/member" --use-context personal auth reuse)"
[[ "$out" == $'{\n  "changed": false,\n  "context": "personal",\n  "dry_run": false,\n  "reason": "destination identity is already configured",\n  "state": "unchanged",\n  "verified": false\n}' ]] \
  || fail "reuse on a complete context changed: $out"
refuse "$(run_at "$WORK/member" --use-context personal auth guide)" 'auth reuse'
[[ "$(cat "$WORK/member/config.yaml")" == "$before" ]] || fail 'an unchanged reuse rewrote the config'

for dir in config member; do
  absent "$WORK/$dir/credentials"
  [[ "$(ls -A "$WORK/$dir")" == "config.yaml" ]] || fail "setup left more than config.yaml in $dir: $(ls -A "$WORK/$dir")"
done
absent "$WORK/empty"
echo 'PASS: offline team setup, idempotency, conflict, activation, environment-only guidance, login guards and reuse guards'
