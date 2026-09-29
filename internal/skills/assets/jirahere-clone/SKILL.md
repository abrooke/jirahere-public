---
name: jirahere-clone
description: >-
  Clone one Jira work item into the same project and issue type, optionally
  rewriting one summary substring, swapping one label, assigning a parent, or
  advancing a [Part N] series. Use when a new item should retain the source's
  description and labels while being linked back to that source; do not use it
  for arbitrary bulk copying or general field editing.
---

# jirahere clone

## Purpose

Use `jirahere clone` to create one new Jira work item from a source item. It
keeps the source's project, issue type, description, and labels (subject to
the optional label swap). It always attempts to create a `Cloners` issue link:
the new item is shown as a clone of the source, and the source is shown as
cloned by the new item. This link is distinct from an optional parent.

This is a primitive, not a quarter workflow or a general copy tool. Compose it
with other commands when more than one text or label edit is needed. A repeated
successful invocation creates another item; cloning is not idempotent.

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
jirahere clone <key> [flags]
```

You must be logged in before running this. The command needs stored Jira
credentials; a missing, unreadable, or unrefreshable login fails before it
contacts Jira.

Run `jirahere clone --help` for the authoritative, shipped list of exact flag
names and usage. In particular, it includes:

- `--summary-old` with `--summary-new`: replace every literal,
  case-sensitive occurrence of one substring in the clone's summary.
- `--label-old` with `--label-new`: replace one source label on the clone.
- `--parent`: set a parent only on the clone. The source's parent is never
  inherited.
- `--part`: advance a `[Part N]` marker in the clone's summary. It cannot be
  combined with either summary-replacement flag. With no existing marker, it
  creates a clone ending in `[Part 2]` and, after creation, attempts to append
  `[Part 1]` to the source; the command reports that source write separately.

The summary and label options are paired, and their value-taking flags are not
repeatable. `--label-old` must exist on the source; `--label-new` must be
non-empty and contain no whitespace. Other source labels carry over unchanged.
Jira validates any remaining label rules during creation.

## Read the result

On a successful create, stdout contains a line in this form:

```text
Created <new-key> (<type>: "<summary>").
```

Read `<new-key>` from that `Created` line. It is printed even if a later link,
parent, or unnumbered-`--part` source-update step fails, so retain it for any
manual repair. `Done.` appears only when all attempted post-create steps
succeed.

## Failures and partial results

`--help` exits 0. Every failure below exits nonzero (currently status 1).
Failures before creation leave no clone; their diagnostic is printed to stderr.

| Condition | Result |
| --- | --- |
| Missing or extra `<key>`, unknown or malformed flags, a repeated value-taking flag, unmatched summary/label pair, or `--part` combined with a summary flag | Usage/validation failure; no Jira write. |
| Not logged in, credentials cannot be loaded, or credentials cannot refresh | Run `jirahere login` (again where indicated); no Jira contact or write. |
| Source or requested parent is not found | No clone is created. |
| Requested summary substring or old label is absent; new label is empty or has whitespace | No clone is created. |
| Jira cannot be reached during pre-flight, or the site has no `Cloners` link type | No clone is created; retry the reachability failure or have an admin restore `Cloners`. |
| Jira rejects or cannot complete the create request after pre-flight (including a label rule Jira alone enforces) | No clone is created. |
| The clone is created but the `Cloners` link fails | The clone exists but is unlinked. Add the `Cloners` link between the new key and source manually or in Jira's UI. |
| The clone is created but the requested parent update fails | The clone exists without that parent. Set a valid parent later. |
| `--part` found no marker and the source-summary update fails | The clone exists with `[Part 2]`; add `[Part 1]` to the source summary manually if needed. |

The three post-create steps are independent: a link failure does not prevent a
requested parent update or the unnumbered-`--part` source update, and more than
one can fail in the same run. They are not rolled back. Inspect all printed
outcome lines, keep the new key, and repair each reported step.
