---
name: req-done
description: >-
  Finalize a completed requirement document: verify it's actually done,
  archive it to the backlog's done/ directory, and report completion. Use
  when the Done When checkbox for a requirement is satisfied and the work is
  ready to close out; use req-update-progress first if it isn't yet
  satisfied, or req-close instead if closing it out unsatisfied.
---

# Requirement Done — Finalize a Completed Requirement

## Purpose

Use this skill to wrap up a requirement document whose work is finished:
confirm it's actually done, move its file into the backlog's `done/`
directory, and report what changed. This skill doesn't implement anything
and doesn't decide how the project reviews or merges changes — it only
handles the requirement document's own lifecycle.

The backlog root is `reqs/` at the project root; `reqs/done/` holds
finished documents.

## Step 1: Pre-Completion Validation

Before archiving, confirm:

- [ ] The Done When checkbox in the requirement document is checked (`[x]`).
  If it isn't, stop here and point the user at `req-update-progress` to
  bring the work to completion first — don't archive a requirement that
  isn't satisfied. If the user wants to close it out anyway, without
  satisfying it, use `req-close` instead of this skill.
- [ ] `Status` in the document's metadata block is set to `Done`.

## Step 2: Run the Project's Own Checks

If the project has its own verification commands — tests, linters, a
build — run them and confirm they pass before treating the work as
complete. This skill doesn't assume or name any specific toolchain; use
whatever the project itself defines.

## Step 3: Archive the Requirement

```bash
git mv reqs/R#####-J#####-<slug>.md reqs/done/
```

Commit the move, along with anything else this requirement's own work
touched — commit only what's relevant to this requirement, nothing else
that happens to be sitting in the working tree — with a message
referencing `R#####`.

## Step 4: Follow the Project's Own Review Flow

If the project uses a PR/review workflow for changes, follow that
project's own normal flow to get this archival change (and whatever else
the requirement's work touched) reviewed and merged. This skill doesn't
script any specific forge's CLI or tooling — use however the project
itself gets changes reviewed and merged.

## Step 5: Report

```markdown
## R##### Done ✅

**Document**: reqs/done/R#####-J#####-<slug>.md
**Review**: <however the project tracked this change for review, if
applicable>

Ready for review and merge.
```

Never merge on the user's behalf — this skill stops once the requirement is
archived and reported; the user reviews and merges per the project's own
flow.
