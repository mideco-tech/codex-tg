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

`codex app-server` is the integration surface, but its thread/read payloads and live notifications can drift across Codex versions. Tests should cover snapshot normalization, plan prompts, tool rendering from `thread/read`, and fallback parsing.

Important areas to re-check after Codex upgrades:

- `agentMessage.phase` classification for commentary versus final answers.
- `status.activeFlags` values such as `waitingOnUserInput` and `waitingOnInput`.
- stale lifecycle snapshots where a latest turn has `final_answer` but still reports `inProgress`.
- `turn/steer` error text for stale turns, especially `no active turn to steer`.
- tool call shape in live notifications and `thread/read` snapshots.
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

## Session lifecycle and interrupted drift

Live Telegram-origin turns can expose two independent forms of App Server drift:

- duplicate live event loops after daemon startup or repair when session lifecycle is not serialized.
- transient non-final `interrupted` snapshots that later recover to `inProgress` or `completed` for the same turn.

Regression tests should cover:

- startup/reconcile/repair cannot create duplicate live subscriptions.
- stale old live loops cannot clear newer session state or trigger repair loops.
- Telegram-origin `interrupted` without a final answer does not compact or render terminal during the grace window, even when partial tool/output evidence is already present.
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

- unit tests cover nil-like map/slice extraction, command rendering, stale session-tail command suppression, summary Markdown rendering, App Server RPC id stringification, and snapshot string normalization.
- live validation must read edited Telegram messages, not only newly delivered messages.
- the checked-in public-safe harness `tests/live_e2e/telegram_readback_e2e.py` exercises sequential `pwd`, `date`, `printf`, and slow command updates against a dedicated private test thread configured only through local env.
- when validating stale-command regressions manually, prefer one private test-thread turn that runs several safe shell commands sequentially as separate tool calls; watch Telegram `MessageEdited` updates for the same `[Tool]` message and verify it changes only through the current commands or neutral empty state.
- for in-progress command visibility, verify the slow command appears in `[Tool]` before its output appears in `[Output]`; this proves App Server live item notifications are reaching the same render path before `thread/read` completion.
- Details view should be checked during E2E when the button is available.
- do not commit Telegram sessions, target thread ids, raw message ids, logs, env files, or screenshots.

Latest local validation note:

- 2026-04-30 PDT macOS live complex `/reply` E2E reproduced a transient partial `interrupted` snapshot with tool/output evidence before final completion. After extending the terminal gate to defer non-final Telegram-origin `interrupted`, the same E2E passed: sequential command updates stayed visible, then a multi-command number-theory task created a temporary helper and ran four Python range commands before reaching `[Final]` with `COUNT=2034 SUM=115514223`; no visible interrupted state, literal `"<nil>"`, stale command, or input rejection appeared.
- 2026-04-30 PDT macOS live logging-flags E2E used MTProto readback against a private test thread after rebuilding and restarting the daemon. It verified sequential `pwd`, `date`, `printf 'alpha\nbeta\n'`, and `sleep 20; printf 'slow-command-done\n'` tool updates, with the slow command visible in `[Tool]` about 20 seconds before `[Output]`; a separate `/reply` math run reached `[Final]` with the expected answer and no visible interrupted state. Daemon diagnostics remained present with default logging flags.
- 2026-04-30 PDT macOS live stale-command E2E used MTProto readback of `MessageEdited` updates for one private test-thread turn with `pwd`, `date`, `printf 'alpha\nbeta\n'`, and `sleep 20; printf 'slow-command-done\n'`. `[Tool]` showed the slow command in progress about 20 seconds before `[Output]` contained `slow-command-done`; no literal `"<nil>"` or stale session-tail command appeared.
- 2026-04-29 macOS live nil-guard E2E completed all three scenarios and found no literal `"<nil>"` in edited New run, summary/Final, Tool, Output, or Details messages after the sanitizer change.

## Turn lifecycle live E2E

The checked-in public-safe harness `tests/live_e2e/telegram_readback_e2e.py` validates Telegram-origin lifecycle behavior. It must use MTProto readback, not Bot API polling, and it must require `CODEX_TG_LIVE_E2E=1` plus `CODEX_TG_E2E_THREAD_ID` so it never defaults to the current operator thread.

Acceptance checks:

- sequential command run reaches `[Final]`.
- slow command becomes visible in `[Tool]` before `[Output]` contains its completion text.
- complex `/reply` math run uses multiple shell commands and reaches `[Final]` with the expected aggregate answer.
- Telegram readback contains no false parallel-turn warning, literal `"<nil>"`, stale known command, or visible non-final `Status: interrupted`.
- optional daemon log correlation for the scenario window contains no premature interrupted terminal without a final answer, no input rejection, and no `telegram_render_contains_nil`.

Do not commit Telegram user sessions, chat/thread ids, raw message ids, raw logs, env files, or screenshots.
