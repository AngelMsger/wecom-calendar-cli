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
  would reconcile.
- **Reads** (never blocked): `calendar list`, `event list`, `meta get`,
  `meta list`, `doctor`, `config show`.

## `--dry-run` — preview, never apply

`meta set` and `meta delete` accept `--dry-run`: the command resolves the write
and prints what it *would* change as JSON, without writing to the store.

```bash
wecom-calendar-cli meta set <uid> task feishu_project 6949886165 --dry-run
# {
#   "dry_run": true,
#   "op": "set",
#   "uid": "<uid>",
#   "namespace": "task",
#   "key": "feishu_project",
#   "value": "6949886165"
# }
```

Use it before any write whose target (`uid`, namespace, key) was inferred
rather than pasted in literally — confirm the UID matches the event you mean.

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
    "message": "operation \"MetaSet\" blocked: read-only mode is enabled",
    "next_steps": [
      "Add --allow-writes to the command line",
      "unset WECOM_CALENDAR_CLI_READ_ONLY",
      "Set defaults.read_only=false in ~/.angelmsger/wecom-calendar/config.yaml"
    ]
  }
}
```

Exit code: 5 (`permission`).

### Per-call override: `--allow-writes`

When you genuinely need to write under a read-only posture, add the root-level
`--allow-writes` flag:

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
   `--dry-run` and confirm the UID and (namespace, key).
3. The two compose: `WECOM_CALENDAR_CLI_READ_ONLY=1 wecom-calendar-cli
   --allow-writes meta delete <uid> task feishu_project --dry-run` previews the
   delete without applying it.
