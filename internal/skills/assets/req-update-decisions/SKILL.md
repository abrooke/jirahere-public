---
name: req-update-decisions
description: >-
  Record design decisions, scope changes, and architectural choices made
  during conversation against an existing requirement document's Decision
  Log and affected sections, whether or not code has caught up to them yet.
  Use when conversation reveals a decision that changes a requirement's
  scope, Done When, or spec body; use req-create afterward if the decision
  splits the requirement into more than one independently-verifiable piece.
---

# Requirement Update Decisions — Record Design Decisions

Capture design decisions, scope changes, and architectural choices made
during conversation that affect a requirement's requirements or approach,
whether or not code has caught up to them yet.

## Step 1: Identify the Target Requirement

Ask if unclear, then read the current file (`reqs/R#####-J#####-*.md`).

## Step 2: Find the Decisions

Look through the conversation for signals like:
- **Scope changes**: "we don't need X anymore", "let's also cover Y"
- **Architecture choices**: "let's put this in `internal/quarter` instead"
- **Requirement changes**: a flag's behavior changed, a failure mode was
  reconsidered, an edge case was found
- **Approach pivots**: "actually, composing it from the existing primitives
  is cleaner than a new one"

## Step 3: Assess Impact

For each decision, work out:
- **Scope**: does In / Out need to change?
- **Split**: does this decision mean the requirement now covers more than
  one atomic, independently-verifiable thing? If so, this is a split, not
  an edit — see Step 4.
- **Key Files**: does this touch files not currently listed?
- **Risks/Dependencies**: any new risk or dependency introduced?
- **Spec content**: do any command-shape examples, flag tables, or failure
  mode tables in the requirement body now say something incorrect?

## Step 4: Update the Requirement

- **Decision Log**: append an entry —
  ```markdown
  - **<YYYY-MM-DD>**: <what was decided>. Rationale: <why>. Impact: <what
    changed in this requirement as a result>.
  ```
- **Scope/Done When**: edit directly to reflect the new decision — reword
  as needed. Don't leave stale content that contradicts the Decision Log
  entry.
- **Spec body**: fix any now-incorrect examples or tables in place.
- **Split decision**: if Step 3 flagged a split, don't cram a second
  checkbox into Done When — record the split in the Decision Log, then run
  `req-create` for the new piece(s) and note the new ID(s)
  here once created.

Show the user the diff of what you're about to change before writing it,
since this skill can touch the requirement's actual requirements, not just
a log.

## Step 5: Commit

```bash
git add -- "reqs/R#####-J#####-<slug>.md"
git commit --only -m "docs(R#####): record design decision - <brief summary>" -- "reqs/R#####-J#####-<slug>.md"
```
`--only` with an explicit pathspec commits just that file, even if other
roles have staged work in the index. Use the exact path of the file read in
Step 1 for `reqs/R#####-J#####-<slug>.md` — do not re-glob.
Do not push unless the user asks.
