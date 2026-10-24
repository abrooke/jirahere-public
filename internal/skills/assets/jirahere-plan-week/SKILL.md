---
name: jirahere-plan-week
description: >-
  Default report layout for a week plan: seven parts in fixed order (header,
  last 7 days, day-by-day schedule, grouping, people, slipping, proposed
  changes). Triggered by "plan week", optionally with a start date. No command
  of its own; applies the shared rules in `jirahere-plan-synthesis` and offers
  to save with `jirahere plan save week`. Use when the user asks to plan a
  week; day and month layouts are in `jirahere-plan-day` and
  `jirahere-plan-month`.
---

# jirahere plan week

## Purpose

Gives a week plan one shipped format, so the user does not re-explain it each
session. This skill fixes the layout only. It has no command of its own and
restates none of the rules: gathering, synthesis, the no-invented-dates rule,
people and assets, circuit-error handling and the go-ahead before any write
all live in `jirahere-plan-synthesis`. Follow that skill first.

## Trigger

The user says "plan week", optionally with a start date (for example
"plan week 2026-10-20"). With no date the window starts today.

- The window is 7 days from the start date, the start date included.
- Gather with `jirahere plan context --week`, adding `--from YYYY-MM-DD` when
  a start date was given (skill `jirahere-plan-context`).
- The same date is later passed to `plan save week --date`.

## Layout

Present these seven parts, in this order, under these headings. An empty part
is one line: `none` (per the shared rules). Never omit a heading.

1. **Header** — the window's first and last dates, and the Vision key.
2. **Last 7 days** — what was Done in the 7 days before the window, and a
   count of Done items per zone.
3. **Schedule** — a table with columns: day, circuit zones in order, other
   tasks, people/assets. One row per day of the window. Zones follow the
   circuit order from the bundle. People and assets only where item text
   names them, otherwise `not stated`.
4. **Grouping** — maintenance and non-recurring items grouped by place or
   asset, each with its key. A group's place or asset comes from item text
   only; items naming none go under `not stated`.
5. **People** — who is involved when, only where item text says so; each
   entry with its key. Otherwise `none`.
6. **Slipping** — tasks whose updated date is old, and zones with no Done
   child this week, each with its key or zone and a few words on why.
7. **Proposed changes (not applied)** — new tasks and reparenting the user
   may want. Proposals only: nothing is created or moved by presenting the
   plan.

Fragments and table rows only, no background prose.

## Saving

After presenting, offer to save it with `jirahere plan save week`. Saving is a
write and follows the shared rules and skill `jirahere-plan-save`: state the
exact command and its effect (the saved description is replaced wholesale if
`Plan Week <YYYYMMDD>` already exists), get the user's go-ahead, then run it.
Proposed changes are never applied by saving; each is a separate write with
its own approval.

## Relation to other skills

- `jirahere-plan-synthesis` — the shared rules this layout applies.
- `jirahere-plan-context` — gathers the bundle.
- `jirahere-plan-save` — writes the approved plan.
- `jirahere-plan-day`, `jirahere-plan-month` — the other layouts.
