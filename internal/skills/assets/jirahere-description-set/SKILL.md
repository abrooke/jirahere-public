---
name: jirahere-description-set
description: >-
  Replace one Jira work item's whole description with new plain text. A
  destructive write: the old description is overwritten, cannot be cleared
  with this command, and cannot be undone. Use only after reading the current
  description and getting the user's go-ahead; use `jirahere description get`
  to read it and `jirahere comment add` to add text without replacing anything.
---

# jirahere description set

## Purpose

`jirahere description set` replaces the entire description of one existing
work item with the plain text you supply. The command encodes the text as an
Atlassian Document Format (ADF) document before sending it; you supply plain
text, not ADF.

This is a **destructive, non-idempotent write**:

- **Total replacement.** Nothing of the old description is kept or merged.
- **It cannot clear a description.** An empty or whitespace-only body is
  rejected, so there is no way to blank a description with this command.
- **No undo.** The command does not read, back up, or version the old
  description, and there is no rollback. Once it succeeds, the previous text
  is recoverable only from Jira's own history, if at all.

It changes only the description. It never touches the item's other fields.

## Invocation

```text
jirahere description set <id> (--body <text> | --body-file <path|->) [--profile <name>]
```

- `<id>` — the work item's key — is the sole positional argument, and it is
  required and non-empty.
- Exactly one of `--body <text>` or `--body-file <path|->` is required.
  `--body` is rejected when empty or whitespace-only. `--body-file` reads the
  text from the file at `<path>`, or from stdin when the value is `-`; use it
  for large or quote-heavy text. The file is UTF-8, at most 1 MiB; one leading
  BOM and trailing newlines are dropped, and an empty or whitespace-only file
  is rejected. The text then follows the same rules as `--body`.
- `--profile <name>` writes via that profile's login; omitted, the default
  profile is used. The name is validated before any I/O.

There is no existence pre-flight and no `--json` output: a bad or unknown
`<id>` surfaces only as the write's own failure.

## Plain-text rules for `--body` and `--body-file`

The body is plain text only. **No markdown is interpreted** — `#`, `*`, `-`,
backticks, and `[links](...)` are written as literal characters.

- A **blank line** (a line that is empty or only whitespace) starts a new
  paragraph. Any number of consecutive blank lines count as one break.
- A **single newline** inside a paragraph becomes a hard line break.
- Leading indentation inside a line is kept as written.
- CR and CRLF line endings are normalized to newlines.

Everything written is therefore paragraphs of text and nothing else: no
headings, lists, code blocks, tables, panels, or links with link marks.

## Required agent procedure

Follow these rules in order, every time. They are not optional: this command
destroys the existing description and cannot be undone.

1. **Check login** with `jirahere status` (see Auth precondition). Stop if it
   reports not logged in.
2. **Read the current description first** with
   `jirahere description get '<id>'` (add `--profile '<name>'` when using a
   profile). Show it to the user. Do not skip this step, even if you believe
   the description is empty or you wrote it yourself.
3. **Warn about loss.** Tell the user that `set` replaces the whole
   description with plain paragraphs, and that anything plain text cannot
   represent — lists, headings, code blocks, tables, panels, media, links and
   other formatting — will be lost. Also remind them `description get` is
   itself lossy and drops that structure from what you showed them, so empty
   or partial output does not prove the description has no such content. When
   the structure matters, point them to Jira instead.
4. **State the exact command** you will run, with the full literal body, and
   **wait for the user's explicit go-ahead.** A go-ahead to draft or discuss
   new text is not a go-ahead to run the command.
5. **Single-quote every value** — `<id>`, the `--body` text, and any
   `--profile` name — so the shell cannot expand, split, or interpret them. A
   literal single quote inside a value is written as `'\''`. Never build the
   command by interpolating untrusted text into double quotes.
6. **Run it once.** Do not retry a failed write blindly; read the description
   again to see whether it landed before running it a second time. Running it
   twice with the same body is harmless, but running it again after the user
   or anyone else has edited the item overwrites those edits.
7. **Report the outcome** from the success line or the diagnostic. Never
   describe the description as replaced unless the success line was printed.

Example:

```text
jirahere description set 'PROJ-123' --body 'First paragraph.

Second paragraph, line one.
Second paragraph, line two.'
```

## Auth precondition

Before running this skill's command, check local login state with
`jirahere status` (or `jirahere status --json`), unconditionally, every
time. This check is local only — it makes no network call and has no side
effects. When using `--profile <name>`, check that profile with
`jirahere status --profile <name>`.

If it reports not logged in, stop: do not run `jirahere description set`.
Tell the user to run `jirahere login` (with `--profile <name>` for a named
profile) and then retry. Never run `jirahere login` yourself; login is the
user's responsibility.

The command itself also enforces auth: a missing login, unreadable stored
credentials, or a credential-refresh failure exits nonzero before Jira is
contacted or anything is written.

## Success output and exit behavior

On success, stdout contains exactly one line: `Description set: <id>`. There
is no `Done.` line and nothing else on stdout. The command exits 0.

## Failure modes and exit behavior

Every failure prints its diagnostic to stderr (usually one line; a
flag-parsing error may also print usage) and leaves stdout empty; nothing is
changed. Usage errors exit 2; all other failures exit 1.

| Condition | Result |
| --- | --- |
| Missing or extra `<id>`, or `<id>` is the empty string | Usage diagnostic (`usage: jirahere description set <id> (--body <text> | --body-file <path|->) [--profile <name>]`), exit 2; no Jira request. |
| Unknown or malformed flag, or a flag missing its value | Parser diagnostic, exit 2; no Jira request. |
| Neither `--body` nor `--body-file` given, or `--body` empty or whitespace-only | `jirahere: description set: --body is required and must not be empty or whitespace-only.`, exit 2; no Jira request. |
| Both `--body` and `--body-file` given | `jirahere: description set: --body and --body-file may not be used together.`, exit 2; no Jira request. |
| `--body-file` unreadable, too large, not UTF-8, or empty / whitespace-only | `jirahere: description set: body file "<path>" <reason>.` (stdin: `body from stdin <reason>.`), exit 1; no Jira request. |
| `--profile` empty or not a valid profile name | Profile diagnostic, exit 2; no Jira request. |
| Not logged in | `jirahere: not logged in. Run "jirahere login" first.` (names the profile when `--profile` is used), exit 1; no Jira request. |
| Stored credentials unreadable, or credential refresh fails | Login remediation diagnostic, exit 1; no Jira request. |
| Jira rejects the write (missing or inaccessible item, no permission, network error, non-2xx response) | `jirahere: description set failed: <reason>.`, body-free, exit 1; the description is not replaced. |

`jirahere description set --help` prints `set`'s own usage (`<id>`, `--body`, `--body-file`, `--profile`) to stdout, exit 0, no Jira contact. Run it for the flag listing.

Jira failures are rendered body-free: the diagnostic may name the operation
and an HTTP status, but never exposes response bodies, credentials, request
payloads, or arbitrary transport-error text.
