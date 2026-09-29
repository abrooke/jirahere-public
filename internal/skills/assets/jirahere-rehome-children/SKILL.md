---
name: jirahere-rehome-children
description: >-
  Reparent every direct child of one work item to another in a single bulk
  operation, with optional per-child summary substring and label swaps applied
  across each rehomed child's descendant tree, and optional status-based
  exclusion of children you want left alone. Use when an epic or parent is being
  retired or split and all of its immediate children must move together; use
  jirahere move to reparent and rewrite a single item's own subtree.
---

# jirahere rehome-children

## Purpose

Use `jirahere rehome-children` to reparent every **direct** child of one work
item (the old parent) to another (the new parent) in one run. It lists the old
parent's immediate children only — one level, not the full descendant tree —
and repoints each one at the new parent.

Only the old parent's own direct children ever have their parent field changed.
Grandchildren and deeper descendants are never reparented at any point, even
when a rewrite pass walks them.

A zero-direct-children old parent is a no-op success. There is no rollback: if
the run stops or a later child fails, any child already reparented (and
rewritten) stays that way — re-running is safe but reparents only what is still
under the old parent.

This is a primitive, not a quarter workflow. Compose it with the other
primitives for follow-up edits or links.

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
jirahere rehome-children <old-parent-key> --to <new-parent-key> [--skip-status <s1,s2,...>] [--summary-old <s> --summary-new <s>] [--label-old <l> --label-new <l>]
```

Run `jirahere rehome-children --help` for the authoritative, shipped list of
exact flag names and usage. Treat that output as the source of truth for exact
flag names; this skill describes intent and failure modes rather than
duplicating a flag table that will drift. Note that `--help` lists only the
flags — the required `<old-parent-key>` positional is shown in the usage line
the command prints when it is given the wrong number of positional arguments
(none, or more than one), not in `--help`.

- `<old-parent-key>` — required positional: the work item whose direct children
  are moved. It may appear anywhere among the flags; `--` ends flag parsing.
  Exactly one positional is required — zero or more than one is a usage error.
- `--to <new-parent-key>` — required. The work item the children are reparented
  under. Omitting it is an error even though the flag list marks it "required"
  rather than enforcing it positionally.
- `--skip-status <s1,s2,...>` — optional. A comma-separated list of status names
  whose children are excluded from the run (see Status exclusion). Each entry
  must be non-empty after trimming; a leading, trailing, or doubled comma is
  rejected in pre-flight.
- `--summary-old <s>` / `--summary-new <s>` — optional, all-or-nothing pair.
  Requesting a summary rewrite (see Per-child rewrite).
- `--label-old <l>` / `--label-new <l>` — optional, all-or-nothing pair.
  Requesting a label swap (see Per-child rewrite).

No flag may be given more than once. `--help` / `-h` prints to stdout, exits 0,
and contacts Jira for nothing.

## Status exclusion

`--skip-status` takes a comma-separated list of status names. A direct child
whose current status **name** matches an entry — trimmed, case-insensitive,
whole-string, never a substring and never Jira's status *category* — is
excluded entirely from the run: it is not reparented, not rewritten, and its
descendant tree is never walked.

`--skip-status Done` matches a status literally named `Done`; it does not match
`Not Done`, and it does not match a differently named status that Jira files
under the Done category. A child whose status field is absent or empty matches
no entry and is processed normally.

A misspelled entry silently matches nothing, so no child is excluded. The
per-child report is how you confirm the exclusion took effect — check which
children are reported skipped versus reparented.

## Per-child summary/label rewrite

When `--summary-old`/`--summary-new` and/or `--label-old`/`--label-new` are
given, then immediately after each non-excluded direct child is reparented, the
command walks that child's **full descendant tree** (any depth) and applies a
best-effort rewrite to the rehomed child itself and every descendant:

- Summary: every literal occurrence of `--summary-old` in the item's summary is
  replaced with `--summary-new`. Items whose summary does not contain the
  substring are left untouched for this part.
- Labels: `--label-old` is swapped for `--label-new` on items that carry it;
  other labels are preserved. Items without `--label-old` are left untouched
  for this part.

No descendant's parent link is changed by this pass — only the old parent's
direct children are reparented. The old parent and new parent items themselves
are never rewritten. An excluded child (see Status exclusion) is never entered,
so none of its subtree is walked or rewritten.

Each write is independent: a failed summary write does not skip the label
write, the same item's other write, or any other item's or child's writes. A
failing branch of one child's descendant walk does not stop the other direct
children. Every such failure is recorded and shown in the report, and makes the
run exit nonzero.

## Read the result

On stdout the run always opens with:

```text
Rehoming <old-parent-key>'s children to <new-parent-key>...
```

If the old parent has no direct children and nothing was skipped or failed:

```text
No direct children found; nothing to do.
Done.
```

Otherwise the per-child report follows, in this order:

- **Skipped** children first, one line each:
  `Skipped <child> (status: <status-name>).`
- **Failed** direct children next — a body-free failure line each, ending
  `It was not rehomed; retry once resolved.` A failed child was not reparented
  and gets no rewrite pass.
- **Reparented** children, in the order Jira listed them:
  `Reparented <child> -> <new-parent-key>.` When a rewrite was requested, each
  reparented child's line is followed by its rewrite-pass lines —
  `Rewrite: rehomed child <key> (parent <p>): summary and labels rewritten.` /
  `summary rewritten.` / `labels rewritten.` / `no match; left untouched.`, and
  one `Rewrite: descendant <key> (depth <n>, parent <p>): ...` line per
  descendant. A branch of the walk that could not be listed, or a walk stopped
  short at the per-child node ceiling, adds its own body-free line here.

The final `Done.` line is printed **only when every attempted write in the
whole run succeeded** — every reparent, every summary write, every label write,
and every per-child descendant walk. If any of them failed, `Done.` is absent
and the command exits nonzero. Treat "no `Done.` line" and the nonzero exit
together as the signal to read the report for the specific failed lines.

## Failures and exit behavior

`--help` prints to stdout and exits 0 without contacting Jira. Every failure
below exits nonzero (the CLI's failure exit is 1). Pre-flight failures happen
before any write and leave every child untouched. Jira failures are rendered
body-free throughout: output may name the operation, safe issue keys, and a
numeric HTTP status, but never a response body, credentials, or request
payloads.

| Condition | Result |
| --- | --- |
| No positional, more than one positional, an unknown or malformed flag, a missing flag value, or a flag given more than once | Usage / validation diagnostic; no Jira contact or write. |
| `--to` not given | `--to <new-parent-key> is required` diagnostic naming the old parent; no Jira contact. |
| Only one of `--summary-old` / `--summary-new`, or only one of `--label-old` / `--label-new` | "must be given together" diagnostic; no write. |
| `--skip-status` has an empty or whitespace-only entry (leading/trailing/doubled comma) | "contains an empty or whitespace-only entry" diagnostic; no write. |
| Not logged in, stored credentials unreadable, or credential refresh fails | Login-remediation diagnostic; no Jira request or write. |
| Old parent does not exist (404 in pre-flight) | `rehome-children old parent <key> was not found (404)`; no write. |
| New parent does not exist (404 in pre-flight) | `rehome-children new parent <key> was not found (404)`; no write. |
| Reading the old or new parent fails for a non-404 reason (transport error or non-2xx) | Body-free "could not reach Jira to validate rehome-children's old/new parent `<key>`" diagnostic — numeric HTTP status for a non-2xx response, none for a transport failure; no write. |
| The old parent is deleted between pre-flight and execution (re-checked and 404s on a zero-children or list-failure outcome) | Explicit "was deleted before execution (404, detected on re-check); any children it had were orphaned in Jira, not rehomed" diagnostic with repair guidance. Its former children are orphaned, not rehomed. |
| Listing the old parent's direct children fails and the old parent is not deleted | Body-free "could not complete rehome-children while listing direct children for `<key>`: `<status>`. Try again." No child reparented. |
| One direct child's reparent (`SetParent`) fails | That child is reported failed and left under the old parent; every other direct child is still attempted; the run exits nonzero. |
| One rehomed child's or descendant's summary or label rewrite write fails | A body-free failure line for that item; the other write, other items, and other children still proceed; the run exits nonzero. |
| One rehomed child's descendant walk cannot list a branch, or is stopped short at the per-child node ceiling | A body-free line for that branch/child; other direct children still proceed; the run exits nonzero. |
| Any reparent or rewrite in the run failed | No `Done.` line; the command exits nonzero even though the successful children stay reparented. There is no automatic retry and no rollback. |
