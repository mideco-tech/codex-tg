# ADR-003: Telegram Observer/UI v2

- Status: accepted
- Decision: the Telegram operator surface moves from additive observer feeds to a single global observer target with per-thread summary panels.

## Decisions

- Global observer monitoring is default-on when the runtime can resolve a single operator DM or another bootstrap target.
- `/observe all` moves the global observer target to the current chat/topic.
- `/observe off` disables global background monitoring.
- Observer state is rendered through a summary panel keyed by `(chat, project, thread)`.
- The summary panel is the only background message that carries thread actions such as `Stop` and `Steer`.
- Tool/output stream messages are informational only and must not carry buttons.
- Final answers are delivered as separate messages and expose on-demand log retrieval through `Получить полный лог`.

## Consequences

- The old model of `main DM + N explicit observer feeds` is replaced by a single active observer target.
- Documentation must no longer assume that observer mode requires a dedicated read-only feed chat.
- Delivery and dedupe rules must distinguish:
  - summary-panel updates
  - passive tool/output stream messages
  - final-answer messages
  - on-demand full-log deliveries
- The runtime still needs explicit routing rules for free-text input; summary-panel actions do not replace normal thread routing.

## Open details that remain runtime-owned

- Whether the default-on target is always the implicit main DM or may be restored from persisted state.
- Whether `Steer` opens a dedicated reply flow, emits a command hint, or switches the next plain-text message into steer mode.
- The exact retention and truncation policy for `Получить полный лог`.
