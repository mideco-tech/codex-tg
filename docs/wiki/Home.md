# codex-tg Wiki

`codex-tg` is a Telegram remote UI and observer for local OpenAI Codex App Server.

## Start Here

- [Quickstart](Quickstart.md)
- [Architecture](Architecture.md)
- [Telegram UX](Telegram-UX.md)
- [Plan Mode](Plan-Mode.md)
- [Security](Security.md)
- [Operations](Operations.md)
- [Demo](Demo.md)

## Core Idea

Keep Codex local, but make its threads observable and controllable from Telegram.

The daemon owns Telegram polling, Codex App Server stdio sessions, SQLite state, observer polling, and route/callback handling.

