---
name: req-close
description: >-
  Close a requirement document that won't be implemented as written:
  superseded, out of scope, duplicate, or already covered elsewhere. Use
  when a requirement no longer applies and should be archived without going
  through the done workflow; use req-done instead if the work was actually
  implemented.
---

# Requirement Close — Close a Requirement Without Implementing It

## Purpose

Use this skill to close out a requirement document that won't be
implemented as written: superseded, out of scope, duplicate, or already
covered by other work. This is different from `req-done`, which is for
requirements that were actually implemented.

The backlog root is `reqs/` at the project root; `reqs/closed/` holds
closed documents.

**Use `req-close` when:**
- Requirements changed and this requirement no longer applies
- It duplicates another requirement
- The functionality already exists (from other work, not from finishing
  this requirement's own implementation)

**Use `req-done` instead when:**
- You just finished implementing this requirement

## Step 1: Identify Requirement and Reason

If not given, ask for the local ID and a closure reason: **Superseded**,
**No Longer Needed**, **Duplicate** (of which requirement?), or **Already
Implemented Elsewhere** (where?).

## Step 2: Confirm

```markdown
## R#####: <Title>
**Status**: <current>

**Summary**: <brief>

**Proposed action**: Close as <reason>

Proceed? (yes/no)
```

## Step 3: Update and Archive

Update the metadata block:
```markdown
**Status**: Closed
**Closed**: <YYYY-MM-DD>
**Closed reason**: <reason and brief explanation>
```

```bash
git mv reqs/R#####-J#####-<slug>.md reqs/closed/
```

Closed requirements go to `reqs/closed/`, not `reqs/done/` — `done/` is
reserved for requirements that were actually implemented (see `req-done`).

## Step 4: Commit

```bash
git add -- "reqs/closed/R#####-J#####-<slug>.md"
git commit --only -m "docs(R#####): close - <brief reason>" -- "reqs/R#####-J#####-<slug>.md" "reqs/closed/R#####-J#####-<slug>.md"
```
`--only` with an explicit pathspec commits just these files, even if other
roles have staged work in the index. List both the old and new path so the
`git mv` deletion is committed with the addition.
Do not push unless the user asks.

## Step 5: Confirm to User

```markdown
✅ R##### closed and archived to reqs/closed/R#####-J#####-<slug>.md
```
