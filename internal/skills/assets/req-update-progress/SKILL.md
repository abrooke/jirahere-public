---
name: req-update-progress
description: >-
  Check a requirement document's one Done When checkbox based on completed
  implementation work, using conversation context first and a
  git-inspection fallback, then commit the result. Use after finishing work
  on a requirement to reconcile and record its progress; use req-done next
  once the checkbox is satisfied, or req-update-decisions first if the
  implementation diverged from the requirement's own design.
---

# Requirement Update Progress — Reconcile and Commit

Check a requirement's one Done When box based on work actually completed,
using conversation context first and git/code inspection as a fallback,
then commit the result. A requirement doc has exactly one checkbox — this
is a binary call, not a percentage.

## Step 1: Identify the Target Requirement

If conversation context already makes it obvious which requirement this
is, skip detection. Otherwise ask for the requirement's local ID
(R#####).

## Step 2: Gather Evidence

**Prefer conversation context** — what was just implemented, tested, and
confirmed working in this session. It's faster and more reliable than
reconstructing it from git.

**Fall back to git/code inspection** when context is thin:
```bash
git log --oneline -n 20
git diff --stat HEAD~10..HEAD
git diff --name-status HEAD~10..HEAD
```
Look at whatever new or modified source and test files these commands
surface, against the project's own actual directory layout — don't assume
any particular structure.

## Step 3: Check the Evidence

Read the requirement's Done When statement. Decide, with evidence:
- **Satisfied** — code exists, does what Done When describes, and (if it's
  about behavior) is covered by a test.
- **Not yet** — no evidence, or only partial evidence.

**Conservative policy**: don't mark it satisfied without direct evidence (a
file, a passing test, a confirmed behavior). If code exists but isn't
tested and Done When implies testing, leave it unchecked and say why.

## Step 4: Present Findings

```markdown
## Progress: <Title> (R#####)

**Done When**: <the checkbox statement>
**Verdict**: ✅ Satisfied — Evidence: <file(s)/test(s)> | ⏳ Not yet — Reason: <what's missing>
```

Wait for the user to confirm, correct, or add context before writing
anything.

## Step 5: Divergence Check

If implementation went differently than the requirement describes
(different approach, added/dropped scope, a design decision that changed
something — including "this turned out to be two requirements"), flag it
and suggest running `req-update-decisions` afterward — don't try
to silently rewrite the requirement's design sections from here.

## Step 6: Apply Update

Apply the verdict once the user confirms it. If satisfied: check the box,
set `Status` to `Done`. If not yet: leave the box unchecked, set `Status`
to `In Progress` (from `TODO` if needed).

## Step 7: Commit

Roles share one worktree, so other roles' staged, unstaged, or untracked
work may be present. Commit only the files this requirement's work touched:
the requirement doc plus the implementation and test files known from the
Step 2 evidence. Derive that path list on each invocation; never hardcode it.
Keep only paths with uncommitted changes per `git status` (Step 2 evidence
may list already-committed files, and `--only` fails with "nothing to
commit" if none has changes). For a deletion or rename, list both the old
and new paths.

```bash
git add -- "<path1>"
git add -- "<path2>"
git commit --only -m "<type>(R#####): <brief description of what was implemented>

- <key changes>
- Checked requirement Done When" -- "<path1>" "<path2>"
```

- One `git add -- "<path>"` per file, then one `git commit --only` listing
  the same paths. `-m` goes before the `--` separator (after it, git reads
  the message as a pathspec). Quote every path.
- `--only` commits just those paths and leaves every other staged entry
  untouched. Do not unstage, reset, or restore another role's changes.
- Never use `git add -A`, `git add .`, `git add -u`, `git commit -a` /
  `-am`, `git commit --amend`, `git commit -i` / `--include`, a bare
  `git add` / `git commit`, a directory or glob pathspec (`reqs/`,
  `internal/*`), or `git stash`.
- A file missed from the list is left unstaged and shows in `git status`;
  it is not swept in. Check `git status` after committing and add any
  missed file of this requirement's work in a follow-up explicit commit.

Do not push unless the user asks.

## Step 8: Next Steps

**If not yet satisfied:**
```
Requirement progress updated and committed. Not yet satisfied.

Continue the implementation, then run this skill again once more work is
done.
```

**If satisfied:**
```
R##### is complete!

Run `req-done` to finalize it and move it to reqs/done/.
```
