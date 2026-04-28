# ADR-009: Run Orientation Card Ordering

- Status: superseded by ADR-010
- Supersedes: the standalone `New run` divider portion of ADR-007 for new global-observer panels

## Context

Foreign Codex GUI/CLI runs can be discovered by the observer before `thread/read` exposes the originating user prompt. If the live trio is created first and `[User]` is sent later, Telegram cannot insert that later message above the already-sent `[commentary]`, `[Tool]`, and `[Output]` messages.

## Decision

- A new foreign global-observer run starts with one orientation card before the live trio.
- If the user prompt is already known, the orientation card is rendered immediately as `[User]`.
- If the prompt is not known yet, the orientation card is rendered as `New run`.
- When the prompt appears later, the daemon edits that existing orientation card into `[User]` instead of appending a new message below the trio.
- The orientation card keeps a DB message route. When edited into `[User]`, the route is updated to the exact `thread_id`, `turn_id`, and user item id.
- Telegram-originated runs still do not get a duplicated `[User]` card.
- Legacy panels that do not have an orientation card must not append late `[User]` messages. They may render a compact `[User]` block inside the summary panel and mark the fingerprint as handled.
- `[Plan]` prompt-cards stay separate because they are actionable waiting-input cards, not passive orientation.

## Consequences

- The visible order for new foreign runs is stable: orientation card first, then summary/tool/output trio.
- Late user prompts no longer appear below active tool/output messages.
- Old Telegram history is not rewritten; the rule applies to new panels and future sync cycles.
- Routing correctness remains based on persisted message routes and callback tokens, not rendered text.

## Non-goals

- No attempt is made to move or reorder already-sent Telegram messages.
- This ADR does not change Final Card, Details, Plan Mode, or Telegram-originated input behavior.
