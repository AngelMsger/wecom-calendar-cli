#!/usr/bin/env bash
# End-to-end smoke test for wecom-calendar-cli.
#
# The default run is fully offline: it drives the built binary against a
# throwaway config directory and asserts the agent-facing contracts that do not
# need a server (output on stdout, notices/errors on stderr, exit codes,
# read-only and --dry-run gates, the time-window flags, cursor validation,
# NDJSON continuation over a seeded store, write outcomes, and the CLI/Skill
# upgrade loop). It ends by running scripts/e2e-setup.sh, the offline team
# setup, personal-login and login-reuse checks. Set WECOM_CALENDAR_E2E_LIVE=1 —
# with real credentials in the environment — to also exercise a live sync.
set -u -o pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${WECOM_CALENDAR_BIN:-$ROOT/bin/wecom-calendar-cli}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
# Read the Skill version from the embedded SKILL.md so bumping it does not
# silently stale the handshake assertions below.
SKILL_MD="$ROOT/skills/wecom-calendar/SKILL.md"
SKILL_VERSION="$(sed -n 's/^version: *//p' "$SKILL_MD" | head -1)"
if [ -z "$SKILL_VERSION" ]; then
  echo "could not read version: from skills/wecom-calendar/SKILL.md" >&2
  exit 1
fi

fail=0
pass() { printf '  ok  %s\n' "$1"; }
bad()  { printf '  FAIL %s\n' "$1"; fail=1; }

# assert_exit <expected-code> <label> -- <cmd...>
assert_exit() {
  local want="$1" label="$2"; shift 2; shift # drop the "--"
  "$@" >/dev/null 2>&1
  local got=$?
  if [ "$got" = "$want" ]; then pass "$label (exit $got)"; else bad "$label (want exit $want, got $got)"; fi
}

# assert_stdout_contains <label> <needle> -- <cmd...>
assert_stdout_contains() {
  local label="$1" needle="$2"; shift 2; shift
  local out; out="$("$@" 2>/dev/null)"
  if printf '%s' "$out" | grep -q -- "$needle"; then pass "$label"; else bad "$label (stdout missing '$needle')"; fi
}

# assert_stderr_contains <label> <needle> -- <cmd...>
assert_stderr_contains() {
  local label="$1" needle="$2"; shift 2; shift
  local err; err="$("$@" 2>&1 >/dev/null)"
  if printf '%s' "$err" | grep -qF -- "$needle"; then pass "$label"; else bad "$label (stderr missing '$needle')"; fi
}

# offline <cmd...> runs a command that could reach the server from the scratch
# directory with the credential variables cleared, so neither a developer's
# .env nor an exported WECOM_CALENDAR_* turns an offline check into a request.
offline() {
  (cd "$WORK" && env -u WECOM_CALENDAR_USERNAME -u WECOM_CALENDAR_PASSWORD "$@")
}

if [ ! -x "$BIN" ]; then
  echo "binary not found at $BIN — run 'make build' first" >&2
  exit 1
fi

CFG="$WORK/cfg"
mkdir -p "$CFG"
base=("$BIN" --config "$CFG")

echo "== offline contracts =="
assert_exit 0 "version"                     -- "$BIN" version
assert_exit 0 "--help"                      -- "$BIN" --help
assert_stdout_contains "skill status is JSON" '"' -- "$BIN" skill status
# event list on an empty store returns a valid, empty envelope (exit 0).
assert_stdout_contains "empty event list envelope" '"items"' -- "${base[@]}" event list --from 2026-01-01 --to 2026-01-02
# read-only posture: a real write is blocked, but --dry-run still previews.
assert_exit 5 "meta set blocked under read-only" -- env WECOM_CALENDAR_CLI_READ_ONLY=1 "${base[@]}" meta set uid ns key val
assert_stdout_contains "meta set --dry-run works under read-only" '"dry_run"' \
  -- env WECOM_CALENDAR_CLI_READ_ONLY=1 "${base[@]}" meta set uid ns key val --dry-run
assert_stdout_contains "meta delete --dry-run works under read-only" '"dry_run"' \
  -- env WECOM_CALENDAR_CLI_READ_ONLY=1 "${base[@]}" meta delete uid ns key --dry-run
# destructive delete without --yes is refused non-interactively (not a silent
# delete). Redirect stdin so the run is unambiguously non-interactive.
"${base[@]}" meta delete uid ns key </dev/null >/dev/null 2>&1
[ $? = 2 ] && pass "meta delete without --yes -> confirm required (exit 2)" \
            || bad "meta delete without --yes -> confirm required (want exit 2)"
# --dry-run must not create the database on a pristine config dir (write nothing).
FRESH="$WORK/fresh"; mkdir -p "$FRESH"
"$BIN" --config "$FRESH" meta delete uid ns key --dry-run >/dev/null 2>&1
[ ! -e "$FRESH/calendar.db" ] && pass "dry-run creates no database" \
                              || bad "dry-run created a database (should write nothing)"
# identity primitive and rich single-event read.
assert_stdout_contains "whoami reports identity" '"configured"' -- "${base[@]}" whoami
assert_exit 6 "event get unknown uid -> not found" -- "${base[@]}" event get no-such-uid
assert_stdout_contains "event list --status accepted" '"items"' -- "${base[@]}" event list --status confirmed,tentative
assert_stdout_contains "event list --include-meta accepted" '"items"' -- "${base[@]}" event list --include-meta
assert_stdout_contains "meta list --value reverse lookup" '"items"' -- "${base[@]}" meta list --value anything
# malformed input is a structured usage error (exit 2), not a crash.
assert_exit 2 "unknown flag -> usage error"  -- "${base[@]}" event list --nope
assert_exit 2 "bad --cursor -> usage error"  -- "${base[@]}" event list --cursor not-a-cursor

echo "== time-window contract =="
# Family vocabulary: --since <duration> looks back from now, --from/--to bound
# [from, to), --since excludes them, and --to requires --from.
assert_exit 0 "event list --since <duration>" -- "${base[@]}" event list --since 7d
assert_exit 0 "a window may reach into the future" -- "${base[@]}" event list --from now --to now+14d
assert_exit 2 "--since with --from -> usage error" -- "${base[@]}" event list --since 7d --from 2026-01-01
assert_stderr_contains "window errors use BAD_TIME_RANGE" '"code": "BAD_TIME_RANGE"' \
  -- "${base[@]}" event list --since 7d --from 2026-01-01
assert_stderr_contains "--to requires --from" '"message": "--to requires --from"' \
  -- "${base[@]}" event list --to 2026-01-02
# The spellings used before the family vocabulary keep working: same result,
# plus one structured stderr notice per deprecated flag, which can be silenced.
legacy_err="$WORK/legacy.err"
legacy_out="$("${base[@]}" event list --since 2026-01-01 --until 2026-01-02 2>"$legacy_err")"
canonical_out="$("${base[@]}" event list --from 2026-01-01 --to 2026-01-02 2>/dev/null)"
if [ -n "$legacy_out" ] && [ "$legacy_out" = "$canonical_out" ] \
   && [ "$(grep -c '"deprecated_flag"' "$legacy_err")" = 2 ] \
   && grep -qF '"flag":"--since <date>"' "$legacy_err" && grep -qF '"replacement":"--from <date>"' "$legacy_err" \
   && grep -qF '"flag":"--until"' "$legacy_err" && grep -qF '"replacement":"--to"' "$legacy_err" \
   && grep -qF 'WECOM_CALENDAR_CLI_NO_DEPRECATION_NOTICE=1' "$legacy_err"; then
  pass "--since <date> / --until still work and each emits a deprecation notice"
else
  bad "deprecated window flags (stdout: $legacy_out, stderr: $(cat "$legacy_err"))"
fi
if env WECOM_CALENDAR_CLI_NO_DEPRECATION_NOTICE=1 "${base[@]}" event list --until 2099-01-01 2>&1 >/dev/null \
     | grep -q '"deprecated_flag"'; then
  bad "WECOM_CALENDAR_CLI_NO_DEPRECATION_NOTICE did not silence the notice"
else
  pass "the deprecation notice can be silenced"
fi
assert_exit 0 "--until alone keeps its default lower bound" -- "${base[@]}" event list --until 2099-01-01
# Paging needs fixed bounds: a cursor on a relative window is refused, and the
# error restates the window as absolute --from/--to.
assert_stderr_contains "cursor on a relative window is rejected" '"code": "CURSOR_RELATIVE_WINDOW"' \
  -- "${base[@]}" event list --since 7d --cursor anything
assert_stderr_contains "the rejection names absolute bounds" 'wecom-calendar-cli event list --from 20' \
  -- "${base[@]}" event list --from now-7d --to now+7d --cursor anything
# expand pins the expansion window with --from/--to and has no look-back.
assert_stdout_contains "expand --from/--to pins the window" '"window_pinned": true' \
  -- "${base[@]}" expand --from 2018-01-01 --to 2040-01-01
assert_stdout_contains "expand --since <date>/--until still pin" '"window_pinned": true' \
  -- "${base[@]}" expand --since 2018-01-01 --until 2040-01-01
assert_stderr_contains "expand aliases emit the deprecation notice" '"command":"wecom-calendar-cli expand"' \
  -- "${base[@]}" expand --since 2018-01-01 --until 2040-01-01
assert_exit 2 "expand --since <duration> -> usage error" -- "${base[@]}" expand --since 7d
assert_stderr_contains "expand explains why it has no look-back" 'would drop every future occurrence' \
  -- "${base[@]}" expand --since 7d
assert_stdout_contains "expand with no flags clears the pin" '"window_pinned": false' -- "${base[@]}" expand

echo "== pagination and NDJSON continuation over a seeded store =="
# `event list` is the paginated command, and it reads the derived instances, so
# a few synthetic occurrences are enough to page for real without a server.
PAGE_CFG="$WORK/page-cfg"; mkdir -p "$PAGE_CFG"
if (cd "$ROOT" && go run ./test/seedstore "$PAGE_CFG") 2>"$WORK/seed.log"; then
  paged=(env WECOM_CALENDAR_CLI_NO_UPDATE_NOTIFIER=1 "$BIN" --config "$PAGE_CFG" event list --fields uid)
  # No window flag: this is the default window, whose cursor used to fail with
  # CURSOR_MISMATCH because its bounds followed the current millisecond.
  rows="$("${paged[@]}" --format ndjson --limit 2 2>"$WORK/page1.err")"
  next="$(sed -n 's/.*"pagination":{[^}]*"next":"\([^"]*\)".*/\1/p' "$WORK/page1.err")"
  if [ "$rows" = $'{"uid":"e2e-a"}\n{"uid":"e2e-b"}' ] && [ -n "$next" ] \
     && grep -qF '"has_more":true' "$WORK/page1.err" \
     && grep -qF '"next_steps":["Pass next as --cursor to retrieve the next page."]' "$WORK/page1.err"; then
    pass "NDJSON keeps projected rows on stdout and the continuation on stderr"
  else
    bad "NDJSON first page (stdout: $rows, stderr: $(cat "$WORK/page1.err"))"
  fi
  rest="$("${paged[@]}" --format ndjson --limit 2 --cursor "$next" 2>"$WORK/page2.err")"
  rest_code=$?
  if [ "$rest_code" = 0 ] && [ "$rest" = $'{"uid":"e2e-c"}\n{"uid":"e2e-ahead"}' ] \
     && ! grep -q '"pagination"' "$WORK/page2.err"; then
    pass "the emitted token resumes the next page, and the last page advertises none"
  else
    bad "NDJSON resume (exit $rest_code, stdout: $rest, stderr: $(cat "$WORK/page2.err"))"
  fi
  everything="$("${paged[@]}" --format ndjson --all 2>"$WORK/all.err")"
  if [ "$everything" = "$rows"$'\n'"$rest" ] && ! grep -q '"pagination"' "$WORK/all.err"; then
    pass "--all returns every row with no continuation notice"
  else
    bad "NDJSON --all (stdout: $everything, stderr: $(cat "$WORK/all.err"))"
  fi
  assert_stdout_contains "JSON envelope carries the same token" "\"next\": \"$next\"" \
    -- "${paged[@]}" --format json --limit 2
  assert_stdout_contains "table footer names the same flag" "re-run with --cursor $next" \
    -- "${paged[@]}" --format table --limit 2
  # A look-back ends now; the look-ahead forms reach the occurrence two days out.
  if "${paged[@]}" --format ndjson --since 7d 2>/dev/null | grep -q 'e2e-ahead'; then
    bad "--since returned an occurrence in the future"
  else
    pass "--since <duration> ends now"
  fi
  ahead="$("${paged[@]}" --format ndjson --from now --to now+14d 2>/dev/null)"
  [ "$ahead" = '{"uid":"e2e-ahead"}' ] && pass "--from now --to now+14d looks ahead" \
                                         || bad "look-ahead window (got: $ahead)"
  # A relative window is read in one call with --all instead of a cursor.
  recent="$("${paged[@]}" --format ndjson --since 7d --all 2>"$WORK/recent.err")"
  if [ "$recent" = $'{"uid":"e2e-a"}\n{"uid":"e2e-b"}\n{"uid":"e2e-c"}' ] && ! grep -q '"pagination"' "$WORK/recent.err"; then
    pass "--since with --all resolves the relative window once"
  else
    bad "--since --all (stdout: $recent)"
  fi
else
  bad "could not seed the store: $(cat "$WORK/seed.log")"
fi

echo "== metadata write sequences from the Skill =="
# set overwrites (last write wins), --allow-writes lifts read-only for one call,
# and a value that parses as JSON keeps its type unless quoted as a JSON string.
assert_stdout_contains "meta set reports status" '"status": "set"' -- "${base[@]}" meta set e2e-uid task link T-1
assert_stdout_contains "meta set overwrites"     '"value": "T-2"' -- "${base[@]}" meta set e2e-uid task link T-2
"${base[@]}" meta get e2e-uid task link 2>/dev/null | grep -q 'T-1' \
  && bad "meta set merged instead of replacing" || pass "meta get shows only the last value"
assert_stderr_contains "read-only block names the override" 'Add --allow-writes to the command line' \
  -- env WECOM_CALENDAR_CLI_READ_ONLY=1 "${base[@]}" meta set e2e-uid class category x
assert_stdout_contains "--allow-writes overrides read-only once" '"status": "set"' \
  -- env WECOM_CALENDAR_CLI_READ_ONLY=1 "${base[@]}" --allow-writes meta set e2e-uid class category customer-meeting
assert_stdout_contains "numeric text is stored as a number" '"value": 6949886165' \
  -- "${base[@]}" meta set e2e-uid task feishu_project 6949886165 --dry-run
assert_stdout_contains "a quoted JSON string stays a string" '"value": "6949886165"' \
  -- "${base[@]}" meta set e2e-uid task feishu_project '"6949886165"' --dry-run
assert_stdout_contains "meta delete --dry-run shows the current value" '"status": "would_delete"' \
  -- "${base[@]}" meta delete e2e-uid class category --dry-run
assert_stdout_contains "meta delete --yes applies" '"status": "deleted"' \
  -- "${base[@]}" meta delete e2e-uid class category --yes
assert_stdout_contains "repeated delete is not an error" '"status": "not_found"' \
  -- "${base[@]}" meta delete e2e-uid class category --yes

echo "== committed writes whose result cannot be printed =="
# The local write has no read-back and no uncertain outcome; the one step after
# the commit is printing the result. An unknown --format fails exactly there.
for operation in set delete; do
  case "$operation" in
    set)    args=(meta set e2e-uid task outcome T-9) ;;
    delete) args=(meta delete e2e-uid task outcome --yes) ;;
  esac
  result_file="$WORK/write-outcome.err"
  out="$("${base[@]}" "${args[@]}" --format bogus 2>"$result_file")"
  result_code=$?
  result_error="$(cat "$result_file")"
  if [ "$result_code" = 2 ] && [ -z "$out" ] \
     && [[ "$result_error" == *'WRITE_SUCCEEDED_OUTPUT_FAILED'* ]] \
     && [[ "$result_error" == *'metadata entry e2e-uid / task / outcome'* ]] \
     && [[ "$result_error" == *'"retryable": false'* ]] \
     && [[ "$result_error" == *"wecom-calendar-cli meta get 'e2e-uid' 'task' 'outcome'"* ]]; then
    pass "meta $operation preserves the committed write and entry identity after an output failure"
  else
    bad "meta $operation output failure contract (exit $result_code, stdout: $out, stderr: $result_error)"
  fi
  # Follow the recovery step: the read shows the committed state.
  read_back="$("${base[@]}" meta get 'e2e-uid' 'task' 'outcome' 2>/dev/null)"
  case "$operation" in
    set)    [[ "$read_back" == *'"T-9"'* ]] && pass "recovery read shows the stored value" \
                                            || bad "recovery read after set (got: $read_back)" ;;
    delete) [[ "$read_back" == *'"items": []'* ]] && pass "recovery read shows the entry is gone" \
                                                  || bad "recovery read after delete (got: $read_back)" ;;
  esac
done
assert_stderr_contains "no write, no write-outcome claim" '"code": "BAD_FORMAT"' \
  -- "${base[@]}" meta delete e2e-uid task outcome --yes --format bogus

echo "== CLI and Skill upgrade loop =="
# doctor resolves the stored credential for its server. Point it at a local
# placeholder so the lookup can never be the developer's real keychain entry
# for the public endpoint, and so no request can leave the machine.
assert_stdout_contains "doctor reports Skill" '"companion-skill"' -- offline "${base[@]}" --base-url http://127.0.0.1:9 doctor --no-update-check
SKILL_DIR="$WORK/skills"
assert_stdout_contains "skill install alignment" '"alignment": "current"' \
  -- "$BIN" skill install --dir "$SKILL_DIR"
SKILL_HOME="$WORK/skill-home"; mkdir -p "$SKILL_HOME"
assert_stdout_contains "skill install for Codex" '"alignment": "current"' \
  -- env HOME="$SKILL_HOME" "$BIN" skill install --agent codex
assert_stdout_contains "skill status version aligned" '"loaded_status": "current"' \
  -- env HOME="$SKILL_HOME" WECOM_CALENDAR_CLI_SKILL="$SKILL_VERSION" "$BIN" skill status
assert_stderr_contains "legacy Skill handshake is detected" '"status":"unknown"' \
  -- env -u WECOM_CALENDAR_CLI_NO_SKILL_HINT HOME="$SKILL_HOME" WECOM_CALENDAR_CLI_SKILL=1 \
       WECOM_CALENDAR_CLI_NO_UPDATE_NOTIFIER=1 "${base[@]}" event list
# The Skill tells an agent which handshake value to export; it must be the
# version the binary embeds, or every agent that follows it reads as outdated.
if grep -qF "WECOM_CALENDAR_CLI_SKILL=$SKILL_VERSION" "$SKILL_MD"; then
  pass "Skill handshake instruction names its own version"
else
  bad "SKILL.md does not tell agents to export WECOM_CALENDAR_CLI_SKILL=$SKILL_VERSION"
fi
# The update notice compares release versions, and `make build` stamps whatever
# `git describe` prints (a bare commit hash in a shallow clone). Pin a
# release-like version, and seed the 24h release cache so the check stays
# offline.
PINNED="$WORK/wecom-calendar-cli-pinned"
UPDATE_CFG="$WORK/update-cfg"; mkdir -p "$UPDATE_CFG"
printf '{"checked_at":"%s","latest":"999.0.0"}' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$UPDATE_CFG/update-cache.json"
if (cd "$ROOT" && CGO_ENABLED=0 go build \
      -ldflags "-X github.com/angelmsger/wecom-calendar-cli/pkg/constants.Version=0.0.1" \
      -o "$PINNED" ./cmd/wecom-calendar-cli) 2>"$WORK/pinned-build.log"; then
  assert_stderr_contains "update notice includes Skill refresh" \
    '"next_steps":["npm install -g @angelmsger/wecom-calendar-cli@latest","wecom-calendar-cli skill install","reload the agent context' \
    -- env -u WECOM_CALENDAR_CLI_NO_UPDATE_NOTIFIER WECOM_CALENDAR_CLI_SKILL="$SKILL_VERSION" \
         "$PINNED" --config "$UPDATE_CFG" event list
else
  bad "could not build the version-pinned binary: $(cat "$WORK/pinned-build.log")"
fi

if [ "${WECOM_CALENDAR_E2E_LIVE:-0}" = "1" ]; then
  echo "== live sync =="
  if [ -z "${WECOM_CALENDAR_USERNAME:-}" ] || [ -z "${WECOM_CALENDAR_PASSWORD:-}" ] || [ -z "${WECOM_CALENDAR_SERVER:-}" ]; then
    bad "live run requested but WECOM_CALENDAR_SERVER/USERNAME/PASSWORD are not all set"
  else
    assert_exit 0 "doctor" -- "${base[@]}" doctor
    assert_exit 0 "sync"   -- "${base[@]}" sync
    assert_stdout_contains "live event list envelope" '"items"' \
      -- "${base[@]}" event list --from 2026-01-01 --to 2027-01-01
  fi
fi

if [ "$fail" != "0" ]; then echo "e2e: FAIL"; exit 1; fi
echo "e2e: PASS"
# Team setup runs as its own script with an empty environment and a scratch
# HOME, so it cannot see a developer's configuration, keychain or .env.
echo "== team setup, personal login and login reuse =="
WECOM_CALENDAR_BIN="$BIN" "$ROOT/scripts/e2e-setup.sh"
