# ADR-013: Retire Session Tail Tool Overlay

- Status: accepted
- Supersedes: `docs/adr/ADR-008-session-tail-tool-overlay.md`

## Context

The session-tail overlay was added so the observer could show in-flight GUI/CLI tool calls before `thread/read` materialized them. In practice it created a second live source of truth for `[Tool]` and `[Output]`.

When the JSONL tail contains an older tool call without a matched completion event, the overlay can render a stale command that is unrelated to the current App Server snapshot. That is worse for the operator than a temporarily empty tool panel.

## Decision

- App Server state is the only live source for Telegram tool, output, final, turn, and progress state.
- Durable snapshots come from App Server `thread/read`; in-flight command visibility may use App Server live `item/started`, `item/updated`, and `item/completed` notifications for the same snapshot/render path.
- The daemon must not read local Codex session JSONL files to supplement live `[Tool]`, `[Output]`, Final Card, Details, hot polling, or terminal-gate state.
- Session JSONL may still be used for explicit operator-requested exports such as full logs.
- For foreign GUI/CLI observer runs, the live trio does not promise current command visibility. `[commentary]` owns whole-run timing, `[Tool]` renders the last completed tool, and `[Output]` renders the last completed tool output.
- For Telegram-origin turns, App Server live `item/started` and `item/updated` notifications may render the current command in `[Tool]` only when the event belongs to the marked `thread_id + turn_id`.
- A Telegram-origin current command is still an App Server live event projection, not a session-tail overlay. Completion must flow back into the same snapshot/detail path used by polling reconciliation.
- Missing completed tool fields render as neutral absence, such as `No completed tool yet.`, not as `"<nil>"` and not as a guessed command from JSONL.
- If App Server does not expose a current GUI/CLI command quickly enough on a platform through either `thread/read` or live notifications, that is an App Server/integration drift issue to validate with live E2E, not a reason to add a second live runtime backbone.

## Consequences

- The observer favors correctness over early command visibility.
- Stale commands from prior session-tail entries cannot be resurrected into current Telegram panels.
- Polling and lifecycle gates rely on App Server state plus existing normalization only; live notifications can update snapshot/detail history but do not create a separate session-tail overlay.
- Telegram-origin current-tool visibility is allowed because the daemon owns the `turn/start` and can validate the marked turn before rendering.
- Foreign current-tool visibility remains out of scope until the bridge has a similarly authoritative live source for those runs.
- Regression coverage must include a stale session JSONL command next to a `thread/read` snapshot with no tool activity; Telegram output and compact snapshots must not contain the JSONL command.
