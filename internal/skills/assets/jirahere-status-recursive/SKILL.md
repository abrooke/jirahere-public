---
name: jirahere-status-recursive
description: >-
  Roll a Jira work item's descendants' statuses up into its own and every
  non-leaf descendant's status, deepest first, one direct transition each.
  Use when a parent's status should follow its children's; supports a
  --dry-run preview. Not for setting one item to a chosen status.
---

# jirahere status recursive

## Purpose

Use `jirahere status recursive` to make parent statuses follow their
children. The command walks the full descendant tree of `<id>`, computes a
target status for every **non-leaf** node from its children's statuses using
the ordered rules below, and transitions each node whose computed status
differs from its current one. This is a write (unless `--dry-run`).

Scope: `<id>` itself and every non-leaf descendant. Leaves (items with no
children) are never changed. Nothing above `<id>` is read or changed, even if
`<id>` has a parent.

For setting one item to a status you choose, use `jirahere status set`.

## Invocation

```text
jirahere status recursive <id> [--dry-run] [--profile <name>]
```

`<id>` is the one required positional argument and must be a well-formed Jira
issue key. `--dry-run` is an optional boolean. `--profile <name>` selects a
configured profile, as in other jirahere commands.

`jirahere status recursive --help` prints the status-group synopsis plus a
short `--dry-run` note, not a per-flag listing (`jirahere status --help`
prints the synopsis only); the arguments above are stated here instead of
being deferred to `--help`.

## Auth precondition

Before running this command, check local login state with `jirahere status`
(or `jirahere status --json`), every time. This check is local only: no
network call, no side effects. If it reports not logged in, stop and tell
the user to run `jirahere login`, then retry. Never run `jirahere login`
yourself.

The command also enforces auth: a missing login, unreadable stored
credentials, or a credential-refresh failure exits nonzero before Jira is
contacted.

## Rollup rules

For each non-leaf node, take its children's effective statuses and apply
these rules in order; the first match wins. Status names compare
case-insensitively and exactly (no trimming or mapping).

1. All children `Done` -> `Done`.
2. Some children `Done` (not all) -> `In Progress`.
3. Any child `In Progress` -> `In Progress`.
4. Any child `To Do` -> `To Do`.
5. All children `Backlog` -> `Backlog`.
6. Otherwise **no match**: the node is left unchanged.

A child's effective status is its computed target when a rule matched, or its
current status when none did. A non-leaf child contributes its computed
status, never its pre-run Jira status. A no-match node keeps its current
status and contributes that to its own parent.

Statuses outside `Done`, `In Progress`, `To Do` and `Backlog` count toward no
bucket: `[Done, Review]` -> `In Progress`, `[To Do, Review]` -> `To Do`, and
`[Review]` or `[Backlog, Review]` -> no match.

## Write semantics

Nodes are processed deepest first (children before parents, `<id>` last).
For each node whose computed status differs from its current status, the
command makes one direct by-name transition, exactly as `jirahere status set`
does: the computed status must be directly reachable from the node's current
status in Jira's workflow. It never chains transitions through intermediate
statuses. Equal or no-match nodes get no write.

## `--dry-run`

`--dry-run` runs the same walk and rule evaluation but makes **no writes**:
it does not list or apply transitions, so it does **not** check whether each
transition is reachable. A dry run that looks clean can still be followed by
a real run that fails on a node with no direct transition to its computed
status. Its pre-write failures (below) behave exactly as in a real run.

## Output

All output is on stdout; a complete run exits 0.

```text
Rolling up <id> ...
Status set: <key> -> <status>
Unchanged <key> (<status>).
No rule matched <key>.
Done.
```

The header is `Rolling up <id> (dry run) ...` under `--dry-run`. Then one line
per non-leaf node, in processing order (deepest first):

- `Status set: <key> -> <status>`: transitioned (real run).
- `Would set: <key> <current> -> <target>`: would transition (dry run only).
- `Unchanged <key> (<status>).`: computed status equals the current one.
- `No rule matched <key>.`: rule 6; left unchanged.

The final line is `Done.`. A real run's `Status set:` lines are one per
write; a dry run prints `Would set:` lines instead and never `Status set:`.

## Failure modes and exit behavior

Failures print one diagnostic line to stderr. Usage errors exit 2; all other
failures exit 1.

Pre-write failures (no header, empty stdout, nothing changed):

| Condition | Result |
| --- | --- |
| Missing or extra `<id>`, unknown or malformed flag, missing flag value | Usage diagnostic, exit 2. |
| `<id>` not a well-formed issue key | `status recursive: <id> must be a well-formed Jira issue key`, exit 2. |
| Invalid or empty `--profile` | Profile diagnostic, exit 2. |
| Not logged in, credentials unreadable, or refresh fails | Login remediation diagnostic. |
| `<id>` cannot be read (missing, or Jira unreachable) | `status recursive could not read "<id>": ...` |
| Descendant walk fails | `status recursive could not walk the descendants of "<id>": ...` |
| A branch's children cannot be listed | `status recursive could not list the children of "<key>": ...` |

Write-time failures (real run only), after the header and zero or more
`Status set:` lines:

| Condition | Result |
| --- | --- |
| Listing a node's transitions fails | `status recursive could not list available transitions for "<key>": ...` |
| No transition targets the computed status | `status recursive: "<status>" is not an available status for "<key>"; available: ...` |
| More than one transition targets it | `status recursive: "<status>" is ambiguous for "<key>" (N matching transitions); no transition was made.` |
| The matched transition fails to apply | `status recursive could not transition "<key>" to "<status>": ...` |

Jira failures are body-free: they may name the operation and an HTTP status
(`could not reach Jira`, or `Jira call failed unexpectedly`) but never response bodies, credentials, or
request payloads.

## Reading a partial failure

A write-time failure aborts the run at the first failing node. Writes already
made are **not** rolled back, and the nodes after the failing one were not
attempted. The signs of a partial run: stdout has the `Rolling up` header and
some `Status set:` lines but **no `Done.`**, and the command exits 1 with the
diagnostic on stderr.

- Every `Status set:` line printed stands; those transitions landed.
- The node named in the stderr diagnostic was not changed; it and everything
  after it (shallower nodes, ending with `<id>`) were not processed.
- Never blind re-run. First run `jirahere status recursive <id> --dry-run`
  to see what the tree now needs: landed nodes show `Unchanged`, the rest
  show `Would set:`. Fix the cause (for example, a status with no direct
  transition must be reached by hand, or via `jirahere status set` through
  an intermediate status), then re-run for real. Remember a dry run does
  not check reachability.
