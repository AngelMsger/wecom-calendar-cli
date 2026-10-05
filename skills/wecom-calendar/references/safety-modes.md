# Safety modes — `--dry-run`, read-only, and what `sync` touches

`wecom-calendar-cli` mutates only the **local store**, and only through two
commands: `meta set` and `meta delete`. Two orthogonal safety mechanisms guard
those writes.

| | Question it answers | Scope |
|---|---|---|
| `--dry-run` | "What would this write change, without applying it?" | Per command |
| Read-only mode | "Block all local writes for this session." | Per invocation / session |

## What counts as a write

- **Writes** (blocked by read-only, accept `--dry-run`): `meta set`,
  `meta delete`. These are the only commands that change the agent-owned
  metadata layer.
- **`sync` is not a metadata write.** It reads the WeCom server and reconciles
  the raw-fact tables of the store; read-only mode does **not** block it, and it
  never writes or deletes metadata. `sync --dry-run` still previews what it
  would reconcile. `expand` likewise only rebuilds derived occurrences.
- **Reads** (never blocked): `calendar list`, `event list`, `event get`,
  `whoami`, `meta get`, `meta list`, `doctor`, `config show`.

## `--dry-run` — preview, never apply

`meta set` and `meta delete` accept `--dry-run`: the command resolves the write
and prints what it *would* change as JSON, without writing to the store.

```bash
wecom-calendar-cli meta set <uid> task feishu_project 6949886165 --dry-run
# {
#   "dry_run": true,
#   "key": "feishu_project",
#   "namespace": "task",
#   "source": "agent",
#   "uid": "<uid>",
#   "value": 6949886165
# }
```

`value` is shown as it would be stored — here a JSON number, because the text
parses as one (see [metadata.md](metadata.md)). A `meta delete --dry-run`
reports `"status": "would_delete"` with the `current_value`, or
`"status": "not_found"`.

Use it before any write whose target (`uid`, namespace, key) was inferred
rather than pasted in literally — confirm the UID matches the event you mean.
A dry run checks the request; it is not a second approval step.

## `--yes` — confirm a destructive delete

`meta delete` removes a metadata entry, so applying it (outside `--dry-run`)
requires confirmation. With a terminal it prompts `[y/N]`; without one — the
usual case for an agent — it refuses with `CONFIRM_REQUIRED` unless you pass
`--yes`. The safe pattern is preview then confirm:

```bash
wecom-calendar-cli meta delete <uid> task feishu_project --dry-run   # preview
wecom-calendar-cli meta delete <uid> task feishu_project --yes       # apply
```

## Read-only mode — lock the session

A session-level switch that blocks every metadata write before it touches the
store. Enable it by either:

- `defaults.read_only: true` in `~/.angelmsger/wecom-calendar/config.yaml`, or
- `WECOM_CALENDAR_CLI_READ_ONLY=1` in the environment.

Blocked writes return a structured error:

```json
{
  "error": {
    "category": "permission",
    "code": "READONLY_BLOCKED",
    "message": "meta set is blocked by read-only mode",
    "hint": "Pass --allow-writes, or unset defaults.read_only / WECOM_CALENDAR_CLI_READ_ONLY.",
    "next_steps": [
      "Add --allow-writes to the command line",
      "unset WECOM_CALENDAR_CLI_READ_ONLY",
      "Set defaults.read_only=false in ~/.angelmsger/wecom-calendar/config.yaml"
    ],
    "retryable": false
  }
}
```

Exit code: 5 (`permission`).

### Per-call override: `--allow-writes`

When the current task authorizes the concrete write despite a configured
read-only default, use the root-level `--allow-writes` flag for that invocation.
Do not override an explicit read-only instruction or change persistent settings
just because an error suggests it. Existing authorization needs no repeated
confirmation; unresolved scope needs clarification.

```bash
WECOM_CALENDAR_CLI_READ_ONLY=1 wecom-calendar-cli --allow-writes \
    meta set <uid> class category "customer-meeting"
```

This is the only way to flip the posture for one invocation without changing
config or env.

### What read-only does NOT block

CLI self-configuration and data sync are out of scope, otherwise an agent that
enabled read-only would lose the ability to recover or refresh:

- `config init`, `auth login`, `auth logout`, `config use-context`
- `skill install`, `skill uninstall`
- `sync` — it does not write metadata; read-only protects the metadata layer,
  not the store's synced facts.

## Recommended pattern for agents

1. If the user said "read-only", "don't change anything", or "just summarize" —
   set `WECOM_CALENDAR_CLI_READ_ONLY=1` for the session. Every read still works;
   any accidental `meta` write hits `READONLY_BLOCKED` before touching the store.
2. Before a `meta set` / `meta delete` whose target you inferred, run it with
   `--dry-run` and confirm the UID and (namespace, key). When the user already
   named the event and the annotation, write it directly.
3. They compose: `WECOM_CALENDAR_CLI_READ_ONLY=1 wecom-calendar-cli
   --allow-writes meta delete <uid> task feishu_project --dry-run` previews the
   delete without applying it; drop `--dry-run` and add `--yes` to apply.
