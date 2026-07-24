#!/usr/bin/env bash
# End-to-end smoke test for wecom-calendar-cli.
#
# The default run is fully offline: it drives the built binary against a
# throwaway config directory and asserts the agent-facing contracts that do not
# need a server (output on stdout, notices/errors on stderr, exit codes,
# read-only and --dry-run gates, cursor validation). Set WECOM_CALENDAR_E2E_LIVE=1
# — with real credentials in the environment — to also exercise a live sync.
set -u -o pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${WECOM_CALENDAR_BIN:-$ROOT/bin/wecom-calendar-cli}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

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
assert_stdout_contains "empty event list envelope" '"items"' -- "${base[@]}" event list --since 2026-01-01 --until 2026-01-02
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

if [ "${WECOM_CALENDAR_E2E_LIVE:-0}" = "1" ]; then
  echo "== live sync =="
  if [ -z "${WECOM_CALENDAR_USERNAME:-}" ] || [ -z "${WECOM_CALENDAR_PASSWORD:-}" ] || [ -z "${WECOM_CALENDAR_SERVER:-}" ]; then
    bad "live run requested but WECOM_CALENDAR_SERVER/USERNAME/PASSWORD are not all set"
  else
    assert_exit 0 "doctor" -- "${base[@]}" doctor
    assert_exit 0 "sync"   -- "${base[@]}" sync
    assert_stdout_contains "live event list envelope" '"items"' \
      -- "${base[@]}" event list --since 2026-01-01 --until 2026-12-31
  fi
fi

if [ "$fail" = "0" ]; then echo "e2e: PASS"; else echo "e2e: FAIL"; fi
exit "$fail"
