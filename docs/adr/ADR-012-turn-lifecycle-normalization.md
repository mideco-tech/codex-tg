# ADR-012: Turn Lifecycle Normalization

- Status: accepted
- Supersedes: the absolute active-turn guard from ADR-006 and the active-reply note in `docs/research/contract-matrix.md`

## Context

`codex-tg` treats `codex app-server` as the only backend integration surface. In practice, `thread/read` can temporarily expose contradictory lifecycle state:

- the latest turn has a `final_answer` item, but the turn status still reads `inProgress`
- the thread carries an `activeTurnId`, but `turn/steer` rejects the same turn with `no active turn to steer`
- after a daemon restart or App Server session repair, SQLite can still hold an old `ActiveTurnID`

If the bridge trusts only the stale status fields, Telegram-originated input is rejected as a parallel-turn risk even though App Server no longer has an active turn. The operator sees:

```text
Codex did not accept input for active turn ... no active turn to steer.
I did not start a parallel turn.
```

That message is correct for a genuinely active-but-not-steerable turn, but wrong for a stale active-turn ghost.

## Decisions

- A `final_answer` item is terminal evidence unless the same snapshot is waiting on approval or user input.
- When `thread/read` contains a latest `final_answer` but the latest turn status is still non-terminal, normalize the bridge snapshot to:
  - `LatestTurnStatus = completed`
  - `Thread.Status = completed`
  - empty `Thread.ActiveTurnID` when it points at the finalized turn
- Metadata merge must not restore an old SQLite `ActiveTurnID` when the current App Server snapshot normalized to a terminal state.
- Telegram routing still prefers steering when a thread is genuinely active.
- `turn/steer` errors that indicate active/in-flight/not-steerable state still block fallback `turn/start`.
- `turn/steer` errors that explicitly say there is no active turn are stale-active evidence. The bridge re-reads the thread and then may fall through to `turn/start` instead of returning the parallel-turn warning.
- Telegram-originated turns remain marked by `thread_id + turn_id`. A global observer sync must not recreate a second panel/New run for the same marked turn if a Telegram-origin panel already exists.
- Diagnostic logs for this lifecycle are structured, sanitized, and bounded. They may include thread/turn ids, route source, operation names, durations, item counts, and redacted stderr tails, but must not include prompt bodies, tokens, sessions, or unbounded file dumps.
- Telegram-visible rendering treats missing, null, empty, and literal `"<nil>"` payload values as absent. App Server and rollout payloads may omit `command`, `arguments`, `request_id`, `phase`, `status`, or related fields while a turn is still active; the bridge must render empty text or a neutral fallback instead of leaking Go stringification artifacts to Telegram.

## Session Lifecycle Contract

The daemon owns one live App Server session and one poll App Server session at a time. Startup, reconcile, close, and repair must be serialized and generation-aware:

- concurrent startup/reconcile paths must not create duplicate live subscriptions or live event loops
- old live loops must not clear `liveConnected`, clear `liveEvents`, or request repair after a newer generation has started
- repair processes pending repair before reconcile starts replacement sessions
- stale old-session close/error events may be logged with `current=false`, but they must not invalidate the current session
- lifecycle diagnostics may include role, generation, operation, thread/turn ids, durations, and sanitized errors, but not raw prompt text, local session paths, SQLite paths, env paths, or unbounded stderr

## Transient Empty Interrupted Contract

App Server can transiently report a Telegram-origin turn as `interrupted` with only a user item before later recovering the same turn to `inProgress` with tool/commentary/final items. The bridge treats that shape as ambiguous drift:

- applies only to Telegram-origin turns
- empty `interrupted` means no final text/fingerprint, no agent messages, no tool id/label/output, no waiting approval/reply, no active session-tail tool, and no detail kinds except `user`
- without an explicit `/stop`, empty `interrupted` is deferred for a short grace window instead of being compacted, rendered terminal, or logged terminal
- if the same turn later shows active/waiting/tool/commentary/output/final evidence, the defer marker is cleared and normal rendering resumes
- if the defer window expires, the empty `interrupted` is accepted as terminal
- explicit `/stop` or Stop button writes an explicit interrupt marker so its terminal `interrupted` bypasses deferral

## Nil-Safe Rendering Contract

Nil-safe rendering is part of turn lifecycle normalization because the bad state usually appears during the same active-to-terminal drift window:

- all Telegram-facing extraction should flow through nil-safe helpers such as `payloadString`, `payloadMapString`, `firstPayloadString`, or `rpcString`
- command rendering must skip nil-like slice/map values and omit the command when no meaningful label exists
- session-tail tool labels and output overlays must use neutral fallbacks or remain empty instead of rendering `"<nil>"`
- summary, tool, output, Details, and delivery text must be cleaned before Telegram entity generation, so Markdown entity offsets stay correct
- diagnostics may log a bounded `telegram_render_contains_nil` marker with ids, panel kind, message id, text length, and text hash, but never the full rendered text
- local live E2E for this area must read edited Telegram messages, not only new bot messages, because `"<nil>"` can appear transiently during panel edits

## Consequences

- A stale `inProgress` status no longer blocks the next Telegram message when App Server has already finalized the prior turn.
- The "do not start a parallel turn" guard remains in place for real active-turn conflicts.
- Final Card collapse can happen even when App Server status lags behind the final item.
- Telegram UI must never expose literal `"<nil>"`; missing App Server fields are treated as data-shape drift, not as meaningful user-facing content.
- Duplicate live App Server session loops are treated as lifecycle bugs, not harmless diagnostics.
- Empty Telegram-origin `interrupted` snapshots do not erase active UI or create a false terminal card until confirmed.
- New agents should debug lifecycle issues from normalized snapshot behavior, not raw `thread/read` status alone.
- Tests must cover both parts of the contract:
  - final-answer normalization clears active turn state
  - `no active turn to steer` falls back to a new turn
  - active-but-not-steerable errors still block fallback
  - global observer does not duplicate Telegram-origin panels
  - nil-like payload values do not leak into command, Details, summary, tool, output, or RPC request rendering
  - old live loops cannot clear newer session state
  - empty Telegram-origin `interrupted` is deferred, recovered, expired, or bypassed for explicit stop

## Non-goals

- This does not introduce a second backend runtime or `codex exec resume`.
- This does not treat every steer failure as safe to retry with `turn/start`.
- This does not rewrite old Telegram history or delete already-sent duplicate cards automatically.
