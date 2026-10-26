---
name: jirahere-comment-add
description: >-
  Post one plain-text comment to an existing Jira work item, encoded as ADF.
  Use when an agent needs to leave a comment on a work item by its key; do
  not use it for editing a work item's own fields or for reading existing
  comments.
---

# jirahere comment add

## Purpose

Use `jirahere comment add` to post exactly one plain-text comment to an
existing work item. The command encodes the plain-text body as an Atlassian
Document Format (ADF) document before sending it; you supply plain text, not
ADF yourself. It makes a single write and returns the new comment's ID. It
never edits the target's own fields, and it cannot list, edit, or delete
existing comments — use a separate command for that.

## Invocation

```text
jirahere comment add <id> (--body <text> | --body-file <path|->) [--profile <name>]
```

`<id>` is required: the one positional argument, the target work item's key.
Exactly one of `--body <text>` or `--body-file <path|->` is required.
`--body` is rejected only when it is the literal empty string; no
whitespace-trimming or content validation is applied, so a body of only
whitespace is accepted and sent as-is. `--body-file` reads the text from the
file at `<path>`, or from stdin when the value is `-`; use it for long or
quote-heavy text. The file is UTF-8, at most 1 MiB; one leading BOM and
trailing newlines are dropped, and an empty or whitespace-only file is
rejected. The text is then posted exactly as `--body` would post it.
`--profile <name>` posts via that profile's login; omitted, the default
profile is used.

Comments cannot be edited or deleted, so a wrong file posts a permanent
comment: check the file's content, state the exact command, and get the
user's go-ahead before running it.

Run `jirahere comment add --help` for the authoritative, shipped flag
listing. This skill describes intent and failure modes rather than
duplicating a flag table.

There is no existence pre-flight: the command does not check whether `<id>`
exists before writing. A bad or unknown `<id>` surfaces only as the
add-comment write's own failure — one HTTP round trip total, not an earlier
read. There is no `--json` output option yet.

## Auth precondition

Before running this skill's command, check local login state with
`jirahere status` (or `jirahere status --json` for a machine-readable
result), unconditionally, every time. This check is local only — it makes no
network call and has no side effects.

If it reports not logged in, stop: do not run `jirahere comment add`. Tell
the user to run `jirahere login` and then retry. Never run `jirahere login`
yourself; login is the user's responsibility.

This is an earlier, cleaner stop, not the only safeguard: the command itself
also enforces auth. A missing login, unreadable stored credentials, or a
credential-refresh failure exits nonzero before Jira is contacted or any
comment is written.

## Success output and exit behavior

On success, stdout contains exactly one line: the new comment's ID. Nothing
else is printed to stdout. The command exits 0.

## Failure modes and exit behavior

Every failure below exits nonzero (usage errors exit 2; all other failures exit
1). Each failure prints one diagnostic line to stderr and leaves stdout empty.

| Condition | Result |
| --- | --- |
| Missing or extra `<id>`, unknown or malformed flag, or missing flag value | Usage diagnostic; no Jira request. |
| Neither `--body` nor `--body-file` given, or `--body` is the literal empty string | Body-required diagnostic; no Jira request. A whitespace-only body is *not* this case — it is accepted and sent. |
| Both `--body` and `--body-file` given | `jirahere: comment add: --body and --body-file may not be used together.`, exit 2; no Jira request. |
| `--body-file` unreadable, too large, not UTF-8, or empty / whitespace-only | `jirahere: comment add: body file "<path>" <reason>.` (stdin: `body from stdin <reason>.`), exit 1; no Jira request. |
| Not logged in, stored credentials unreadable, or credential refresh fails | Login remediation diagnostic; no Jira request. |
| Jira rejects the write (including an `<id>` that does not exist or is not visible) | Body-free failure diagnostic naming the HTTP status; no comment is created. |

`--help` prints help to stdout and exits 0 without contacting Jira.

Jira failures are rendered body-free: the diagnostic may name the operation
and an HTTP status, but never exposes response bodies, credentials, request
payloads, or arbitrary transport-error text.
