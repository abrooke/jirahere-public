---
name: jirahere-description-get
description: >-
  Read one Jira work item's description as plain text, or as JSON or Markdown
  that also carries the key. The read is lossy: lists, headings, code blocks
  and tables are not shown. Nothing in Jira is mutated. Use when an agent
  needs an item's description text and nothing else; use `jirahere context get`
  for its place in the hierarchy.
---

# jirahere description get

## Purpose

`jirahere description get` reads one work item and prints its description.
Use it when the description is all you need. For the item's parent,
children, or ancestry, use `jirahere context get` (skill
`jirahere-context-get`) instead.

It makes no changes to Jira: it only reads.

## Invocation

```text
jirahere description get <id> [--json|--md] [--profile <name>]
```

- `<id>` — the work item's key — is the sole positional argument, and it is
  required.
- `--json` prints one JSON object carrying the key and the description.
- `--md` prints a Markdown section with the key as a header.
- `--json` and `--md` are mutually exclusive.
- `--profile <name>` runs against that profile's login, settings, and cache;
  omitted, the default profile is used. The name is validated before any I/O.

Run `jirahere description get --help` for the authoritative, shipped flag
listing. This skill describes intent and failure modes rather than
duplicating a flag table.

## Read-only contract

The command issues one work-item read. It never edits the item, and running
it repeatedly is safe. To replace a description, use
`jirahere description set` — a separate, mutating command.

## Auth precondition

Before running this skill's command, check local login state with
`jirahere status` (or `jirahere status --json`), unconditionally, every
time. This check is local only — it makes no network call and has no side
effects. When using `--profile <name>`, check that profile with
`jirahere status --profile <name>`.

If it reports not logged in, stop: do not run `jirahere description get`.
Tell the user to run `jirahere login` (with `--profile <name>` for a named
profile) and then retry. Never run `jirahere login` yourself; login is the
user's responsibility.

The command itself also enforces auth: a missing login, unreadable stored
credentials, or a credential-refresh failure exits nonzero before Jira is
contacted.

## Output shape

All three shapes carry the same description text.

**Default (plain text).** The description, then a trailing newline. There is
no header and no key.

**`--json`.** One JSON object, indented by two spaces:

```json
{
  "key": "<id>",
  "description": "<plain text>"
}
```

**`--md`.**

```text
# <id>

## Description

<plain text>
```

`--json`'s `key` is Jira's canonical key (e.g. `PROJ-1`), not `<id>` as typed.
The `--md` `# <id>` header carries the same canonical key with control and
format characters stripped.

## Empty description

When the item has no description, or one with no readable paragraphs, the
command still exits 0:

- Default: prints nothing at all (empty stdout).
- `--json`: `"description"` is the empty string.
- `--md`: the `## Description` section is present and its body is empty.

Empty output therefore means "nothing readable", not failure — and, because
the read is lossy, not necessarily "the description is blank" either.

## Lossy read — do not treat the output as the full description

The description is converted from Jira's rich-text format (ADF) to plain
text, and only paragraph text survives. Paragraphs are separated by a blank
line, a hard line break becomes a newline, and a mention appears as its
`@name`. Top-level paragraphs and paragraphs nested directly in a blockquote
are shown. Control characters (C0, C1) and bidi override/embedding/isolate
characters are stripped from the text. Everything else is **silently dropped**, with no placeholder and no
warning:

- headings
- bulleted and numbered lists
- code blocks
- tables
- panels, media, and inline emoji or smart links

Mark-up such as bold, italic, code, and links is flattened to bare text.
Consequences:

- A description made only of a list, heading, code block or table reads as
  empty.
- A mixed description can look complete while missing its lists or code.
- Do not use this output as the basis for `jirahere description set`
  without telling the user that structure beyond paragraphs will be lost:
  `set` replaces the whole description with plain paragraphs.

When the description's structure matters, say so and send the user to Jira.

## Failure modes and exit behavior

Every failure prints one line to stderr and leaves stdout empty. Usage
errors exit 2; all other failures exit 1.

| Condition | Result |
| --- | --- |
| Missing or extra `<id>`, or `<id>` is the empty string | Usage diagnostic (`usage: jirahere description get <id> [--json\|--md] [--profile <name>]`), exit 2; no Jira request. |
| Unknown or malformed flag, or a flag missing its value | Parser diagnostic, exit 2; no Jira request. |
| `--json` and `--md` together | `jirahere: description get: --json and --md may not be used together`, exit 2; no Jira request. |
| `--profile` empty or not a valid profile name | Profile diagnostic, exit 2; no Jira request. |
| Not logged in | `jirahere: not logged in. Run "jirahere login" first.` (names the profile when `--profile` is used), exit 1; no Jira request. |
| Stored credentials unreadable, or credential refresh fails | Login remediation diagnostic, exit 1; no Jira request. |
| Jira's read fails (missing or inaccessible item, network error, non-2xx response, undecodable response) | `jirahere: description get failed: <reason>.`, body-free, exit 1. |

`--help` prints help to stdout and exits 0 without contacting Jira.

Jira failures are rendered body-free: the diagnostic may name the operation
and an HTTP status, but never exposes response bodies, credentials, or
arbitrary transport-error text.
