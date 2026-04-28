# ADR-005: User Request Notice

- Status: accepted
- Decision: foreign Codex GUI/CLI user prompts are duplicated into Telegram as a separate `[User]` notice before the live panel.

## Decisions

- A user prompt discovered through observer polling or live Codex state is rendered as a separate `[User]` Telegram message.
- The `[User]` notice is an orientation marker for a run and is not deleted when the run collapses into a Final Card.
- Dedupe is persisted per panel/run through `last_user_notice_fp`.
- Telegram-originated `/reply` and plain-text input are not duplicated, because the user's original Telegram message is already visible.
- Telegram-originated turns are marked by `thread_id + turn_id`, so later observer/poll resync cannot recreate a `[User]` notice for the same prompt.
- The `[User]` notice is routed to the same Codex `thread_id`, `turn_id`, and user item id, so replying to it still targets the correct thread.

## Consequences

- Operators can see what GUI/CLI request started a run before reading commentary, tools, output, or the Final Card.
- The notice is independent from Final Card and Details state.
- Parser support must handle real App Server `userMessage.content[].text`, not only simplified `userMessage.text`.
- Live Telegram e2e must cover both sides: GUI/CLI prompts produce `[User]`, Telegram-originated prompts do not.
