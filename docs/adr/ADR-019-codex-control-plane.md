# ADR-019: Codex Control Plane

- Status: accepted
- Amends: ADR-001, `docs/wiki/Architecture.md`
- Related: ADR-013, ADR-015, ADR-018

## Context

`codex-tg` was built as a Telegram remote UI and observer for local Codex App
Server. That was useful while the project was the primary mobile control
surface. Official Codex Remote Connections now cover the broad "continue Codex
from a phone" workflow, including remote threads, approvals, notifications, and
host resources.

The project still has a valuable local control layer: thread routing, turn
lifecycle normalization, live event handling, approval/input routing, Details,
logs, service management, and notification policy. Those capabilities should be
made usable by future adapters and a router agent instead of remaining embedded
in Telegram command handlers.

Codex App Server has also grown beyond the original stdio-only assumption. The
local Codex CLI exposes `stdio://`, `unix://`, WebSocket, and proxy transports,
and the generated App Server schema includes thread, turn, event, skill, hook,
MCP, app, config, filesystem, command, review, and realtime surfaces.

## Decision

- `codex-tg` moves toward a local Codex Control Plane.
- App Server remains the authoritative source for interactive Codex threads,
  turns, approvals, live events, history, and snapshots.
- Telegram becomes the first channel adapter, not the product center.
- SDK and MCP integrations are allowed as orchestration adapters when they
  complement App Server, for example Agents SDK workflows, handoffs, traces, or
  multi-agent router experiments.
- SDK/MCP adapters must not replace App Server state for live thread rendering,
  approval routing, Details, or notification truth unless a future ADR explicitly
  changes that contract.
- Spawned stdio App Server remains supported. Future work may prepare `unix://`
  and `app-server proxy` transports to decouple daemon lifecycle from Codex
  session lifecycle.
- Session JSONL remains out of live UI/control state. It may be used only for
  explicit exports and diagnostics, preserving ADR-013.

## Consequences

- The old "only App Server over stdio and no SDK/MCP runtime backbone" rule is
  superseded for new control-plane work.
- New architecture should expose adapter-independent control interfaces before
  adding new UI channels.
- Telegram behavior remains protected by its existing contracts and tests.
- Router-agent and voice-assistant work should consume the control core instead
  of copying Telegram-specific routing logic.
- Public positioning should describe `codex-tg` as a local Codex control layer
  with a Telegram adapter, not as a replacement for official Remote Connections.

## Non-goals

- No public App Server exposure.
- No cloud broker.
- No immediate HTTP API.
- No immediate voice assistant.
- No downgrade of Telegram E2E requirements for Telegram-facing changes.
- No generated schema committed as source of truth.
