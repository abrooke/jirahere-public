---
name: jirahere-plan-day
description: >-
  Default report layout for a day plan: seven parts in fixed order (header,
  circuit position, plan table, in progress, Vision line, flags, ask), fitting
  one screen. Triggered by "plan day", optionally with a start date. No command
  of its own; applies the shared rules in `jirahere-plan-synthesis` and offers
  to save with `jirahere plan save day`. Use when the user asks to plan a day
  or asks "what should I do today"; week and month layouts are in
  `jirahere-plan-week` and `jirahere-plan-month`.
---

# jirahere plan day

## Purpose

Gives a day plan one shipped format, so the user does not re-explain it each
session. This skill fixes the layout only. It has no command of its own and
restates none of the rules: gathering, synthesis, the no-invented-dates rule,
people and assets, circuit-error handling and the go-ahead before any write
all live in `jirahere-plan-synthesis`. Follow that skill first.

## Trigger

The user says "plan day", optionally with a start date (for example
"plan day 2026-10-20"). With no date the window is today.

- Gather with `jirahere plan context --day`, adding `--from YYYY-MM-DD` when
  a start date was given (skill `jirahere-plan-context`).
- The same date is later passed to `plan save day --date`.

## Layout

Present these seven parts, in this order, under these headings. An empty part
is one line: `none` (per the shared rules). Never omit a heading.

1. **Header** — the date, the Vision key, and a one-line intent for the day.
2. **Circuit position** — the last zone worked, when, and which layer if the
   bundle logs one; then the next zone. A circuit error is reported as the
   shared rules say, not retried.
3. **Plan** — a table with columns: order, task or zone, key, place,
   people/assets, minutes. Recurring segments default to 3 unless the user
   says otherwise. People and assets only where item text names them,
   otherwise `not stated`.
4. **In progress** — tasks already started that can finish today.
5. **Vision line** — the Vision's current daily/phase text, quoted tersely;
   do not restate the whole Vision.
6. **Flags** — urgent or stale tasks affecting today, each with its key and
   a few words on why.
7. **Ask** — one question: how many segments the user will do today.

Fits one screen: fragments and table rows only, no background prose.

## Saving

After presenting, offer to save it with `jirahere plan save day`. Saving is a
write and follows the shared rules and skill `jirahere-plan-save`: state the
exact command and its effect (the saved description is replaced wholesale if
`Plan Day <YYYYMMDD>` already exists), get the user's go-ahead, then run it.
Do not save before the user answers the Ask, unless they approve saving as is.

## Relation to other skills

- `jirahere-plan-synthesis` — the shared rules this layout applies.
- `jirahere-plan-context` — gathers the bundle.
- `jirahere-plan-save` — writes the approved plan.
- `jirahere-plan-week`, `jirahere-plan-month` — the other layouts.
