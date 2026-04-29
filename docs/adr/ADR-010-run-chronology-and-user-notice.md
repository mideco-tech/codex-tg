# ADR-010: Run Chronology And User Notice

- Status: accepted
- Supersedes: ADR-009

## Context

The Telegram observer is also a security surface: if a local GUI/CLI or another process sends a new request into a Codex thread, the operator must see that a request happened. Editing a `New run` card into `[User]` hides the original run-start signal and loses chronology.

## Decision

- `New run` and `[User]` are separate Telegram messages.
- For GUI/CLI or poll-discovered runs, the order is always `New run`, `[User]`, `[commentary]`, `[Tool]`, `[Output]`.
- If the user prompt is not available when a run is discovered, the bridge sends a lightweight `[User]` placeholder and later edits that placeholder into the actual prompt.
- For Telegram-originated runs, the user request is already visible as the operator's Telegram message, so the bridge sends `New run` and the live trio without a duplicated `[User]`.
- `New run` is an orientation card with source metadata only. It does not own run status.
- `[User]` is delivered once as request context. It does not show run status and is not updated for status-only changes.
- The live `[commentary]` card owns run status while the run is active.
- At terminal finalization, `New run`, `[Tool]`, and `[Output]` are deleted best-effort. `[User]` remains as historical request context, and `[commentary]` is edited into `[Final]`.
- `[Final]` shows final-answer text and status only; completed commentary/tool/output history is available through Details.
- DB message routes and callback tokens remain the routing source of truth.

## Consequences

- New foreign runs keep both security visibility and chronological readability.
- Placeholder `[User]` cards are rare fallback state, not a normal user-facing mode.
- Old Telegram history is not rewritten; this applies to new panels and future sync cycles.
- Legacy panels without a stored `[User]` card are not retroactively repaired.

## Non-goals

- This does not introduce Telegram forum topics.
- This does not change Plan prompt-card behavior.
- This does not parse rendered text for routing.
