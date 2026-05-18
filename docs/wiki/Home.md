# codex-tg Wiki

`codex-tg` is a local Codex Control Plane with Telegram as its first production
adapter.

## Start Here

- [Quickstart](Quickstart.md)
- [Architecture](Architecture.md)
- [Control Plane](Control-Plane.md)
- [Telegram UX](Telegram-UX.md)
- [Plan Mode](Plan-Mode.md)
- [ADR-011: Telegram Codex Model Settings](../adr/ADR-011-telegram-codex-model-settings.md)
- [Security](Security.md)
- [Operations](Operations.md)
- [Demo](Demo.md)

## Core Idea

Keep Codex local, but make its threads observable and controllable through
stable private adapters.

The daemon currently owns Telegram polling, Codex App Server connectivity,
SQLite state, observer polling, and route/callback handling. The v0.5 direction
extracts the Codex control layer so future router agents, voice assistants, and
local APIs can reuse it without copying Telegram-specific behavior.
