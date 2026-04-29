# Architecture

```text
Telegram Bot API
      |
      v
codex-tg Go daemon
  - Telegram long polling
  - route and callback handling
  - observer and panel rendering
  - SQLite state
      |
      v
codex app-server over local transport
  - auto
  - stdio
  - loopback ws
  - local unix socket
      |
      v
local Codex sessions and workspaces
```

## Integration Surface

The daemon uses `codex app-server` as the only execution/control backend. It does not use `codex exec resume`, MCP, or SDK-only wrappers as the control plane.

Transport selection is runtime state in SQLite. `auto` probes a local Unix App Server control socket, then an explicit local endpoint, then falls back to stdio. `desktop_bridge` enables an experimental Desktop visibility adapter while execution still goes through the same App Server session.

## Observer Model

Live notifications are used for daemon-owned runs. Foreign GUI/CLI runs are covered through bounded `thread/read` polling and session-tail overlay for active tools.

Codex Desktop GUI live visibility is expected only when Desktop and `codex-tg` share a live App Server transport or when the experimental Desktop Bridge attaches successfully. Persisted rollout/thread state alone is not treated as live GUI sync.

## State

SQLite stores routes, callback tokens, bindings, observer target, panels, pending prompts, delivery metadata, and App Server transport settings.
