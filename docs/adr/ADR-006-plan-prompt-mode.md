# ADR-006: Plan Prompt Mode

- Status: accepted
- Decision: Codex Plan Mode waiting input is rendered as a separate `[Plan]` prompt-card with structured Telegram actions.

## Decisions

- A waiting Plan Mode prompt is rendered as a separate `[Plan]` prompt-card, not as a Final Card state and not as a passive tool/output message.
- Telegram-originated Plan Mode runs must be started with App Server `turn/start` `collaborationMode.mode = plan`; prompt wording alone is not treated as Plan Mode.
- Telegram exposes Plan start commands through `/plan <text>`, `/plan_mode <text>`, `/plan <thread> <text>`, `/plan_mode <thread> <text>`, and `/reply --plan <thread> <text>`.
- Default Mode reset UX is superseded by ADR-016: Plan Final Cards expose `Turn off Plan`, and `/stop` arms a one-shot reset for the next ordinary turn. Hidden `/default` and `/reply --default` compatibility paths may still pass `collaborationMode.mode = default`, but they are not public commands.
- `/plan <text>` and `/plan_mode <text>` use normal routing precedence after the command: reply-to route, armed state, then bound thread.
- `/plan <thread> <text>` and `/plan_mode <thread> <text>` are treated as explicit-thread commands only when the first token is a known thread id or UUID-like Codex thread id. Unknown plain words remain part of the prompt text for the implicit route.
- `/reply --plan <thread> <text>` stays strict and requires an explicit thread id.
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
- Command parsing must not steal the first prompt word as a thread id for `/plan <text>`.
- Structured buttons must remain safe to replay and reconstruct from persisted card metadata or callback payloads.
- Synthetic polling fallback must dedupe by stable thread/turn state and avoid creating duplicate Plan prompt-cards for the same waiting state.
- Live Telegram e2e is required for Plan UI changes because unit tests cannot prove reply-first routing, card callbacks, and backend fallback behavior together.
