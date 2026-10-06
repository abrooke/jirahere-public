---
name: jirahere-vision
description: >-
  Drive `jirahere vision` to resolve the current quarter's active Vision item
  — the latest-created not-Done direct child of the quarter's `_Vision`-titled
  epic — and render it as Markdown or JSON, in the same shape as
  `jirahere context get` at level 0. Nothing in Jira is mutated. Use when the
  agent needs the current quarter's active Vision item and holds no key for
  it, instead of listing the quarter, eyeballing titles, and guessing which
  item to pass to `context get`.
---

# jirahere vision

## Purpose

`jirahere vision` answers one question: what is the active Vision item for
the current quarter. In a single call it:

1. Lists the configured current quarter's work items and finds the epic whose
   title carries the `_Vision` marker.
2. Lists that epic's direct children and picks the latest-created child that
   is not Done.
3. Fetches that child in full and renders it.

It is a pure **read**: it only issues search and GET requests to Jira and
never creates, edits, moves, transitions, links, or deletes anything. Running
it repeatedly is safe and has no side effects. It keeps no cache of its own;
each call re-resolves the epic and its children.

Reach for it when the agent needs the current quarter's active Vision item
and has no key for it in context. Without it, the agent would have to run
`quarter list`, spot the Vision epic by title, list its children, and choose
one by hand. Only the active item is returned — not the epic, and not the
other children.

## Invocation

```
jirahere vision [--json | --md]
```

`jirahere vision --help` prints the authoritative flag listing to stdout and
exits 0; treat that output as the source of truth for the exact flag names and
defaults. This skill describes intent and failure modes, not an exhaustive
flag table.

There is no positional argument and no `--level` flag; the quarter and project
always come from settings.json (see Config precondition). A stray positional
argument is a pre-flight failure.

### `--json` / `--md` output selection

`--json` and `--md` choose the render format and are mutually exclusive.
`--md` is the default when neither is given. Passing both is a pre-flight
failure: nothing is fetched and the command exits non-zero.

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

## Config precondition

Two settings.json defaults must be set: `defaults.project` and
`defaults.current_quarter`. If either is unset the command fails before
contacting Jira with one line naming the missing key; settings.json is the
fix. `vision` always targets the current quarter — it has no
`--previous`/`--next`/`--label` counterpart.

## Output shape

The output is byte-identical in shape to `jirahere context get <activeKey>`
at its default level 0, where `<activeKey>` is the item the command selected:
the selected item alone, with no parent, ancestors, children, or descendants
(a `--md` document with the title line, an optional resolved-date line,
labels, and description; a `--json` object with `level` 0, the full `item`
— including `resolved` — and the relation keys `null`). For the field-level
detail of both formats, see the `jirahere-context-get` skill rather than
restating it here.

The selected item's key appears in the output (the Markdown title line, or
`item.key` in JSON). Read it from there when a follow-up is needed.

## Failure modes and exit behavior

Every failure prints one line to stderr, exits 1, and writes nothing to
stdout — in both `--md` and `--json` forms (`--json` changes only the success
payload). A successful run and a `--help` request exit 0. Nothing is ever
written to Jira on any path.

Three failures are specific to `vision` and each has its own message. Read
the stderr line for the exact wording and the rule it enforces, rather than
relying on this skill to restate it:

| Condition | Meaning | Remedy |
|---|---|---|
| No Vision epic found | No work item in the current quarter carries the `_Vision` marker in its title per the quarter's convention. | Tell the user: the quarter needs a Vision epic (title with the `_Vision` marker, quarter label applied). Creating one is a separate step, not something `vision` does. |
| Ambiguous Vision epic | More than one work item in the quarter matches, so the command refuses to pick; the message names every matching key. | Tell the user to keep exactly one Vision epic for the quarter; use the named keys to see which candidates exist (for example with `jirahere context get <key>`). |
| No active Vision item | A single Vision epic was found, but none of its direct children is outside Done (it has no children, or all are Done). The message names the epic key. | Tell the user the epic has no open child; a new not-Done child under that epic is needed. To inspect the epic itself, use `jirahere context get <epicKey> --level 1`. |

The remaining failures reuse the wording of `quarter list` and `context get`:

| Condition | When |
|---|---|
| Unknown or malformed flag, a stray positional argument, or `--json` with `--md` | pre-flight, nothing fetched |
| settings.json could not be loaded | pre-flight, nothing fetched |
| `defaults.project` or `defaults.current_quarter` unset | before contacting Jira |
| Not logged in, stored credentials unreadable, or credential refresh failed | before contacting Jira; each names `jirahere login` as the fix, so tell the user to run it |
| The quarter's inventory could not be listed | during the epic lookup |
| The Vision epic's children could not be listed | after the epic is resolved |
| The selected item was not found (404), or could not be read | after selection, before rendering |

Because `vision` resolves the epic and its children fresh on every call, a
failure in one call says nothing about the next; fix the cause and retry.

## Relation to `quarter list` and `context get`

- `quarter list` (skill `jirahere-quarter-list`) is the flat inventory of a
  quarter. `vision` runs the same inventory read internally, so use
  `quarter list` when the agent needs the whole quarter, not just the active
  Vision item.
- `context get` (skill `jirahere-context-get`) renders any item at levels
  0–3. `vision` covers only the level-0 view of the one item it selects. For
  the Vision epic itself, for a level above 0 (parent, children, ancestors,
  subtree), or for any other key, resolve the key first — from the `vision`
  output or a failure message — and then run `jirahere context get <key>`
  at the level that matches what is needed.
