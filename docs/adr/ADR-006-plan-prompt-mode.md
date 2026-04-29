# ADR-006: Plan Prompt Mode

- Status: accepted
- Decision: Codex Plan Mode waiting input is rendered as a separate `[Plan]` prompt-card with structured Telegram actions.

## Decisions

- A waiting Plan Mode prompt is rendered as a separate `[Plan]` prompt-card, not as a Final Card state and not as a passive tool/output message.
- Telegram-originated Plan Mode runs must be started with App Server `turn/start` `collaborationMode.mode = plan`; prompt wording alone is not treated as Plan Mode.
- Telegram exposes explicit Plan start commands through `/plan <thread> <text>`, `/plan_mode <thread> <text>`, and `/reply --plan <thread> <text>`.
- The collaboration-mode `model` and `reasoning_effort` are configurable through Telegram button menus backed by SQLite state, with app-server defaults used when no explicit selection exists. The menu lifecycle is specified in ADR-011.
- The `[Plan]` prompt-card is reply-first: the operator should be able to reply to that card with the next instruction without first using a command.
- Buttons on the `[Plan]` prompt-card are structured actions only. They must not encode arbitrary free-text prompts.
- Plan prompt routing is scoped to the exact `threadId`, `turnId`, and `requestId` when `requestId` is available.
- Observer polling may synthesize a Plan prompt-card when Codex exposes a waiting Plan state without a `requestId`; this fallback must remain scoped by thread and turn and must not invent a durable request id.
- Sending the operator response should prefer `turn/steer` for the waiting Plan prompt, then fall back to `turn/start` when steering is not accepted by the backend state.
- Hanif may be used only as a product reference for Plan prompt behavior and UX expectations. Its code, storage shape, and implementation details are not copied into this runtime.

## Consequences

- `[Plan]` is a live operator input surface with its own lifecycle and route metadata.
- Reply routing must preserve enough metadata to distinguish a Plan prompt from normal thread binding, `[User]` notices, summary panels, and Final Cards.
- Structured buttons must remain safe to replay and reconstruct from persisted card metadata or callback payloads.
- Synthetic polling fallback must dedupe by stable thread/turn state and avoid creating duplicate Plan prompt-cards for the same waiting state.
- Live Telegram e2e is required for Plan UI changes because unit tests cannot prove reply-first routing, card callbacks, and backend fallback behavior together.
