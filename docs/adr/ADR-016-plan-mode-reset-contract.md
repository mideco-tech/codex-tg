# ADR-016: Plan Mode Reset Contract

- Status: accepted
- Decision: expose a low-risk, one-shot Default Mode reset for threads that remain in Plan Mode.

## Context

Codex App Server may keep a thread in Plan Mode after a completed Telegram-originated Plan turn. If the operator then sends an ordinary prompt, Codex can answer with another plan/refusal instead of running tools. A visible escape hatch is needed, but the bridge should not add another public command or invent a separate reset run.

## Decisions

- Plan-like Final Cards show `Turn off Plan` when the completed turn contains Plan details or a matching Plan prompt.
- `Turn off Plan` is a panel-bound callback. Its payload must include `panel_id`, and the callback must match the panel chat/topic, thread, turn, and Final message id before any state changes.
- Pressing `Turn off Plan` sets local daemon state `thread_collaboration_override.<thread_id> = default` and edits the same Final Card without the button.
- `/stop <thread>` sets the same Default override after route resolution, even if the thread is already idle. It keeps the existing interrupt behavior when an active turn exists.
- The override is one-shot. The next ordinary `turn/start` with no explicit collaboration mode sends `collaborationMode.mode = default`, then clears the override after `turn/start` succeeds.
- If that `turn/start` fails, the override remains for the next retry.
- Explicit collaboration modes keep priority. `/plan`, `/reply --plan`, hidden `/default`, hidden `/reply --default`, and related explicit paths pass their requested mode to App Server. A successful explicit start clears any stale override.
- `/default` and `/reply --default` remain hidden compatibility fallbacks but are not advertised in the Telegram command menu, `/help`, README, or user-facing docs.

## Non-Goals

- No App Server protocol changes.
- No DB migration.
- No reset-only `turn/start`.
- No Details/Back routing changes beyond using the existing panel guard for `Turn off Plan`.
- No runtime use of full logs or session JSONL to detect Plan Mode.

## Consequences

- The operator can leave Plan Mode without memorizing another command.
- The reset is applied only at the next safe App Server boundary: a normal `turn/start`.
- A stale button cannot reset a newer or different run because the callback fails closed through the same panel binding rules as Details.
- Live Telegram E2E remains required for this behavior because the important outcome is user-visible: the next normal prompt must execute instead of producing another Plan Mode refusal.
