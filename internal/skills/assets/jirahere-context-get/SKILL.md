---
name: jirahere-context-get
description: >-
  Drive `jirahere context get` to validate a Jira work item and assemble a
  read-only picture of where it sits in the hierarchy — the item itself and,
  by level, its parent, full ancestor chain, immediate children, or full
  descendant subtree — as Markdown or JSON. Nothing in Jira is mutated. Use
  when an agent needs to understand or reason about an item's place in the
  tree (its parents, its children, what lives underneath it) before planning
  or performing other work, or when it needs that structure as machine input.
---

# jirahere context get

## Purpose

`jirahere context get` validates that a work item exists and prepares
hierarchy context around it for an agent or a human to read. It is a pure
**read**: it only issues GET / search requests to Jira and never creates,
edits, moves, transitions, links, or deletes anything. Running it repeatedly
is safe and has no side effects.

Reach for it when you need to know where an item sits — its parent and
ancestors, its immediate children, or its whole descendant subtree — rather
than to change anything. To actually reparent or rewrite items, use `move`
or `rehome-children`.

## Invocation

```
jirahere context get <key> [--level {0,1,2,3}] [--json | --md]
```

`jirahere context get <key> --help` prints the authoritative flag listing to
stdout and exits 0; treat that output as the source of truth for the exact
flag names and defaults. (`jirahere context get --help` with no key only
prints the one-line usage synopsis and exits non-zero — the positional is
validated before the help flag is seen.)

### `<key>` positional

Exactly one positional argument, required: the Jira issue key of the target
item (for example `PROJ-123`). It must be well-formed — a project part that
is an uppercase letter followed by zero or more uppercase letters, digits, or
underscores, then a hyphen and a run of digits. A digit- or underscore-led
project part is rejected. A missing key, a malformed key, or more than one
positional argument is a pre-flight failure: nothing is fetched and the
command exits non-zero.

### `--level` 0–3

Selects how much of the surrounding hierarchy is assembled. Defaults to `0`.
Each level is additive over the one below it, except that level 2 and level 3
each extend level 1 in different directions (level 2 walks up, level 3 walks
down; neither includes the other's walk).

| Level | Adds | Fetches |
|---|---|---|
| 0 | The target item alone — no relations. | The target only. |
| 1 | The **immediate parent** (one hop, shallow) and the **immediate children** (one hop, shallow). | Target, its parent field, its direct children. |
| 2 | Everything in level 1, plus the **full ancestor chain** from the target up to the root item (the one with no parent), nearest-parent-first, each node shallow. | Level 1, plus one relation-only fetch per ancestor. Bounded by a fixed 10,000-ancestor ceiling, a fixed 1 MiB rendered-ancestor-output budget, and a 2-minute overall deadline for the supplementary reads. |
| 3 | Everything in level 1, plus the target's **full descendant subtree** — every child, grandchild, and deeper — each node shallow, carrying its `depth` and `parentKey`. Level 3 does **not** walk upward, so it has no ancestor chain. | Level 1, plus a paginated descendant walk, bounded by a fixed 10,000-node ceiling. A failure on one branch is reported in-band (see below), not treated as a command failure. |

A `--level` value that is non-numeric or outside 0–3 is a pre-flight failure.

### `--json` / `--md` output selection

`--json` and `--md` choose the render format and are mutually exclusive.
`--md` is the default when neither is given. Passing both is a pre-flight
failure.

- `--md` — human- and agent-readable Markdown.
- `--json` — a single stable JSON object, for machine consumption.

## Auth precondition

Before running this skill's command, check local login state with
`jirahere status` (or `jirahere status --json` for a machine-readable
result). That check is local only — it makes no network call, has no side
effects, and is safe to run repeatedly.

If it reports not logged in — `Not logged in. Run "jirahere login" first.`
in text form, or `"logged_in": false` in the `--json` object — stop: do not
run the skill's command. Tell the user to run `jirahere login` first and
then retry. Never run `jirahere login` yourself; login is the user's
responsibility and this skill only surfaces the remediation.

This is an earlier, cleaner stop, not the only safeguard: the underlying
command still enforces auth itself, so any per-command auth failure-mode
rows elsewhere in this skill stay exactly as they are.

## Output shape per level

Relations other than the target render as a **shallow** node — `key`, `type`,
`summary`, `status` only; no description or labels.

### `--md`

- Title line: `# <key> — <summary> (<type>, <status>)`.
- `Labels: <comma-separated>` — only when the item has labels.
- `## Description` section — only when the item has a non-empty description.
- `Parent: <key> — <summary> (<type>, <status>)` — level 1+ only, and only
  when the item has a parent.
- `## Ancestors` — level 2 only, and only when the chain is non-empty; one
  bullet per ancestor, nearest parent first.
- `## Children` — level 1+ only. At levels 1 and 2 it is a flat one-bullet-
  per-child list. At level 3 it is the full descendant subtree rendered as an
  indented tree: the target's immediate children sit flush-left and each level
  deeper adds two spaces. Omitted when there are no children.
- `## Descendant walk failures` — level 3 only, and only when a branch could
  not be walked; one bullet per unreachable subtree with its lineage and a
  body-free reason.

A section is omitted entirely when it is empty or absent. Its absence means
"there is none", never "it was not fetched" — the level table above says what
each level fetches.

### `--json`

One object with these keys always present:

- `level` — the requested level.
- `item` — the full target: `{key, type, summary, status, labels, description}`.
  `labels` is always an array (`[]` when none); `description` is `""` when
  absent.
- `parent` — a shallow node, or `null`.
- `ancestors` — an array of shallow nodes (nearest first) at level 2; `null`
  at every other level.
- `children` — an array of shallow nodes at level 1+; `null` at level 0.
- `descendants` — at level 3, an array of `{node: {key, type, summary,
  status}, depth, parentKey}` envelopes; `null` at every other level.
- `descendantFailures` — at level 3, an array of `{key, parentKey, depth,
  reason}` for each branch the walk could not finish; `null` at every other
  level.

`null` means the relation was not fetched at this level. A non-null empty
array (`[]`) means it was fetched and none were found.

## Failure modes and exit behavior

Every failure prints one line to stderr and exits non-zero (this CLI uses
exit code 1 for all failures). A successful run and a `--help` request exit
0. Nothing is ever written to Jira on any path.

| Condition | Message (stderr) | When |
|---|---|---|
| No `<key>`, or more than one positional | `usage: jirahere context get <id> [--level {0,1,2,3}] [--json\|--md].` | pre-flight, nothing fetched |
| Malformed `<key>` | `jirahere: context get: <id> must be a well-formed Jira issue key.` | pre-flight, nothing fetched |
| `--level` non-numeric or outside 0–3 | `jirahere: context get: --level must be one of 0, 1, 2, or 3.` | pre-flight, nothing fetched |
| `--json` and `--md` together | `jirahere: context get: --json and --md may not be used together.` | pre-flight, nothing fetched |
| Unknown or malformed flag | `jirahere: <parser detail>` | pre-flight, nothing fetched |
| Not logged in | `jirahere: not logged in. Run "jirahere login" first.` | before contacting Jira |
| Stored credentials unreadable | `jirahere: could not load Jira credentials. Run "jirahere login" again.` | before contacting Jira |
| Credential refresh failed | `jirahere: could not refresh Jira credentials[ (<status>)]. Run "jirahere login" again.` | before contacting Jira |
| Target item does not exist | `jirahere: context get <key>: item was not found (404).` | after existence check |
| Target existence check failed otherwise | `jirahere: context get <key>: could not validate item existence: <reason>.` | after existence check |
| Immediate children could not be listed | `jirahere: context get <key>: could not list immediate children: <reason>.` | levels 1–3; at level 2 the 2-minute deadline also bounds this fetch and a hit here surfaces with this message |
| Ancestor chain unreadable, or it hit the 10,000-node ceiling, the 1 MiB output budget, or the 2-minute deadline | `jirahere: context get <key>: could not read ancestor chain: <reason>.` | level 2 (the 2-minute deadline covers all level-2 supplementary reads; only a hit during the ancestor walk surfaces with this message) |
| Descendant walk did not complete | `jirahere: context get <key>: descendant walk did not complete.` | level 3 |

At level 3, a failure to walk a **single** branch is not a failure mode: the
command still exits 0, emits the partial subtree, and enumerates each gap
(with lineage) in `descendantFailures` / the `## Descendant walk failures`
section.
