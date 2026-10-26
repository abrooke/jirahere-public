---
name: req-create
description: >-
  Create one atomic requirement document (one Done When checkbox) in the
  local requirements backlog, assigned the next sequential R##### number and,
  when it originates from a Jira work item, that item's reference. Use when a
  new unit of work needs its own tracked document; split first if it covers
  more than one independently-verifiable thing.
---

# Requirement Create — Draft a New Document

## Purpose

Use this skill to write a single markdown file in the local requirements
backlog that nails down one atomically-completable, independently-verifiable
unit of work and tracks it via exactly one Done When checkbox. It doesn't
have to be a whole working feature — it can be a single flag's validation,
one function, one failure mode.

The backlog root is `reqs/` at the project root. It has three directories:
active documents live directly at `reqs/`, `reqs/done/` holds finished work,
`reqs/closed/` holds abandoned or superseded work. This skill only ever
writes to `reqs/` itself — moving a document to `reqs/done/` or
`reqs/closed/` is a different skill's job.

If `reqs/` doesn't exist yet in this project, create it as part of writing
this document — there's no separate init step and no need to ask the user
where to put it or what to call it. `reqs/done/` and `reqs/closed/` are not
created here; they come into existence lazily, on first use by whichever
skills move documents into them. Never look for, or defer to, a
differently-named or differently-located existing requirements directory —
this skill always targets `reqs/`.

## Step 1: Understand the Unit of Work

Ask what problem it solves, who or what it affects, and whether it's
actually one atomic, independently-verifiable thing. If it isn't, go to
Step 2 first.

## Step 2: Split Before Writing Anything

Signals you're looking at more than one document: the Done When statement
needs "and" to join two independently-verifiable things, the work has a
natural "first X, then Y" sequence, or the user describes a whole feature
with several distinct checkable behaviors (parses flags / does the thing /
reports failures, for example).

When that happens, don't write one doc with several checkboxes. Run this
skill once per piece instead — each gets its own `R#####`. Use Dependencies
(Step 4) to link a piece that needs another piece done first.

## Step 3: Assign the Next Local Number

Scan `reqs/`, `reqs/done/`, and `reqs/closed/` for filenames matching
`R(\d{5})-J\d{5}-.*\.md`, take the highest `R#####` found across all three
directories, and use the next integer, zero-padded to five digits. Any of
these three directories, including `reqs/` itself, may not exist yet in a
fresh project — treat a missing directory as contributing no matches rather
than an error. If no matches exist anywhere, start at `R00001`.

`R#####` numbers are never reused, even after a document is closed or moved
to `done/`.

For `J#####`: if this document originates from a Jira work item (for
example, decomposing a larger item into smaller pieces), use that item's
issue number, zero-padded to five digits. Otherwise use the fixed
placeholder `J00000`, meaning "no Jira item".

Derive `<slug>` as a short kebab-case version of the title.

**Filename**: `R#####-J#####-<slug>.md`, written at `reqs/`.

## Step 4: Write the Document

Work through the template section by section with the user rather than
filling it in silently — especially Problem, Scope, and Done When, since
those are worth a second pair of eyes before implementation starts. Write
every section tersely: fragments, not prose.

**Every Risk needs a specified behavior or retiring evidence.** A Risks
entry saying what must *not* happen (e.g. "must not silently assume X") must
also say what the document's Solution/Scope/Done When specifies instead —
or cite evidence the risk cannot occur. A worry with no specified behavior
leaves whoever implements this guessing.

```markdown
# Requirement R#####-J#####: <Title>

**Status**: TODO
**Priority**: High | Medium | Low
**Created**: <YYYY-MM-DD>
**Local ID**: R#####
**Jira ID**: J##### (or "J00000 — no Jira item linked")

## Problem
<What's missing/broken, for whom. Terse.>

## Solution
<The approach. Terse.>

## Done When
- [ ] <One concrete, verifiable statement. The only checkbox in this doc.>

## Scope
### In
- <...>
### Out
- <...>

## Dependencies
<Other R##### numbers this needs done first, external systems, or "None.">

## Risks
- **<risk>**: <mitigation>

## Decision Log
<Empty at filing time. Filled in later as decisions get made.>
```

**One checkbox, no exceptions.** If Done When needs more than one bullet to
state, that's the Step 2 split signal — go back and split instead of
listing multiple checkboxes here.

Good (one document each):
- [ ] `--parent` flag parses and rejects undocumented combinations
- [ ] Descendant summary/label rewrite applies at every depth in the subtree
- [ ] Every documented failure mode produces its documented message + exit code

Bad (two documents crammed into one):
- [ ] Flag parses, validates, AND the full command runs end-to-end — split
  into a parse/validate document and a separate execute document.

## Step 5: Confirm and Save

Show the user the complete content for one pass of review before writing
the file. Once confirmed, write `R#####-J#####-<slug>.md` to `reqs/`,
creating `reqs/` first if it doesn't already exist.

## Step 6: Next Steps

Tell the user the document is created and named (`R#####-J#####-<slug>.md`),
and that they can start on it whenever they're ready, or commit it now and
pick it up later. Don't start implementation from within this skill.
