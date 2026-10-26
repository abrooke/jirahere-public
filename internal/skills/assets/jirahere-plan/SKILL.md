---
name: jirahere-plan
description: >-
  Drive `jirahere plan` to resolve the current quarter's plan epic — the one
  work item whose title carries the `_Plan` marker — and render it as
  Markdown or JSON, in the same shape as `jirahere context get` at level 0,
  and `jirahere plan init` to create that epic when the quarter has none.
  `plan` mutates nothing; `plan init` is the one write and is idempotent. Use
  when the agent needs the quarter's plan epic and holds no key for it, or
  needs to ensure one exists, instead of listing the quarter, eyeballing
  titles, and guessing which item is the plan.
---

# jirahere plan

## Purpose

`jirahere plan` answers one question: which work item is the current
quarter's plan epic. In a single call it:

1. Lists the configured current quarter's work items and finds the one whose
   title carries the `_Plan` marker.
2. Fetches that epic in full and renders it.

`jirahere plan init` answers the follow-up: make sure the quarter has one.
It looks for the plan epic and, only when there is none, creates it.

Reach for these when the agent needs the quarter's plan epic and has no key
for it in context, or must be sure one exists before relying on it. Without
them, the agent would have to run `quarter list` and spot the plan epic by
title by hand.

## Invocation

```
jirahere plan [--json | --md]
jirahere plan init [--dry-run]
```

`jirahere plan --help` and `jirahere plan init --help` print the
authoritative flag listings to stdout and exit 0; treat that output as the
source of truth for exact flag names and defaults (including any profile
selection flag). This skill describes intent and failure modes, not an
exhaustive flag table.

There is no key argument and no `--level` flag; the quarter and project come
from settings.json (see Config precondition). A stray positional argument is
a pre-flight failure. `plan` also has `plan context` and `plan save`; they
are not covered here.

### `jirahere plan` — read

A pure **read**: it only issues search and GET requests to Jira and never
creates, edits, moves, transitions, links, or deletes anything. Running it
repeatedly is safe. `--json` and `--md` choose the render format and are
mutually exclusive; `--md` is the default. Passing both is a pre-flight
failure: nothing is fetched and the command exits non-zero.

### `jirahere plan init` — the one write

Checks the current quarter for a plan epic using a fresh read (the cached
inventory is bypassed, so a stale cache cannot hide an existing epic), then:

- **Exactly one exists** — reports its key, writes nothing, exits 0.
- **None exists** — creates one new epic in the configured project, titled
  for the current quarter and carrying the quarter label, and prints the
  result.
- **Several exist** — refuses, names every matching key, writes nothing.

It is **idempotent**: it creates only when the plan epic is absent, so
running it again after success finds the epic and writes nothing. It is the
only mutating command in this skill.

`--dry-run` reports what would be created, or that the epic already exists,
and never writes. It is the safe way to preview.

Before running `plan init` without `--dry-run`: state the exact command and
what it would create, and get the user's go-ahead. Running with `--dry-run`
first is the cheapest way to show the user the effect. Do not run the real
write on inference alone.

## Auth precondition

Before running either command, check local login state with
`jirahere status` (or `jirahere status --json` for a machine-readable
result). That check is local only — it makes no network call, has no side
effects, and is safe to run repeatedly.

If it reports not logged in — `Not logged in. Run "jirahere login" first.`
in text form, or `"logged_in": false` in the `--json` object — stop: do not
run the skill's command. Tell the user to run `jirahere login` first and
then retry. Never run `jirahere login` yourself; login is the user's
responsibility and this skill only surfaces the remediation.

This is an earlier, cleaner stop, not the only safeguard: the underlying
commands still enforce auth themselves, so any per-command auth failure
rows below stay exactly as they are.

## Config precondition

Two settings.json defaults must be set: `defaults.project` and
`defaults.current_quarter`. If either is unset the command fails before
contacting Jira with one line naming the missing key; settings.json is the
fix. Both commands always target the current quarter.

## Output shape

`jirahere plan` output is byte-identical in shape to
`jirahere context get <planKey>` at its default level 0, where `<planKey>` is
the epic the command found: that item alone, with no parent, ancestors,
children, or descendants. For the field-level detail of both the `--md` and
`--json` formats, see the `jirahere-context-get` skill rather than restating
it here. The epic's key appears in the output (the Markdown title line, or
`item.key` in JSON); read it from there when a follow-up is needed.

`jirahere plan init` prints plain text, not that shape: either the existing
epic's key, a dry-run description, or the result of the creation.

## Failure modes and exit behavior

Every failure prints one line to stderr and writes nothing to stdout. A
successful run and a `--help` request exit 0. Usage errors exit 2; all other
failures exit 1. Read the stderr line for the exact wording rather than relying
on this skill to restate it.

| Condition | Meaning | Remedy |
|---|---|---|
| No plan epic found (`plan`) | No work item in the current quarter carries the `_Plan` marker in its title. | Offer `jirahere plan init` — preview with `--dry-run`, then get the user's go-ahead before the real run. |
| Ambiguous plan epic (`plan`, `plan init`) | More than one work item in the quarter matches, so the command refuses to pick; the message names every matching key. `plan init` creates nothing. | Tell the user to keep exactly one plan epic for the quarter; use the named keys to see which candidates exist (for example with `jirahere context get <key>`). |

Failures shared with other commands:

| Condition | When |
|---|---|
| Unknown or malformed flag, a stray positional argument, or `--json` with `--md` | pre-flight, nothing fetched |
| settings.json could not be loaded | pre-flight, nothing fetched |
| `defaults.project` or `defaults.current_quarter` unset | before contacting Jira |
| Not logged in, stored credentials unreadable, or credential refresh failed | before contacting Jira; each names `jirahere login` as the fix, so tell the user to run it |
| The quarter's inventory could not be listed | during the epic lookup |
| The plan epic was not found (404) or could not be read | `plan` only, after the epic is resolved |
| The epic could not be created | `plan init` only; follows `jirahere create`'s failure behavior — see the `jirahere-create` skill, and check the quarter again before retrying |

Both commands resolve the epic fresh on every call, so a failure in one call
says nothing about the next; fix the cause and retry. Because `plan init` is
idempotent, retrying it after an unclear failure is safe: it will find an
epic that was created and write nothing further.

## Relation to other skills

- `quarter list` (skill `jirahere-quarter-list`) is the flat inventory of a
  quarter; use it when the agent needs the whole quarter, not just the plan
  epic.
- `context get` (skill `jirahere-context-get`) renders any item at levels
  0–3. `plan` covers only the level-0 view of the plan epic. For its
  children or any level above 0, take the key from the `plan` output and run
  `jirahere context get <key>` at the level needed.
- `create` (skill `jirahere-create`) is the general-purpose creator that
  `plan init` uses for its single write.
