# ADR-015: Telegram Notification Contract

- Status: accepted
- Supersedes: the terminal finalization message-edit detail in ADR-004 and ADR-010.

## Context

Telegram notification volume can become noisy when every observer card is sent as a normal message. The operator still needs audible attention for genuinely important events: a new run, a final answer, and a Plan Mode question that needs a choice or reply.

Telegram edits do not provide a reliable per-edit notification contract. Therefore a `[Final]` that is produced by editing the live `[commentary]` card cannot be made audible without also making the earlier commentary message audible.

## Decision

- New messages are silent by default through Telegram Bot API `disable_notification=true`.
- Audible messages are limited to:
  - `New run`, controlled by `CTR_GO_NOTIFY_NEW_RUN` and enabled by default;
  - a newly sent `[Final]` card;
  - a routeable `[Plan]` prompt-card for user input or structured choices.
- `[commentary]`, `[Tool]`, `[Output]`, `[User]`, command/menu responses, explicit exports, and fallback/error messages are silent.
- Finalization sends a new `[Final]` card, records its message route, moves the panel summary message id to that new card, and then best-effort deletes the old `[commentary]`, `New run`, `[Tool]`, and `[Output]` messages.
- `[User]` remains as historical request context.
- Details and Back callbacks remain bound to the completed run panel/card. After finalization, the panel's summary message id is the new `[Final]` message id.

## Consequences

- The operator receives fewer notifications while preserving alerts for run start, required Plan input, and run completion.
- The completed-run surface is still one stable Final/Details message, but it is no longer the same Telegram message that previously held live commentary.
- Old routes for deleted live commentary messages may remain in SQLite, but active callback routing uses the new Final card message id.
- Cleanup failures for old live messages must not fail Final delivery.

## Non-goals

- This does not add per-chat notification profiles.
- This does not make Telegram edits audible.
- This does not change App Server protocol or Plan Mode routing.
