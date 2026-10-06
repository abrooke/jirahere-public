---
name: jirahere-quarter-list
description: >-
  Drive `jirahere quarter list` to retrieve the flat work-item inventory for
  a quarter — one line (or one JSON object) covering every item in the
  configured project and quarter, each carrying only its key, summary, status,
  created/updated timestamps, resolved date, and assignee. Defaults to
  `defaults.current_quarter`, or
  targets the configured previous/next quarter or an arbitrary labelled
  quarter via `--previous`/`--next`/`--label`. Nothing in Jira is mutated.
  Use when the agent's context holds no knowledge of what work items exist
  in Jira and it needs a quarter's inventory as its starting point, before
  it can name an item to inspect or act on.
---

# jirahere quarter list

## Purpose

`jirahere quarter list` returns a quarter's work-item inventory as a flat
list. It is the entry point when nothing about Jira's items is in context:
run it first to learn which items exist, then follow up per item for
anything deeper.

By default it lists the configured `defaults.current_quarter`, but it can
also target the configured previous or next quarter, or an arbitrary quarter
label typed on the command line — see Flags below.

The list is deliberately flat and shallow. Each entry carries only:

- `key`
- `summary`
- `status`
- `created`
- `updated`
- `resolved` — the item's resolution date, or unresolved (see Output shape)
- `assignee` — the item's assignee display name, or `(unassigned)` (see
  Output shape)

There is no parent, no children, no ancestor chain, no description, and no
labels. When the agent needs any of that hierarchy or detail for an item it
found here, it should follow up with `jirahere context get <key>` (skill
`jirahere-context-get`) at the appropriate `--level`.

## Invocation

```
jirahere quarter list [--json] [--previous|--next|--label <value>]
```

`jirahere quarter list --help` prints the authoritative flag listing to
stdout and exits 0; treat that output as the source of truth for the exact
flag names and defaults. This skill describes intent and failure modes, not
an exhaustive flag table.

There is no positional argument. The project always comes from
settings.json. The quarter comes from settings.json by default, or from one
of `--previous`/`--next`/`--label` when given (see Flags below).

## Flags

`--previous`, `--next`, and `--label <value>` each redirect which quarter is
listed, in place of `defaults.current_quarter`. At most one may be given; the
command rejects two or more (see Failure modes).

- `--previous` — list `defaults.previous_quarter`.
- `--next` — list `defaults.next_quarter`.
- `--label <value>` — list `<value>` verbatim. The value is used exactly as
  given, with no shape validation (no `QQ'YY`-style check or similar); run
  `jirahere quarter list --help` for the authoritative flag description.

`--previous` and `--next` only read whatever is already stored in
`defaults.previous_quarter`/`defaults.next_quarter` — they do not compute an
adjacent quarter from `defaults.current_quarter` or any other value. If the
stored fields are stale, the flags return stale results; keeping them
current is a `jirahere configure set` concern, not something this command
does.

Given none of the three flags, behavior is unchanged: the command lists
`defaults.current_quarter`.

## Read-only contract

The command issues an issue search only. It never creates, edits, moves,
transitions, links, or deletes anything in Jira. Running it repeatedly is
safe and has no side effects. A warm per-(project, quarter) cache may serve
the result after a single lightweight freshness probe, so a repeat call
often makes no full Jira search at all — but see the auth precondition,
which still applies on that warm path.

## Auth precondition

Before running this skill's command, check local login state with
`jirahere status` (or `jirahere status --json` for a machine-readable
result). That check is local only — it makes no network call, has no side
effects, and is safe to run repeatedly. This precondition is unconditional:
it applies even when a warm per-(project, quarter) cache would otherwise
serve the result with no Jira search at all.

If it reports not logged in — `Not logged in. Run "jirahere login" first.`
in text form, or `"logged_in": false` in the `--json` object — stop: do not
run the skill's command. Tell the user to run `jirahere login` first and
then retry. Never run `jirahere login` yourself; login is the user's
responsibility and this skill only surfaces the remediation.

This is an earlier, cleaner stop, not the only safeguard: the underlying
command still enforces auth itself, so any per-command auth failure-mode
rows elsewhere in this skill stay exactly as they are.

## Config precondition

Two settings.json defaults must be set:

- `defaults.project`
- `defaults.current_quarter`

If either is unset the command fails before contacting Jira with one line
that names the missing key, for example
`jirahere: defaults.project is not set; add it to settings.json.` (or
`defaults.current_quarter` in the same form). settings.json is the fix;
there is no flag override for `defaults.project`.

`defaults.current_quarter` only matters when none of `--previous`/`--next`/
`--label` was given. When `--previous` or `--next` is given, the
corresponding `defaults.previous_quarter`/`defaults.next_quarter` field must
be set instead (empty is a failure — see Failure modes); `--label` needs
none of the three quarter defaults set, since its value comes from the
command line.

## Output shape

Items are emitted in the inventory's own order in both formats.

### text (default)

One work item per line, tab-separated, in this field order:

```
<key>\t<summary>\t<created>\t<updated>\t<status>\t<resolved>\t<assignee>
```

`created` and `updated` are RFC 3339 timestamps. `status` is the Jira status
name (for example, `In Progress`), not the status category. `resolved` is an
RFC 3339 (ISO-8601) timestamp when the item is resolved, or the literal `-`
when it is not — never an empty field. `assignee` is the assignee's display
name, or the literal `(unassigned)` when the item has none — never an empty
field — so every row has exactly seven tab-separated columns. An empty
inventory prints exactly one line naming the quarter:

```
no work items in <quarter>
```

where `<quarter>` is the quarter actually listed: `defaults.current_quarter`
when none of `--previous`/`--next`/`--label` was given, or the
flag-resolved label (`defaults.previous_quarter`, `defaults.next_quarter`,
or the `--label` value) otherwise. Exit 0 in both the populated and empty
cases.

### `--json`

Exactly one JSON object on stdout and nothing else:

```json
{"items": [{"key": "...", "summary": "...", "status": "...", "created": "...", "updated": "...", "resolved": "...", "assignee": "..."}]}
```

`items` is always a list. An empty inventory is `{"items": []}`, never
`null`. Each element carries the same seven fields as the text form. `status`
is the Jira status name (for example, `In Progress`), not the status category.
`resolved` is always present: an ISO-8601 string when the item is resolved,
JSON `null` when it is not. `assignee` is always present as a string, never
`null`: the display name, or the literal `"(unassigned)"` when the item has
none. Exit 0.

## Failure modes and exit behavior

Every failure prints one sanitized line to stderr, exits 1, and writes
nothing to stdout — in both the text and `--json` forms (`--json` changes
only the success payload). A successful run and a `--help` request exit 0.
Nothing is ever written to Jira on any path.

| Condition | Message (stderr) | When |
|---|---|---|
| Unknown/malformed flag, or an unexpected positional argument | `jirahere: <parser detail>` or `usage: jirahere quarter list [--json] [--previous \| --next \| --label <value>]` | pre-flight, nothing fetched |
| More than one of `--previous`/`--next`/`--label` given | `usage: jirahere quarter list [--json] [--previous \| --next \| --label <value>]` (body-free usage error, same line as above) | pre-flight, before settings.json is even read, nothing fetched |
| `--label` given with an empty string | `jirahere: --label was given but is empty; an empty value is not allowed.` | pre-flight, before settings.json is even read, nothing fetched |
| settings.json could not be loaded | `jirahere: could not load settings: <reason>.` | pre-flight, nothing fetched |
| `--previous` given and `defaults.previous_quarter` is empty | `jirahere: defaults.previous_quarter is not set; add it to settings.json.` | after settings.json loads, before contacting Jira |
| `--next` given and `defaults.next_quarter` is empty | `jirahere: defaults.next_quarter is not set; add it to settings.json.` | after settings.json loads, before contacting Jira |
| Not logged in | `jirahere: not logged in. Run "jirahere login" first.` | before contacting Jira |
| Stored credentials unreadable | `jirahere: could not load Jira credentials. Run "jirahere login" again.` | before contacting Jira |
| Credential refresh failed | `jirahere: could not refresh Jira credentials[ (<status>)]. Run "jirahere login" again.` | before contacting Jira |
| `defaults.project` or `defaults.current_quarter` unset (no `--previous`/`--next`/`--label` given) | `jirahere: <key> is not set; add it to settings.json.` | before contacting Jira |
| Cache-dir resolution, cache read/write failure, the search's HTTP error, or an unreachable host | `jirahere: quarter list failed: <reason>.` — `<reason>` is `could not reach Jira` or `Jira returned an unexpected response (<status>)` | during the inventory read |

## Following up

`quarter list` tells the agent *which* items exist, not how they relate. Once
it has a key of interest, the next step is `jirahere context get <key>`
(skill `jirahere-context-get`) at the `--level` that matches what it needs —
level 0 for the item's own fields, level 1 for immediate parent and
children, level 2 to add the full ancestor chain, level 3 for the full
descendant subtree.
