---
name: jirahere-comment-list
description: >-
  Retrieve every comment already posted on a Jira work item, oldest first, in
  Jira's own returned order. Nothing in Jira is mutated. Use when an agent
  needs a work item's comment history — before deciding whether to add a new
  comment, or to summarize the discussion already on an item.
---

# jirahere comment list

## Purpose

`jirahere comment list` retrieves every comment on a work item, oldest first,
in exactly the order Jira's API returns them. It never re-sorts, filters, or
truncates that order. Use it to read a work item's discussion history before
deciding whether a new comment is warranted, or to summarize what has already
been said.

It makes no changes to Jira: it only reads.

## Invocation

```text
jirahere comment list <id> [--json] [--profile <name>]
```

`<id>` — the target work item's key — is the sole positional argument, and
it is required. `--json` prints one JSON object instead of the text form
(see Output shape). `--profile <name>` reads via that profile's login;
omitted, the default profile is used.

Run `jirahere comment list --help` for the authoritative, shipped flag
listing. This skill describes intent and failure modes rather than
duplicating a flag table.

## Read-only contract

The command issues a comment read only. It never creates, edits, or deletes
a comment, and it never touches the work item's own fields. Running it
repeatedly is safe and has no side effects.

## Pagination

Jira paginates its comment listing; this command follows that pagination to
completion before printing anything, so the output always reflects the
item's full comment history, not just a first page. A defensive per-call
page-count ceiling (the same defensive purpose as the `ChildrenOf` bound
used elsewhere in jirahere) guards against pagination that never
terminates — it is not a caller-facing limit, and there is no `--limit` or
cursor flag to control it today.

## Auth precondition

Before running this skill's command, check local login state with
`jirahere status` (or `jirahere status --json` for a machine-readable
result), unconditionally, every time. This check is local only — it makes no
network call and has no side effects.

If it reports not logged in, stop: do not run `jirahere comment list`. Tell
the user to run `jirahere login` and then retry. Never run `jirahere login`
yourself; login is the user's responsibility.

This is an earlier, cleaner stop, not the only safeguard: the command itself
also enforces auth. A missing login, unreadable stored credentials, or a
credential-refresh failure exits nonzero before Jira is contacted.

## Output shape

One block per comment, printed in Jira's returned order (oldest first), each
block:

```text
<author display name>\t<created timestamp>
<comment body as plain text>
```

For the second block and every one after it, the command first prints a
bare blank line as a separator, then that block's own two parts: the
header line (the comment's author and created timestamp, tab-separated),
and the comment body, rendered from Jira's Atlassian Document Format down
to plain text -- zero or more lines, since a body can be empty, a single
line, or span several lines when it has multiple paragraphs or hard breaks.

A body with no renderable content prints nothing when another comment
follows (its header line is then followed directly by the separator blank
line); when it is the last comment, it prints one trailing blank line. A
multi-paragraph body is ambiguous with the separator: it embeds its own
blank line between paragraphs, which reads identically to a separator
between blocks. Do not
parse the text output by splitting on blank lines -- there is no reliable
way to recover per-comment boundaries that way. Use `--json` for
per-comment boundaries.

If the item has no comments at all, the command instead prints exactly one
line:

```text
no comments on <id>
```

Both the populated and the empty case exit 0.

With `--json`, stdout is exactly one JSON object and nothing else, in
Jira's returned order (oldest first):

```json
{"comments":[{"id":"<comment id>","author":"<author display name>","created":"<created timestamp>","body":"<comment body as plain text>"}]}
```

The output is pretty-printed (two-space indent) rather than compact as
shown above. `comments` is always a list; with no comments it is `[]` (not the
`no comments on <id>` line). `author`, `created` and `body` carry the same
text as the text form, so no raw Atlassian Document Format reaches stdout.
Failures under `--json` behave as in the table below: stderr diagnostic,
nonzero exit, empty stdout.

## Failure modes and exit behavior

Every failure below prints one line to stderr, exits nonzero (usage
errors exit 2; all other failures exit 1), and leaves stdout empty.

| Condition | Result |
| --- | --- |
| Missing or extra `<id>`, or `<id>` is the empty string | Usage diagnostic (`usage: jirahere comment list <id> [--json] [--profile <name>]`), exit 2; no Jira request. |
| Unknown or malformed flag | Parser diagnostic, exit 2; no Jira request. |
| Not logged in, stored credentials unreadable, or credential refresh fails | Login remediation diagnostic, exit 1; no Jira request. |
| Jira's comment read fails (network/transport error, non-2xx response, a response jirahere cannot decode, or the internal page-count ceiling is exceeded) | Body-free failure diagnostic naming the operation and, where applicable, the HTTP status, exit 1; no comments are printed. |

`--help` prints help to stdout and exits 0 without contacting Jira.

Jira failures are rendered body-free: the diagnostic may name the operation
and an HTTP status, but never exposes response bodies, credentials, or
arbitrary transport-error text.

## Following up

When the next step after reading a work item's comment history is posting a
new comment, follow up with `jirahere comment add <id> --body <text>` (skill
`jirahere-comment-add`).
