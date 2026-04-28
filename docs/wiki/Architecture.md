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
codex app-server over stdio
      |
      v
local Codex sessions and workspaces
```

## Integration Surface

The daemon uses `codex app-server` over stdio. It does not use `codex exec resume`, MCP, or SDK-only wrappers as the control plane.

## Observer Model

Live notifications are used for daemon-owned runs. Foreign GUI/CLI runs are covered through bounded `thread/read` polling and session-tail overlay for active tools.

## State

SQLite stores routes, callback tokens, bindings, observer target, panels, pending prompts, and delivery metadata.

