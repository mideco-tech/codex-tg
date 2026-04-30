# Vertical Slice Template

Use one slice per independently reviewable behavior.

## Type

AFK / Human-in-the-loop / Review / QA

## Goal

What observable behavior is true after this slice?

## Scope

Files, modules, or surfaces the agent may change.

## Out Of Scope

Work the agent must not touch in this slice.

## Required Workflow

1. Read `AGENTS.md`.
2. Read the relevant ADR, regression map, contract doc, or nearby tests when present.
3. Add or update the smallest meaningful failing test.
4. Confirm the test fails for the expected reason.
5. Implement the minimum change.
6. Run targeted checks.
7. Run broader checks required by `AGENTS.md`.
8. Record live Telegram validation if this changes UI, routing, observer, lifecycle, Plan Mode, or callbacks.
9. Commit the focused slice.

## Acceptance Criteria

- [ ] Criterion 1
- [ ] Criterion 2
- [ ] Criterion 3
