# Working with the user

How to carry a calendar task through without redundant lookups, repeated
permission requests or open-ended waiting. The other references explain the
commands; this one holds the rules for using them on someone's behalf.

## Reuse what is already known

Use the `uid`, calendar `id`, `occurrence_key` and date window the user
supplied or an earlier command returned. Do not re-run `event list` to find an
event you already hold. Discover only what is missing.

Sync once when the task starts, again when a read prints
`{"_notice":{"stale":…}}`, and when the user says the calendar changed — not
before every read.

`event list` without window flags covers 30 days either side of today and
moves with the date; `--since` and `now±duration` move with the clock. When
results have to stay comparable, or you will follow a cursor, pass absolute
`--from`/`--to` and keep working from the `uid`.

## Continue within the authorization you have

A request to annotate, tag, link or remove an annotation authorizes that write
on that event. Resolve the target, then do it once; do not ask again.
`--dry-run` checks the target and value — it is not a second approval step.

Ask when something is still open: several events match the description, the
namespace or key to use is unclear and existing entries do not settle it, or
the write would overwrite or delete an annotation the request did not mention.

Honor an explicit read-only request ("just look", "don't change anything") for
the whole task: return the annotation you would have written instead of writing
it. `--allow-writes` is for a write the current task authorizes despite a
configured read-only default. A `READONLY_BLOCKED` error is not by itself a
reason to add it, and its `next_steps` are options for the user, not
instructions: never unset the variable or edit `defaults.read_only` on your
own. See [safety-modes.md](safety-modes.md).

A request to create, move or cancel an event is outside this CLI. Hand it to
`wecom-cli`; do not record it as metadata instead.

## Bounded reads, no open-ended waiting

Size the window to the question and project with `--fields`. Call `event get`
for the events whose detail matters, not for every row of a list. Page sizes,
cursors and `--all` are covered in
[querying.md](querying.md#pagination-event-list).

A `sync` is finite. Let a long first or `--full` run finish instead of starting
another, and read its `progress` notices as liveness. Do not loop `sync` or a
query to wait for a change unless the user asked you to watch for one, and
agree a deadline or stopping condition before you start. `--timeout` bounds one
request to the server; it is not a deadline for a watch. Setting up a recurring
`sync` job is the user's decision.

## Preserve annotations you were not asked to change

`meta set` replaces the whole value at `(uid, namespace, key)`, and its
`source` tag with it. It never merges, and there is no version check: the last
write wins.

- To change part of a structured value, read it with `meta get`, edit that
  part, and write the complete value back.
- Read an entry before overwriting or deleting it unless you wrote it earlier
  in this task. Entries whose `source` is not yours belong to the user or
  another workflow; leave them alone unless the request names them.
- Delete only the entries the request names.

`status` in the result tells you what happened. If a write reports an error,
follow
[errors-and-exit-codes.md](errors-and-exit-codes.md#writes-that-succeeded)
before repeating it.

## Report the result

Lead with the answer. Cite the events that decide it — summary, start time
and calendar — and the window you queried. State what limits the conclusion:
a `stale` notice you did not clear, `partial_coverage` or `expansion_truncated`,
a `has_more` you did not follow, a `--status` filter. Keep what the store shows
apart from what you infer from it; an event on the calendar does not prove the
meeting took place.

For a write, name the entry and its new value. Leave out raw JSON, command
transcripts, and descriptions or attendee lists the question did not need.
