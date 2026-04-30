# Agent Feature Workflow

This document is pull-context. The mandatory operating rules live in `AGENTS.md`; read that file first after any context reset.

## Cycle

1. Align before coding when the request is vague.
2. Write or identify a short destination: issue, feature brief, or bug report.
3. Cut work into vertical slices that produce observable behavior.
4. Implement one unblocked slice with TDD.
5. Run targeted checks, then broader checks.
6. Use fresh-context review for meaningful diffs.
7. Validate Telegram-facing changes through live readback when available.
8. Move durable decisions into ADRs, regression maps, or contract docs; do not leave stale plans as source of truth.

## Alignment Questions

- What user-visible behavior changes?
- What is explicitly out of scope?
- Which module boundary should contain the change?
- Which tests prove the behavior?
- Which live Telegram scenario proves the behavior, if UI/routing/lifecycle is involved?

## Agent Task Shape

Each implementation task should be small enough to commit separately and should name:

- goal
- allowed files or subsystem
- out-of-scope work
- expected tests
- required live validation, if applicable
- commit message

## Review Shape

A fresh reviewer should receive the issue/brief, diff, tests, and relevant ADR/rules. Review tests first, then implementation.
