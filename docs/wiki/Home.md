# codex-tg Wiki

`codex-tg` is a Telegram remote UI and observer for local OpenAI Codex App Server.

## Start Here

- [Quickstart](Quickstart.md)
- [Architecture](Architecture.md)
- [Telegram UX](Telegram-UX.md)
- [Plan Mode](Plan-Mode.md)
- [ADR-011: Telegram Codex Model Settings](../adr/ADR-011-telegram-codex-model-settings.md)
- [Security](Security.md)
- [Operations](Operations.md)
- [Demo](Demo.md)

## Core Idea

Keep Codex local, but make its threads observable and controllable from Telegram.

The daemon owns Telegram polling, Codex App Server stdio sessions, SQLite state, observer polling, and route/callback handling.
