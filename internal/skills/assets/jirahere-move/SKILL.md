---
name: jirahere-move
description: >-
  Reparent a Jira work item and/or rewrite its summary and labels together
  with matching descendants, in place. Use when a work-item subtree changes
  parent, naming convention, or quarter label without creating a replacement.
---

# jirahere move

## Purpose

Use `jirahere move` to reparent one existing work item, rewrite a literal
substring in its summary, swap one label, or combine those operations. It
edits the target **in place**: it creates nothing and returns no new key.

Summary and label rewrites also apply to every discovered descendant at any
depth. The target alone can be reparented: descendants keep their existing
parents. A parent-only move does not walk descendants. Omitting `--parent`
means leave the target's parent unchanged; it does not detach the target.

## Invocation

```
jirahere move <source-key> [--parent <new-parent-key>] [--summary-old <text> --summary-new <text>] [--label-old <label> --label-new <label>]
```

Run `jirahere move --help` for the authoritative, shipped flag listing and
flag descriptions. Treat that output as the source of truth for exact flag
names; this skill describes behavior rather than duplicating a flag table.

`<source-key>` is the one required positional argument. The source key may
appear among the flags, but exactly one positional argument is required.

- `--parent` sets the target's new parent.
- `--summary-old` and `--summary-new` form a required pair. Every occurrence
  of the old literal in a matching summary is replaced with the new text.
- `--label-old` and `--label-new` form a required pair. The old label is
  replaced in matching label sets; unrelated labels are retained.

No move flag may be repeated. Supply at least one of `--parent`, the summary
pair, or the label pair.

## Descendant scope

For either rewrite pair, the command walks the full descendant tree:
children, grandchildren, and deeper items. Each matching descendant is
rewritten independently. A descendant whose summary lacks `--summary-old` or
whose labels lack `--label-old` is left untouched for that part and reported
as `no match`; that is not itself a failure. No descendant is reparented.

The source target is stricter: before any write, it must exist and must match
each requested summary or label rewrite. Its requested new parent must also
exist. This preflight prevents writes on an invalid target request.

## Auth precondition

Before running the command, check local login state with `jirahere status`
(or `jirahere status --json` for machine-readable state). This has no network
side effects. If it reports not logged in, stop and ask the user to run
`jirahere login` and retry; do not run login on the user's behalf.

The command enforces the same safeguard. A missing login, unreadable stored
credentials, or a credential-refresh failure exits nonzero before Jira is
contacted or any item is changed.

## Output and exit behavior

On a complete success, progress and per-operation outcome lines are printed
to stdout, ending in `Done.`, and the command exits 0. It never returns a
created-item key because it creates no item.

Requested target operations are reported separately as reparent, summary,
and labels; an omitted operation is reported as `not requested`, rather than
as a failure. For a descendant with no failed write, its output identifies
the item's depth and immediate parent and says whether its summary, labels,
both, or neither were rewritten. If either descendant write fails, the
command instead prints a failure/repair line for each failed part and does
not separately report the successful or no-match outcome of the other part.

`--help` prints help to stdout and exits 0 without contacting Jira.

## Failure modes and exit behavior

All failures exit nonzero (the CLI's failure exit is 1). Parser, validation,
authentication, and target-preflight failures print one sanitized diagnostic
to stderr and make no Jira writes:

| Condition | Result |
| --- | --- |
| Missing or extra `<source-key>`, unknown/malformed flag, missing flag value, or repeated move flag | Usage/parser diagnostic; no write. |
| Only one summary pair flag or only one label pair flag | Pairing diagnostic; no write. |
| No requested operation | `nothing to do` diagnostic; no write. |
| Not logged in, stored credentials unreadable, or credential refresh fails | Login remediation diagnostic; no Jira request or write. |
| Source is absent, requested parent is absent, source summary lacks `--summary-old`, or source labels lack `--label-old` | Ordered target-preflight diagnostic; no write. |
| `--label-new` is empty or contains whitespace | Preflight rejection; no write. |
| Jira cannot complete the source or requested-parent preflight read | Body-free retry diagnostic; no write. |

After preflight succeeds, requested target reparent, target summary rewrite,
target label rewrite, and every matching descendant rewrite are independent.
A failure in one does not suppress the other safe operations. For one
descendant, however, a failed part hides that item's successful or no-match
outcome for the other part from output; a failure line does not mean the
other requested write was skipped. A failed descendant-children listing
leaves that branch incomplete while other queued branches continue; a whole
descendant-walk failure (such as its fixed node ceiling) still reports and
applies the partial work already discovered. Any target write failure,
descendant write failure, incomplete branch, or whole walk failure prints its
outcome/repair guidance on stdout, omits `Done.`, and exits nonzero. Some
requested edits may therefore already have succeeded; inspect every reported
line and repair or retry the named work.

Jira failures are rendered body-free: output may name the operation, safe
issue lineage, and an HTTP status, but never exposes response bodies,
credentials, request payloads, or arbitrary transport-error text.
