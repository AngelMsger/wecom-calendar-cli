# The metadata layer — agent-owned annotations

Alongside the synced calendar data, the store holds a **free-form metadata
layer**: annotations you attach to events and fully own. `sync` never writes or
deletes it, so annotations survive every re-sync. Use it to classify events,
tag them, or link them to external systems.

A request to annotate an event authorizes that write; do not ask again after
resolving the target. What to check before overwriting or deleting an existing
entry is in [working-with-the-user.md](working-with-the-user.md).

## Model — (uid, namespace, key) → value

Each metadata row is addressed by three parts plus a value:

- **`uid`** — the event it annotates, taken from an `event list` item's `uid`.
- **`namespace`** — a grouping you choose (e.g. `task`, `class`, `note`). The
  schema hard-codes none; namespaces are conventional.
- **`key`** — the attribute within the namespace (e.g. `feishu_project`,
  `category`).
- **`value`** — stored as JSON. Text that parses as JSON keeps that type —
  `6949886165` becomes a number, `true` a boolean, `'{"id":1}'` an object —
  and anything else is stored as a JSON string. To keep digits as a string,
  pass a quoted JSON string: `'"6949886165"'`.

An optional `--source` records who set the row (e.g. `agent`, a workflow name)
for provenance; it does not change addressing.

## Commands

```bash
# Set / overwrite one annotation
wecom-calendar-cli meta set <uid> <namespace> <key> <value> [--source agent] [--dry-run]

# Read: whole event, one namespace, or one key
wecom-calendar-cli meta get <uid>                 # every annotation on the event
wecom-calendar-cli meta get <uid> task            # just the "task" namespace
wecom-calendar-cli meta get <uid> task feishu_project   # one value

# Search across events
wecom-calendar-cli meta list --namespace task     # every task-namespace row
wecom-calendar-cli meta list --uid <uid>          # everything on one event
wecom-calendar-cli meta list --key category       # one key across all events
wecom-calendar-cli meta list --value g-5980639611 # reverse: events linked to a task

# Remove one annotation: preview, then confirm
wecom-calendar-cli meta delete <uid> <namespace> <key> --dry-run
wecom-calendar-cli meta delete <uid> <namespace> <key> --yes
```

`meta set` replaces the value and `source` at `(uid, namespace, key)` if the
entry already exists; it does not append or merge. To change part of a
structured value, read it with `meta get`, edit it, and write the whole value
back. The result carries `"status": "set"`, plus a `warning` when no live event
has that `uid` (the entry is kept anyway). `meta delete` reports
`"status": "deleted"`, or `"not_found"` when there was nothing to remove. If a
write reports an error instead, see
[errors-and-exit-codes.md](errors-and-exit-codes.md#writes-that-succeeded)
before repeating it.

`meta get` and `meta list` return the `{items, next, has_more}` envelope like
the other list commands, in one page. `--value <v>` is a **reverse lookup** —
it matches entries whose stored JSON value contains `<v>`, so a bare task id like
`g-5980639611` finds every event linked to that task (whether stored as a scalar
`"g-5980639611"` or inside a larger object). Combine it with `--key`/`--namespace`
to narrow the match. To pull an event's annotations *together with* its own
fields, use `event get <uid> --include-meta` (or `event list --include-meta`).

## Schema-agnostic — pick your own namespaces

The layer imposes no vocabulary. Common conventions:

```bash
# Classify an event
wecom-calendar-cli meta set <uid> class category "customer-meeting"

# Link an event to a Feishu project work item
wecom-calendar-cli meta set <uid> task feishu_project "6949886165" --source agent

# A structured value as JSON
wecom-calendar-cli meta set <uid> task link '{"system":"feishu","id":"6949886165"}'
```

Then find them later — e.g. every event linked to a task:

```bash
wecom-calendar-cli meta list --namespace task --key feishu_project
```

## Survives re-sync (the core guarantee)

Metadata is keyed by event UID, and `sync` / recurrence rebuild only write the
raw-fact and derived tables. So you can annotate an event today, `sync`
repeatedly, and `meta get <uid>` still returns the annotation. Even when an
event is later **soft-deleted** on the server, its metadata rows remain and stay
resolvable by UID.

## Writes honor read-only mode

`meta set` and `meta delete` are the CLI's only writes. Under a session
read-only posture (`WECOM_CALENDAR_CLI_READ_ONLY=1` or `defaults.read_only`)
they are blocked with `READONLY_BLOCKED` before touching the store.
`--allow-writes` overrides that for one invocation whose write the current task
authorizes; `--dry-run` previews the change without applying it. See
[safety-modes.md](safety-modes.md).
