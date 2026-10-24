---
name: jirahere-plan-month
description: >-
  Default report layout for a month plan: seven parts in fixed order (header,
  circuit coverage, work-stream progress, week-by-week themes, assets and
  cadence, structure, Vision drift). Triggered by "plan month", optionally
  with a start date. No command of its own; applies the shared rules in
  `jirahere-plan-synthesis` and offers to save with `jirahere plan save
  month`. Use when the user asks to plan a month; day and week layouts are in
  `jirahere-plan-day` and `jirahere-plan-week`.
---

# jirahere plan month

## Purpose

Gives a month plan one shipped format, so the user does not re-explain it each
session. This skill fixes the layout only. It has no command of its own and
restates none of the rules: gathering, synthesis, the no-invented-dates rule,
people and assets, circuit-error handling and the go-ahead before any write
all live in `jirahere-plan-synthesis`. Follow that skill first.

## Trigger

The user says "plan month", optionally with a start date (for example
"plan month 2026-11-01"). With no date the window starts today.

- The window is 30 days from the start date, the start date included.
- Gather with `jirahere plan context --month`, adding `--from YYYY-MM-DD` when
  a start date was given (skill `jirahere-plan-context`).
- The same date is later passed to `plan save month --date`.

## Layout

Present these seven parts, in this order, under these headings. An empty part
is one line: `none` (per the shared rules). Never omit a heading.

1. **Header** — the window's first and last dates, and the Vision key.
2. **Circuit coverage** — loops completed in the window, segments done per
   zone, and zones untouched in the window.
3. **Work-stream progress** — a table with columns: stream, parents, Done
   child tasks, open child tasks, trend (up, flat, down against the previous
   30 days). Streams are not in the bundle: group parent items by their
   parent-child structure and item text only. Where a parent's stream is
   unclear, put it under `not stated`; never invent a stream.
4. **Week-by-week themes** — one line per week of the window, naming the
   theme the planned work shares. A week with no clear theme is
   `not stated`.
5. **Assets and cadence** — maintenance items, each with its key and when it
   was last done (its most recent Done date from the bundle), otherwise
   `not stated`.
6. **Structure** — parents to split (many open children), items without a
   parent, and series left mid-flight, each with its key.
7. **Vision drift** — whether the work done in the window matches the Vision
   text, in a few words, and proposed Vision wording changes. Proposals only:
   nothing is edited by presenting the plan.

Fragments and table rows only, no background prose.

## Saving

After presenting, offer to save it with `jirahere plan save month`. Saving is
a write and follows the shared rules and skill `jirahere-plan-save`: state the
exact command and its effect (the saved description is replaced wholesale if
`Plan Month <YYYYMMDD>` already exists), get the user's go-ahead, then run it.
Proposed Vision changes are never applied by saving; each is a separate write
with its own approval.

## Relation to other skills

- `jirahere-plan-synthesis` — the shared rules this layout applies.
- `jirahere-plan-context` — gathers the bundle.
- `jirahere-plan-save` — writes the approved plan.
- `jirahere-plan-day`, `jirahere-plan-week` — the other layouts.
