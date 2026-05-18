# Architecture

`codex-tg` is moving from a Telegram-first bridge toward a local Codex Control
Plane. The current production adapter is Telegram; future adapters can include
tray workflows, local HTTP/unix-socket clients, voice assistants, or a separate
router agent.

```text
Channel adapters
  - Telegram
  - macOS tray
  - future voice / router / local API
        |
        v
codex-control core
  - thread and turn lifecycle
  - event normalization
  - approvals and user input
  - notification policy
  - durable routing state
        |
        v
Codex connectors
  - App Server
  - optional SDK/MCP orchestration adapters
        |
        v
local Codex sessions and workspaces
```

## Current Runtime

The v0.4 runtime still runs as a Go daemon with:

- Telegram Bot API long polling;
- route and callback handling;
- observer and panel rendering;
- SQLite state;
- local Codex App Server connectivity.

The current implementation starts `codex app-server` over stdio. That remains
supported. ADR-019 allows future work to prepare official App Server `unix://`
and `app-server proxy` transports when they improve lifecycle safety.

## Integration Surface

App Server is the authoritative control surface for interactive Codex state:
threads, turns, approvals, user input, live events, history, and snapshots.

SDK and MCP integrations may be used as orchestration adapters for router-agent
workflows, Agents SDK handoffs, traces, or multi-agent experiments. They do not
replace App Server state for live rendering, Details, approvals, or notification
truth in v0.5.

## Observer Model

Live App Server notifications are used for daemon-owned runs. Foreign GUI/CLI
runs are covered through bounded `thread/read` polling. Local session JSONL is
reserved for explicit log exports, not live observer state.

## State

SQLite stores routes, callback tokens, bindings, observer target, panels,
pending prompts, delivery metadata, and daemon state.

Future control-plane work should keep adapter-specific state, such as Telegram
message ids, outside the control core wherever practical.

## Further Reading

- [Control Plane](Control-Plane.md)
- [ADR-019: Codex Control Plane](../adr/ADR-019-codex-control-plane.md)
