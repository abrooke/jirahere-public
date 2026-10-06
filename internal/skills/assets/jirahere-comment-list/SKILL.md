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
jirahere comment list <id>
```

`<id>` — the target work item's key — is the sole positional argument, and
it is required. There are no other flags today.

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

A body with no renderable content prints as a single bare blank line in
the body's place, which looks identical to the separator blank line
described above. When that empty-body comment is not the last one, this
produces two consecutive blank lines between its header and the next
block's header -- one from the empty body, one from the separator that
follows it (a known issue, tracked as B0014). When the empty-body comment
is the last one, that same blank line instead becomes a trailing blank
line at the end of the whole output. A multi-paragraph body has the same
ambiguity from the other direction: it embeds its own blank line between
paragraphs, which reads identically to a separator between blocks. A hard
break inside a single paragraph does not: it renders as one extra
non-blank line, so it creates no such ambiguity on its own. Do not parse
this output by splitting on blank lines -- there is
no reliable way to recover per-comment boundaries that way, and there is
no `--json` mode yet to get them structurally.

If the item has no comments at all, the command instead prints exactly one
line:

```text
no comments on <id>
```

Both the populated and the empty case exit 0. There is no `--json` output
option yet.

## Failure modes and exit behavior

Every failure below prints one line to stderr, exits nonzero (currently
status 1), and leaves stdout empty.

| Condition | Result |
| --- | --- |
| Missing or extra `<id>`, or `<id>` is the empty string | Usage diagnostic (`usage: jirahere comment list <id>`); no Jira request. |
| Unknown or malformed flag | Parser diagnostic; no Jira request. |
| Not logged in, stored credentials unreadable, or credential refresh fails | Login remediation diagnostic; no Jira request. |
| Jira's comment read fails (network/transport error, non-2xx response, a response jirahere cannot decode, or the internal page-count ceiling is exceeded) | Body-free failure diagnostic naming the operation and, where applicable, the HTTP status; no comments are printed. |

`--help` prints help to stdout and exits 0 without contacting Jira.

Jira failures are rendered body-free: the diagnostic may name the operation
and an HTTP status, but never exposes response bodies, credentials, or
arbitrary transport-error text.

## Following up

When the next step after reading a work item's comment history is posting a
new comment, follow up with `jirahere comment add <id> --body <text>` (skill
`jirahere-comment-add`).
