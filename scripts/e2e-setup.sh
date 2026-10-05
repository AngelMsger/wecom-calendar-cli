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
fail() { echo "FAIL: $1" >&2; exit 1; }
require() { [[ "$1" == *"$2"* ]] || fail "expected $2"; }
refuse() { [[ "$1" != *"$2"* ]] || fail "unexpected $2"; }
absent() { [[ ! -e "$1" ]] || fail "$1 must not exist"; }

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
absent "$WORK/config/credentials"
[[ "$(ls -A "$WORK/config")" == "config.yaml" ]] || fail "setup left more than config.yaml: $(ls -A "$WORK/config")"
echo 'PASS: offline team setup, idempotency, conflict, activation, environment-only guidance and login guards'
