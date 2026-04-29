# Validation Notes

This file captures validation nuances that are useful for agents and maintainers. It is not a list of release blockers.

For feature-to-test ownership, see `docs/testing/regression-map.md`.

## Runtime and documented UX contract

The public docs describe the intended observer/UI contract:

- `/observe all` moves one global observer target.
- `/observe off` disables passive monitoring.
- foreign GUI/CLI runs render as `New run -> [User] -> [commentary] -> [Tool] -> [Output]`.
- completed runs collapse the summary card into `[Final]` with Details, leaving status on `[commentary]`/`[Final]` instead of `New run` or `[User]`.
- `Tools file` and `Get full log` are explicit on-demand exports.

When changing this behavior, update the ADRs and run both unit tests and a live Telegram E2E path if a bot token is available.

## App Server drift

`codex app-server` is the integration surface, but its thread/read payloads and live notifications can drift across Codex versions. Tests should cover snapshot normalization, plan prompts, active tool overlay, and fallback parsing.

Important areas to re-check after Codex upgrades:

- `agentMessage.phase` classification for commentary versus final answers.
- `status.activeFlags` values such as `waitingOnUserInput` and `waitingOnInput`.
- stale lifecycle snapshots where a latest turn has `final_answer` but still reports `inProgress`.
- `turn/steer` error text for stale turns, especially `no active turn to steer`.
- tool call shape in live notifications and JSONL session tails.
- availability of `thread.raw_json.thread.path` for full-log exports.
- missing or null command/request/status fields that could render as literal `"<nil>"`.

## Stale active turns

`thread/read` status alone is not sufficient to decide whether Telegram input would start a parallel turn. The bridge normalizes a latest `final_answer` to completed state and clears the active turn before routing. If a steer attempt returns `no active turn to steer`, the bridge re-reads the thread and may start a new turn. Errors that imply an active or not-steerable turn still block fallback.

Regression tests for this area should cover:

- final-answer normalization from stale `inProgress`.
- no resurrection of SQLite `ActiveTurnID` after a terminal snapshot.
- fallback `turn/start` after `no active turn to steer`.
- no fallback for active-but-not-steerable failures.
- no duplicate global-observer panel for a marked Telegram-origin turn.

## Session lifecycle and empty interrupted drift

Live Telegram-origin turns can expose two independent forms of App Server drift:

- duplicate live event loops after daemon startup or repair when session lifecycle is not serialized.
- transient empty `interrupted` snapshots that later recover to `inProgress` for the same turn.

Regression tests should cover:

- startup/reconcile/repair cannot create duplicate live subscriptions.
- stale old live loops cannot clear newer session state or trigger repair loops.
- empty Telegram-origin `interrupted` does not compact or render terminal during the grace window.
- explicit `/stop` accepts `interrupted` immediately.
- recovered turns clear the defer marker and continue normal live panel rendering.

## Routing precedence

The product contract remains:

1. explicit thread id in a command.
2. reply-to routed message.
3. armed one-shot steer or answer state.
4. bound thread for the current chat/topic.

Route correctness must be tested through persisted message routes and callback tokens, not by parsing rendered Telegram headers.

## Storage-backed tests

The runtime depends on SQLite through `modernc.org/sqlite`. Broad `go test ./...` coverage expects `go.sum` to be committed and the local Go toolchain to be available.

Validation commands:

```powershell
go test ./...
go build -buildvcs=false ./...
git diff --check
```

## Live Telegram validation

Bot API send/edit success is not enough for user-facing changes. For Telegram UI, routing, callbacks, Plan Mode, Details, Markdown formatting, or observer behavior, verify the rendered result with a real Telegram readback when possible.

## Nil-safe Telegram rendering validation

Literal `"<nil>"` in Telegram is treated as a rendering bug. It usually means App Server or rollout data omitted a field that Go code stringified with `%v`.

Validation expectations:

- unit tests cover nil-like map/slice extraction, command rendering, session-tail overlay labels, summary Markdown rendering, App Server RPC id stringification, and snapshot string normalization.
- live validation must read edited Telegram messages, not only newly delivered messages.
- the ignored local runner `~/.codex-tg/e2e/nil_guard_e2e.py` exercises fast text, fast command, and a roughly one-minute active tool/output window against a dedicated private test thread.
- Details view should be checked during E2E when the button is available.
- do not commit the runner, Telegram session, target thread id, raw message ids, logs, or screenshots.

Latest local validation note:

- 2026-04-29 macOS live nil-guard E2E completed all three scenarios and found no literal `"<nil>"` in edited New run, summary/Final, Tool, Output, or Details messages after the sanitizer change.

## Turn lifecycle live E2E

The ignored local runner `~/.codex-tg/e2e/turn_lifecycle_e2e.py` validates Telegram-origin lifecycle behavior. It must use MTProto readback, not Bot API polling, and it must require `CODEX_TG_E2E_THREAD_ID` so it never defaults to the current operator thread.

Acceptance checks:

- fast Telegram-origin turn reaches `[Final]`.
- reply to a Final Card can route to a new turn without a false parallel-turn warning.
- immediate next turn after final does not hit stale active state.
- active-turn guard does not start a parallel turn.
- Details edits the same message.
- daemon logs for the scenario window contain no duplicate live session starts, no premature empty interrupted terminal, and no `telegram_render_contains_nil`.
- read-only SQLite correlation maps the visible Final Card to the expected thread/panel.

Do not commit local runners, Telegram user sessions, chat/thread ids, raw message ids, raw logs, env files, or screenshots.
