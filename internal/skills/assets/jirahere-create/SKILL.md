---
name: jirahere-create
description: >-
  Create one new Jira work item from directly supplied fields — a required
  summary plus optional project, issue type, description, parent, and
  repeatable labels, with project and issue type falling back to configured
  defaults when omitted. Use when a work item must be made from scratch; use
  jirahere clone to copy an existing item and jirahere move to edit one in
  place.
---

# jirahere create

## Purpose

Use `jirahere create` to create exactly one new Jira work item from fields you
supply on the command line. Only `--summary` is required; project, issue type,
description, parent, and labels are optional, and the project and issue type
fall back to configured defaults when omitted.

This is a primitive, not a quarter workflow or a bulk tool. Compose it with the
other primitives when the new item needs follow-up edits or links. `create` is
not idempotent: run it twice and you get two work items.

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

## Invocation

```text
jirahere create --summary <text> [--project <key>] [--type <name>] [--description <text>] [--parent <key>] [--label <label>]... [--suppress-auto-quarter]
```

Run `jirahere create --help` for the authoritative, shipped list of exact flag
names and usage. Treat that output as the source of truth for exact flag
names; this skill describes intent and failure modes rather than duplicating a
flag table that will drift.

- `--summary` — required, non-empty. The new item's title.
- `--project <key>` — project to create in. When omitted, resolves to
  settings.json's `defaults.project`. With neither the flag nor the default,
  the command fails before contacting Jira and names both remedies.
- `--type <name>` — issue type name. When omitted, resolves to
  settings.json's `defaults.issue_type`. With neither the flag nor the
  default, the command fails before contacting Jira and names both remedies.
- `--description <text>` — plain text, converted to Jira's document format: a
  blank line starts a new paragraph, a single newline is a hard break, and no
  markdown is interpreted. Omit the flag for no description; passing it with an
  empty value is rejected locally.
- `--parent <key>` — an existing work item to set as the new item's parent.
  Its existence is checked during pre-flight; the link itself is a separate
  write made after the item is created (see `--parent` link partial failure).
- `--label <label>` — repeatable; pass it once per label. Each value must be
  non-empty and contain no whitespace. `create` builds the label set from
  scratch — it does not swap one label the way `clone` and `move` do — so
  repeat the flag for every label you want. Duplicates are collapsed, first
  occurrence wins. Any other Jira label rule is left to Jira, enforced when the
  create request is made.
- `--suppress-auto-quarter` — bool, default `false`. Opts out of the
  current-quarter auto-add described below: `defaults.current_quarter` is
  never appended, regardless of whether it is configured. The submitted label
  list becomes exactly the de-duplicated `--label` values you passed, and the
  success output's auto-added-quarter clause never appears. It adds no new
  "which quarter" selection — to file into a different quarter, pass that
  quarter's label yourself via `--label` alongside this flag.

## Current-quarter label

When settings.json's `defaults.current_quarter` is configured and non-empty,
`create` appends that label to the new item unless you already passed it as a
`--label`. The success output names it — `<label> was auto-added for the
current quarter.` — so you can tell which label the command supplied. An unset
or empty `defaults.current_quarter` is a no-op, and passing the quarter label
yourself as `--label` suppresses the auto-add note. `--suppress-auto-quarter`
suppresses it unconditionally, whether or not `defaults.current_quarter` is
configured.

## Read the result

On a successful create, stdout has this fixed shape:

```text
Created a new <type> in <project>.
<NEW-KEY>
Labels: <label>, <label>. <quarter-label> was auto-added for the current quarter.
Done.
```

The new issue key is printed alone on the second line — the line immediately
after `Created a new ... in ...` — so `... | sed -n 2p` reads it cleanly.
`Labels: none.` appears when the item has no labels; the auto-added-quarter
clause is present only when the command supplied that label.

When `--parent` was given, a parent line is added before `Done.`:
`Parent set: <NEW-KEY> -> <parent-key>.` on success. `Done.` is printed only
when every attempted step succeeded.

## `--parent` link partial failure

The create and the parent link are two independent Jira writes. If the item is
created but the parent link then fails, you see:

- the `Created a new ... in ...` line and the `<NEW-KEY>` line — the item
  exists;
- the `Labels: ...` line — always present, listing the item's labels or
  `Labels: none.`;
- `Could not set <parent-key> as <NEW-KEY>'s parent: <status>. Set one with
  the parent/child link primitive once you have a valid target.` — a body-free
  diagnostic that may or may not include an HTTP status code, and never a Jira
  response body;
- no `Done.` line.

The command exits nonzero. The created item is not rolled back — v1 has no
delete primitive. Keep the printed key and set the parent afterwards with the
parent/child link primitive; do not re-run `create`, which would make a second
item.

## Failures and exit behavior

`--help` prints to stdout and exits 0 without contacting Jira. Every failure
below exits nonzero (the CLI's failure exit is 1). Failures before the create
call leave no item and print one sanitized diagnostic to stderr.

| Condition | Result |
| --- | --- |
| Missing or empty `--summary`, a surplus positional argument, an unknown or malformed flag, or a missing flag value | Usage/validation diagnostic; no Jira contact or write. |
| A `--label` value is empty or contains whitespace | Validation diagnostic; no write. |
| `--description` is given with an empty value | Validation diagnostic; no write. |
| No `--project` and no `defaults.project`, or no `--type` and no `defaults.issue_type` | Config-resolution diagnostic naming both remedies; no Jira contact. |
| settings.json cannot be loaded | Diagnostic; no Jira contact. |
| Not logged in, stored credentials unreadable, or credential refresh fails | Login-remediation diagnostic; no Jira request or write. |
| The project's create metadata cannot be fetched (transport error or non-2xx) | Body-free "could not resolve project" diagnostic — an HTTP status code for a non-2xx response, none for a transport failure; no write. |
| The project offers no create metadata | "not available for creation" diagnostic; no write. |
| The resolved issue type is not one the project offers | Diagnostic listing the project's available issue types; no write. |
| `--parent` resolves to nothing (a definitive 404) | "parent ... was not found (404)" diagnostic; no write. |
| The `--parent` existence check fails for a non-404 reason | Body-free "could not validate parent" diagnostic — an HTTP status code for a non-2xx response, none for a transport failure; no write. |
| Jira rejects or cannot complete the create request after pre-flight (including a label rule only Jira enforces) | "create failed: <status>. Nothing was created." No item. |
| The item is created but the `--parent` link write fails | The item exists and its key is printed; body-free "Could not set ... parent" line with repair guidance; no `Done.`; exit nonzero. Not rolled back. |

Jira failures are rendered body-free throughout: output may name the
operation, safe issue keys, and an HTTP status, but never a response body,
credentials, or request payloads.
