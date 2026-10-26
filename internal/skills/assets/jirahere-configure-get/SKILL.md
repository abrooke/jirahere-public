---
name: jirahere-configure-get
description: >-
  Drive `jirahere configure get` to read the five configured `defaults.*`
  values — `project`, `issue_type`, `current_quarter`, `previous_quarter`,
  `next_quarter` — straight from settings.json, as labeled text lines or one
  `--json` object. Nothing in Jira is touched; this reads only a local file.
  Use when the agent needs a configured default in context and does not have
  it yet — for example before choosing a `--label` value for
  `jirahere quarter list`, or to check whether `defaults.current_quarter`,
  `defaults.previous_quarter`, or `defaults.next_quarter` is set at all.
---

# jirahere configure get

## Purpose

`jirahere configure get` reads settings.json's `defaults` object and reports
all five fields — `project`, `issue_type`, `current_quarter`,
`previous_quarter`, `next_quarter` — every time, whether or not they are set.
It is a pure query: it never writes settings.json and never contacts Jira.

Reach for it whenever a configured default is needed in context and is not
already known — for example, before passing `--label` to
`jirahere quarter list`, or to check whether `defaults.current_quarter` (or
`previous_quarter`/`next_quarter`) has been set before relying on it. To
change a default instead of reading it, use `jirahere configure set` — a
separate command with no skill of its own yet (see Companion write path
below).

## Invocation

```
jirahere configure get [--json]
```

`jirahere configure get --help` prints the authoritative flag listing to
stdout and exits 0; treat that output as the source of truth for the exact
flag names and defaults. This skill describes intent and failure modes, not
an exhaustive flag table.

There is no positional argument; a surplus one is a pre-flight failure.

## Read-only, no-auth contract

This command reads only the local settings.json file. It makes no Jira
request of any kind, so — unlike every Jira-touching skill in this bundle —
there is no auth precondition to check and no `jirahere status` call needed
first. Running it repeatedly is safe and has no side effects.

## Output shape

### text (default)

Five labeled lines, always in this order, one per field:

```
project: <value>
issue_type: <value>
current_quarter: <value>
previous_quarter: <value>
next_quarter: <value>
```

An unset field prints the literal marker `(unset)` in place of a value —
never a blank line and never an omitted line. All five lines are always
printed, regardless of how many fields are set.

### `--json`

Exactly one JSON object on stdout and nothing else, with all five keys
always present:

```json
{
  "project": "...",
  "issue_type": "...",
  "current_quarter": "...",
  "previous_quarter": "...",
  "next_quarter": "..."
}
```

An unset field serializes as JSON `null`, not an empty string and not an
omitted key.

## Failure mode and exit behavior

The only documented failure is settings.json being unreadable or malformed.
That prints one sanitized line to stderr —
`jirahere: could not load settings: <reason>.` — and exits non-zero with no
field output in either form.

Short of that, a successful read always exits 0, no matter how many of the
five fields are actually set. This command is a query, not a validation: it
never fails just because a field is unset — an unset field simply renders as
`(unset)` (text) or `null` (`--json`). A `--help` request also exits 0.

## Companion write path

`jirahere configure set` writes these same `defaults.*` fields. It exists and
is shipped, but has no skill of its own yet — out of scope here. Do not
invoke `configure set` on the strength of this skill; treat it as a sibling
command to look up separately when a default actually needs to change.
