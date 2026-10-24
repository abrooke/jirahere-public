---
name: jirahere-plan-save
description: >-
  Drive `jirahere plan save` to write an approved day, week or month plan into
  Jira as one work item titled `Plan <Kind> <YYYYMMDD>` under the current
  quarter's plan epic. An idempotent upsert: the first run creates the item,
  re-running updates the same item, and on update the description is replaced
  wholesale. A write. Use when the user has approved saving a synthesized
  plan; use `jirahere plan context` to gather the facts and `jirahere plan
  init` to create the plan epic first.
---

# jirahere plan save

## Purpose

`jirahere plan save` stores a finished plan report in Jira. It reads the plan
body from a file or stdin and upserts one work item for the window:

- The item is a Task titled exactly `Plan <Kind> <YYYYMMDD>` — `Day`, `Week`
  or `Month`, then the window's start date — for example `Plan Week 20261017`.
  It lives under the current quarter's plan epic (the `_Plan` item) and
  carries the current quarter's label.
- **No such item yet** — it is created, with the body as its description.
- **One such item** — its description is replaced with the body. If it has no
  parent, the plan epic is set as its parent. Nothing else on it is touched.
- **Several such items** — the command fails and writes nothing.

Re-running with the same kind and date therefore updates the same item; it
never makes a second one. The command looks for an existing item in a freshly
fetched quarter inventory, never the cache, so a recent save is always seen.

It saves text; it does not write the plan. Gathering facts is `plan context`
(skill `jirahere-plan-context`) and deciding what goes in the plan is the
agent's job, with the user's approval, before this command runs.

## Invocation

```
jirahere plan save <day|week|month> --body-file <path|-> [--date YYYY-MM-DD] [--profile <name>]
```

`jirahere plan save --help` prints the authoritative flag listing to stdout
and exits 0; treat that output as the source of truth for exact flag names
and defaults. This skill describes intent and failure modes, not an
exhaustive flag table.

- `<day|week|month>` — the one positional argument, required. It picks the
  `Day`, `Week` or `Month` in the title. Anything else, or a missing or extra
  positional, fails.
- `--body-file <path|->` — required. Reads the plan text from the file at
  `<path>`, or from stdin when the value is `-`. There is deliberately **no
  inline body flag**. The file is UTF-8, at most 1 MiB; one leading BOM and
  trailing newlines are dropped, and an empty or whitespace-only file is
  rejected.
- `--date YYYY-MM-DD` — the window's start date; default today. It sets the
  `<YYYYMMDD>` in the title, so the same plan saved for a different `--date`
  is a different item. Any other date form is rejected.
- `--profile <name>` — run against that profile's login, settings and cache;
  omitted, the default profile is used.

### The body is plain text

The body is stored as plain text. **No markdown is interpreted** — `#`, `*`,
`-`, backticks and `[links](...)` are written as literal characters. A blank
line starts a new paragraph and a single newline becomes a hard line break.
Write the report accordingly: no headings, lists, tables or links will render.

### Give the body by file or stdin

Prefer piping the report on stdin with `--body-file -`. Otherwise write it to
a file in your own scratch directory or one created with `mktemp`; never a
predictable name in a shared `/tmp`, which invites symlink and race problems
and may be world-readable. Do not try to squeeze a multi-paragraph report into a
shell-quoted argument; there is no such argument. The file or pipe avoids all
quoting and expansion problems for the body. The persona's single-quote rule
still applies to the short values — the path, the kind and the date.

```text
jirahere plan save week --body-file - --date '2026-10-17'
```

## A write: get the go-ahead first

This command mutates Jira, and a later run replaces what an earlier one wrote.

1. **Check login** with `jirahere status` (see Auth precondition).
2. **Show the user the plan text** you intend to save and the item it will
   land in: kind, window start date and the resulting title.
3. **State the exact command** and **wait for the user's explicit
   go-ahead.** Approval of the plan's content, or of drafting it, is not
   approval to run the command.
4. **Run it once**, then read the whole result before doing anything else.
5. **Report the outcome** from the success output or the diagnostic. Never
   say a plan was saved unless a success line was printed.

There is no dry run, no rollback and no delete command.

## Replacement is wholesale

On update, the item's whole description is overwritten with the new body.
Nothing of the old text is merged or kept, so **any edit made to that
description in Jira since the last save is lost**, along with anything plain
text cannot hold — lists, headings, tables, panels, links. The old text is
recoverable only from Jira's own history, if at all. Before re-saving a plan
that may have been edited in Jira, say so to the user, and have them copy
what they want to keep. To add text without replacing anything, use
`jirahere comment add` on the item instead.

## Auth precondition

Before running the command, check local login state with
`jirahere status` (or `jirahere status --json`; `--profile <name>` for a
named profile). That check is local only — it makes no network call, has no
side effects, and is safe to run repeatedly.

If it reports not logged in — `Not logged in. Run "jirahere login" first.`
in text form, or `"logged_in": false` in the `--json` object — stop: do not
run `plan save`. Tell the user to run `jirahere login` first and then retry.
Never run `jirahere login` yourself; login is the user's responsibility.

This is an earlier, cleaner stop, not the only safeguard: the command still
enforces auth itself. A missing login prints
`jirahere: not logged in. Run "jirahere login" first.` and exits 1 before
Jira is contacted.

## Config precondition

The settings.json defaults for the project and the current quarter must be
set. If one is unset the command fails before writing anything with one line
naming the missing key and the remediation (a `jirahere configure set`
command); follow the remediation the diagnostic names.

## Success output and exit behavior

On success the command exits 0 and prints to stdout:

- **Created:** the new item's key on one line, then `Done.`
- **Updated:** one line, `Plan saved: <KEY> (updated)`. There is no `Done.`
  line on update.

Every failure prints its diagnostic to stderr, usually one line, and exits
non-zero. Read the stderr line for the exact wording rather than relying on
this skill to restate it.

## Failure modes and exit behavior

Usage errors — a malformed invocation: flag-parser errors, a missing or extra
kind, an unknown kind, a missing `--body-file`, or an invalid `--date` — exit 2;
every other failure exits 1, including an unreadable or empty body file.
Everything in the first four rows below is checked before settings, credentials or
Jira are touched, so nothing is written.

| Condition | Meaning and remedy |
|---|---|
| Missing kind, extra positional, or kind not `day`, `week` or `month` | Usage diagnostic; fix the invocation. |
| `--body-file` not given | `jirahere: plan save: --body-file is required.` plus usage. |
| Body file unreadable, over 1 MiB, not UTF-8, or empty or whitespace-only (stdin: `body from stdin ...`) | `jirahere: plan save: body file "<path>" <reason>.` Fix the file or the pipe. |
| `--date` not `YYYY-MM-DD`, unknown flag, or invalid `--profile` | Diagnostic naming the problem; fix the invocation. |
| settings.json could not be loaded, or project / current quarter default unset | One line naming the cause; before Jira is contacted. |
| Not logged in, stored credentials unreadable, or credential refresh failed | Each names `jirahere login` as the fix; tell the user to run it. |
| The quarter inventory could not be fetched | `jirahere: plan save: <reason>.`; nothing written. Fix the cause and retry. |
| **No plan epic** in the current quarter | `jirahere: plan save: no plan epic ... Run "jirahere plan init" to create one.` Run `plan init` (skill `jirahere-plan`, itself a write needing a go-ahead), then retry. |
| **More than one plan epic** | The message names the keys; the user must keep exactly one. The command will not pick. |
| **Duplicate plan items** — several items already titled `Plan <Kind> <YYYYMMDD>` | `jirahere: plan save: <n> work items are titled "<title>" (<keys>); keep exactly one.` Nothing is written. The user must resolve the duplicates in Jira; do not guess which to update. |
| Creating the item failed | `jirahere: create ...`-style diagnostic; nothing was created, so retrying is safe once the cause is fixed. |
| On update: reading the item, replacing its description, or setting its parent failed | `jirahere: plan save: could not ...: <reason>.`, naming the key. The description may or may not have changed (the parent step comes after it); re-running is safe and repeats the upsert. |

Jira failures are rendered body-free: the diagnostic may name the operation
and an HTTP status, but never exposes response bodies, credentials, request
payloads or arbitrary transport-error text.

## Partial failure after create

Creating the item and putting it under the plan epic are two independent
Jira writes. If the item is created but the parent write then fails, the
command prints the new key on stdout, then fails with:
`jirahere: plan save: created <KEY> but could not set <EPIC> as its parent: <reason>. Re-run the same plan save to repair the parent.`
and exits 1, with no `Done.`

The item **exists** and is not rolled back; there is no delete command. So:

- **Keep the printed key** and tell the user it exists but is not yet under
  the plan epic.
- **Re-run the same `plan save`** (same kind, same `--date`, same body). The
  next run finds the item by its title, takes the update path, and sets the
  missing parent.
- **Never create another item by other means** — not `jirahere create`, not
  Jira's UI. That would leave a duplicate title, and `plan save` then refuses
  to run until the user removes one.

## Relation to other skills

- `plan context` (skill `jirahere-plan-context`) gathers the facts a plan is
  built from; run it before drafting the plan this command saves.
- `plan` and `plan init` (skill `jirahere-plan`) resolve and create the plan
  epic that `plan save` files under.
- `description set` (skill `jirahere-description-set`) replaces one item's
  description by key; `plan save` is the right tool for plan items because it
  finds or creates the item by title.
- `comment add` (skill `jirahere-comment-add`) adds text without replacing
  anything.
