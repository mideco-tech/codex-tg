# AGENTS

Purpose: help AI agents work on `codex-tg` without increasing complexity or weakening the operator-facing Telegram control loop.

Good code is code that is easy to understand, change, test, and safely extend. Good agent work is evidence-backed, small in scope, and validated through the same surfaces the operator uses.

## Repository Purpose

`codex-tg` is a Go daemon that turns Telegram into a thread-first remote UI and observer for local OpenAI Codex App Server.

The repo is public-facing. Keep every change safe for open-source publication: no private paths, tokens, Telegram ids, local sessions, databases, logs, screenshots with private data, or environment-specific credentials.

## Working Mode

- First understand the current design. Read nearby code, tests, ADRs, and `docs/testing/regression-map.md` when it exists before editing.
- Do not treat a vague request as enough context. Ask focused questions when requirements, constraints, or tradeoffs are unclear and cannot be discovered safely.
- Prefer small steps: inspect -> plan -> test -> implement -> refactor -> validate.
- Do not generate large rewrites, broad refactors, or speculative frameworks unless the task explicitly requires them.
- Build what is needed now. Avoid adding libraries, build tools, abstractions, or cross-platform machinery without an immediate reason.
- For this operator, write working plans, handoffs, and status updates in Russian unless asked otherwise. Public README, wiki, demo, release notes, and GitHub-facing docs may stay English-first.

## Agent Workflow Kernel

- Do not start coding vague work. For unclear features, run alignment first: ask focused questions, lock goal, non-goals, UX, data, tests, and acceptance.
- Feature work needs a destination before implementation: a short issue, feature brief, or equivalent written target.
- Slice vertically. Prefer one small end-to-end behavior over horizontal phases such as storage first, daemon later, UI last.
- Use TDD for non-trivial behavior changes and bug fixes: failing or updated test, minimal implementation, refactor, targeted checks.
- Use fresh-context review for meaningful changes. The reviewer should inspect issue/brief, diff, tests, ADR/rules, and validation output, not the implementation chat.
- Human/live QA remains required for Telegram-facing behavior when a live contour is available.
- After compaction, context reset, or handoff, reread this file first, then pull only the docs called out by the Context Pull Map.

## Context Pull Map

- Telegram UI, routing, observer panels, lifecycle, Plan Mode, diagnostics, or rendering: read `docs/testing/regression-map.md` and the ADR named there when present.
- Public command behavior or Telegram product contract: read `docs/research/contract-matrix.md` when present.
- New feature planning, bugfix shaping, or review handoff: read `docs/process/agent-workflow.md` and the related template when present.
- Release, README, demo, or public positioning work: read README plus relevant `docs/wiki/*`, `docs/demo/*`, `CONTRIBUTING.md`, and `SECURITY.md` when present.
- If a referenced document is absent in the current branch, do not invent it or add broken links. Use this `AGENTS.md`, nearby code/tests, and existing ADRs as the source of truth.

## Core Decisions

- Backend integration surface is only `codex app-server` over stdio.
- Durable identity is `threadId`; Telegram chat/topic is only an input and rendering surface.
- Live observer events come from the daemon session; foreign GUI/CLI activity must also be covered by polling `thread/read`.
- App Server session lifecycle transitions must be serialized and generation-aware; stale old-session close/error events must not invalidate newer sessions or create repair loops.
- Startup must remain non-blocking; never put full thread sync into synchronous startup.
- SQLite is the local source of truth for bindings, routes, callbacks, panels, observer target, delivery metadata, and daemon state.
- Do not add a second runtime backbone through `codex exec resume`, SDK-only wrappers, or MCP.

## Design And Code Principles

- Simple code beats clever code. Keep changes local and avoid making unrelated subsystems harder to understand.
- Prefer cohesive vertical slices and deep modules: expose small, clear interfaces that hide useful internal complexity.
- Keep modules focused. Each module should have one main reason to change.
- Make interfaces explicit with typed inputs/outputs, clear ownership, and precise error cases.
- Avoid duplication. If the same routing, rendering, lifecycle, or parsing rule appears twice, extract one source of truth.
- Inject or pass dependencies for IO, time, config, external services, and randomness. Do not hardcode them deep in business logic.
- Make invalid states hard to represent when practical, especially around thread/turn/panel lifecycle and callback routing.
- Use domain terms consistently: thread, turn, panel, observer target, route, callback, Plan prompt, Final Card, Details.
- Do not rename domain concepts casually. A renamed concept must improve clarity across code, tests, ADRs, and docs.
- Prefer guard clauses and early returns over deeply nested conditionals.
- Follow existing project conventions and standard formatters. Do not invent a new style.
- Comments should explain intent, constraints, tradeoffs, or non-obvious decisions. Do not comment obvious syntax.

## Telegram UX Invariants

- Global observer is a single target. `/observe all` moves it; `/observe off` disables passive monitoring.
- Foreign GUI/CLI runs must render in this order: `New run`, `[User]`, `[commentary]`, `[Tool]`, `[Output]`.
- If a foreign prompt is not available at panel creation, render a `[User]` placeholder and later edit the same message.
- Telegram-originated runs create `New run` and the live trio, but do not duplicate the user's Telegram message as `[User]`.
- Active runs keep live trio visibility. Completed runs collapse the summary message into one Final Card with Details view-state.
- `Details` pagination edits the same message; it must not create a stream of replacement pages.
- `Tools file` and `Get full log` are explicit on-demand exports. Automatic per-tool document spam is forbidden.
- Document exports should use in-memory multipart upload. Temp files are allowed only as fallback and must be cleaned up.
- Plan Mode waiting input is a separate routeable `[Plan]` prompt-card. Buttons are allowed only for structured choices provided by Codex.
- Telegram-originated Plan Mode starts must pass App Server `collaborationMode.mode = plan`; prompt wording alone is not Plan Mode.
- `/model` and `/effort` are Telegram button menus for collaboration-mode model settings. Persist selections in SQLite, not env-only local config. After a selection, remove the inline choice buttons from the edited message.
- Replies to active turns should steer the active turn. If steering is rejected while the thread still looks active, do not start a parallel turn.
- Stale active-turn ghosts are different: if `thread/read` exposes a final answer or `turn/steer` says `no active turn to steer`, follow the branch's lifecycle ADR or contract note when present and allow a new `turn/start` after re-read instead of returning a false parallel-turn warning.
- Every observer-visible message must include the shared identity header: emoji marker, project, thread, `T:`, `R:`, and kind.
- Emoji markers are visual hints only. Persisted message routes and callback tokens are the routing authority.

## Routing Precedence

1. Explicit thread id in a command.
2. Reply-to routed message.
3. Armed one-shot steer/answer state.
4. Bound thread for the current chat/topic.

## Collaboration Contract

- Screenshots, Telegram message ids, daemon logs, CI logs, and user-observed symptoms are primary evidence. Investigate them before generalizing.
- If private env/auth/chat/thread/session state is missing, stop and ask. Do not invent credentials, route ids, Telegram chat ids, thread ids, or live state.
- Keep an explicit original-blockers checklist in long tasks and state which initial problems are fixed, partially fixed, or still open.
- If the same symptom repeats, stop patching the surface and inspect the lower layer: app-server session lifecycle, poll/live race, terminal gating, routing state, or App Server state.
- Separate readback identity, Bot API delivery/routing identity, and durable Codex identity. Do not conflate Telegram readback chat, Bot API chat id, and `threadId`.

## Definition Of Done For Telegram-Facing Changes

- For UI, routing, observer, lifecycle, Plan Mode, Details, Markdown rendering, or callback changes, unit tests are not enough when a live contour is available.
- Done means: rebuild, restart daemon if needed, perform real Telegram readback, inspect edited messages, click/read callbacks when relevant, and correlate daemon logs or SQLite when lifecycle is involved.
- Bot API send/edit success alone is not sufficient.
- If live E2E is unavailable, state the exact blocker and mark the change as not fully validated.
- After changes, run the smallest relevant check first, then broader checks such as targeted tests, `go test ./...`, and `go build -buildvcs=false ./...`.

## Testing And TDD Discipline

- Use tests as the main feedback loop. Add or update tests for new behavior and bug fixes.
- Prefer TDD for non-trivial changes: write or update a failing test, make it pass, then refactor.
- Test public behavior through stable interfaces. Mock only external boundaries or slow/non-deterministic dependencies.
- Keep tests deterministic, isolated, and runnable without manual setup.
- If a check cannot be run, say exactly why and what should be run manually.
- For feature/test context before changing Telegram routing, observer panels, Plan Mode, diagnostics, Telegram rendering, or lifecycle recovery, read `docs/testing/regression-map.md` and the ADR named there when present.
- Behavior changes need the matching ADR or contract note, regression-map/test anchors when present, and validation notes when live Telegram was used.

## Branch, Commit, And Release Discipline

- Keep diffs focused on the requested task. Do not mix broad refactors with feature or bugfix work.
- When multiple branches are active, maintain a branch ledger: branch, purpose, base, latest commit, remote status, and done/not done.
- Before committing, check dirty tree, staged diff, `git config user.name`, `git config user.email`, tests, and secret scan.
- Before pushing, verify the current branch, upstream, ahead/behind state, and that the intended commits are included.
- Release or branch handoff summaries must include branch, commit hash, push status, checks run, docs/ADR changed, and daemon version/restart status when applicable.

## Repository Layout

- `cmd/ctr-go/` daemon CLI entrypoint.
- `internal/appserver/` JSON-RPC stdio client and snapshot normalization.
- `internal/config/` env-driven config.
- `internal/daemon/` runtime orchestration, observer, panels, callbacks, log exports.
- `internal/model/` shared types.
- `internal/storage/` SQLite schema and repositories.
- `internal/telegram/` Telegram Bot API transport.
- `internal/tgformat/` Markdown-to-Telegram entity renderer.
- `tests/` black-box and live-gated tests.
- `docs/` ADRs, wiki pages, demo docs, metrics, acceptance notes.

## Private Vs Public Context

- Keep private deep handoff/debug files outside the repo.
- Do not commit raw Telegram ids, local sessions, raw logs, private screenshots, local E2E runners, tokens, phone numbers, local paths, `.env`, databases, or runtime artifacts.
- Do not commit giant generated docs, logs, or plans.
- Public docs should describe durable behavior and contracts, not private investigation history.

## Change Rules

- Keep changes small, contract-preserving, and focused on the requested task.
- Remove dead code introduced by the change.
- Update README and public docs when public behavior changes.
- Add or update tests for routing, observer target, panel lifecycle, callbacks, Markdown/entity rendering, export behavior, and lifecycle recovery.
- Update ADRs when behavior or architecture changes. Supersede old ADRs instead of silently contradicting them.
- Do not hide uncertainty. Call out assumptions, remaining risks, and checks that could not be run.
- Final summaries should state what changed, what was tested, and what risk remains.

## Required Checks

Run before committing code changes:

```powershell
go test ./...
go build -buildvcs=false ./...
```

Run a targeted secret/local scan before committing or publishing:

```powershell
rg -n "BOT_TOKEN|TELEGRAM_BOT_TOKEN|api_hash|api_id|phone|password|secret|\\.session|\\.sqlite|\\.env|C:\\\\Users\\\\<private-user>" .
```

For docs-only changes, at minimum run `git diff --check`, a targeted secret/local scan on edited docs, and `git status --short`.

## Demo Rules

- Demo tests must be gated by explicit env vars and build tags.
- Screenshot demo content must be in English and safe for public display.
- Demo screenshots must be reviewed manually before committing.
- Do not fake product screenshots. Use the real Telegram renderer and Bot API.
