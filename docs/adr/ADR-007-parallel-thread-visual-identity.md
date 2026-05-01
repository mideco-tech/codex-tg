# ADR-007: Parallel Thread Visual Identity

- Status: accepted
- Decision: the Telegram observer remains DM-first, and every Codex thread/run message gets a deterministic visual identity header.

## Decisions

- The primary operator surface remains one Telegram DM observer target.
- Each thread gets a stable emoji marker plus short text chips in every user-facing observer card.
- The default header shape is:
  `emoji [Project] [Thread] [T:thread] [R:run] [Kind]`
- Emoji markers are visual hints only. Routing correctness depends on persisted message routes and callback route tokens, not rendered text.
- Short `T:` and `R:` chips are copy hints only. They are not route authority and are not sufficient for explicit commands that need the full Codex id.
- `/context` must show the full bound `Thread ID` when a thread is available.
- Live summary and Final Card actions expose `Get thread id`, which sends a separate copyable message with full `Thread ID` and `Turn ID`.
- Marker assignment is deterministic by `threadId`, but the runtime avoids active marker collisions while palette slots are available.
- Marker assignments remain reserved for 30 minutes after the last render, reducing confusing reuse in nearby chat history.
- When active threads exceed the palette, the runtime may reuse a base emoji with suffixes such as `#2`.
- Every observer message must carry source identity, including `[Output]`, Details, exported Details tools, observer event cards, approval cards, and full-log captions.
- A new foreign GUI/CLI run may emit a one-time `New run` divider before the `[User]`, `[Plan]`, or live trio messages.

## Consequences

- Renderers must use a shared identity header helper instead of hand-written `[Project] [Thread]` strings.
- The first release after this ADR may edit existing panels because header hashes change.
- Tests must assert identity presence by message kind and route metadata, not exact string prefixes like `[Final]`.
- Tests must cover full-id access through `/context`, live summary cards, and Final Cards.
- Live Telegram e2e for UI changes must include at least two parallel Codex threads, so marker/chip separation is verified in the real chat.

## Non-goals

- Telegram forum topics are not introduced by this decision.
- Emoji uniqueness is not a correctness guarantee.
- Header text is not parsed for routing.
