---
name: jirahere-status-set
description: >-
  Transition a Jira work item to a specific named workflow status in one
  direct hop. Use when an agent needs to move a work item to a target status
  by name; not for reading current login/status state (a separate, unauthored
  skill) and not for multi-hop transitions across intermediate statuses.
---

# jirahere status set

## Purpose

Use `jirahere status set` to move one existing work item directly to a named
workflow status. The command resolves `--status <name>` against `<id>`'s
currently available Jira workflow transitions and invokes the single matching
one. It never chains multiple transitions across intermediate statuses on the
client side: exactly one Jira-mediated transition call is made per
invocation, and the target must already be directly reachable from `<id>`'s
current status in Jira's own configured workflow. This is a write.

This skill is not for checking whether the user is logged in or what site
they are logged in to — that is the bare `jirahere status` check, a separate,
currently unauthored skill.

## Invocation

```text
jirahere status set <id> --status <name>
```

Both `<id>` and `--status` are required. `<id>` is the one positional
argument. `--status` is rejected only when it is the literal empty string; it
is matched exactly and case-sensitively against one of `<id>`'s currently
available transition target names.

Run `jirahere status set --help` (or `jirahere status --help`) to see this
invocation's synopsis. Both forms print only the same two-line status-group
usage summary, not a per-flag listing — the `<id>` positional and required
`--status` flag are stated directly above instead of being deferred to
`--help`.

A workflow-gated required field on the target transition (for example, a
mandatory resolution field) is not checked here — it surfaces only as the
transition call's own failure, described below.

## Auth precondition

Before running this skill's command, check local login state with
`jirahere status` (or `jirahere status --json` for a machine-readable
result), unconditionally, every time. This check is local only — it makes no
network call and has no side effects.

If it reports not logged in, stop: do not run `jirahere status set`. Tell the
user to run `jirahere login` and then retry. Never run `jirahere login`
yourself; login is the user's responsibility.

This is an earlier, cleaner stop, not the only safeguard: the command itself
also enforces auth. A missing login, unreadable stored credentials, or a
credential-refresh failure exits nonzero before Jira is contacted or any
transition is made.

## Success output and exit behavior

On success, stdout contains exactly one line: `Status set: <id> -> <name>`.
The command exits 0.

## Failure modes and exit behavior

Every failure below exits nonzero (currently status 1). Each failure prints
one diagnostic line to stderr and leaves stdout empty; nothing is changed.

| Condition | Result |
| --- | --- |
| Missing or extra `<id>`, unknown or malformed flag, or missing flag value | Usage diagnostic; no Jira request. |
| `--status` omitted or the literal empty string | Flag-required diagnostic; no Jira request. |
| Not logged in, stored credentials unreadable, or credential refresh fails | Login remediation diagnostic; no Jira request. |
| Listing `<id>`'s available transitions fails | Body-free retry diagnostic; nothing changed. |
| No available transition targets `--status`'s name | Body-free diagnostic listing the status names that are actually available (or stating that none are); no transition attempted. |
| More than one available transition targets `--status`'s name | Body-free ambiguity diagnostic naming the match count; no transition attempted. |
| The single matched transition fails to apply | Body-free failure diagnostic; nothing changed. |

`--help` prints help to stdout and exits 0 without contacting Jira.

Jira failures are rendered body-free: the diagnostic may name the operation
and an HTTP status, but never exposes response bodies, credentials, request
payloads, or arbitrary transport-error text.
