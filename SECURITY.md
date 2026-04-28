# Security Policy

## Supported Model

`codex-tg` is designed as a local-first bridge:

- Codex App Server is started locally over stdio.
- Telegram is the remote input/rendering surface.
- SQLite state stays on the operator machine.
- Bot access must be restricted with allowed user/chat ids.

## Do Not Expose

- Do not expose Codex App Server on a public network interface.
- Do not publish bot tokens, Telegram user sessions, `.env` files, SQLite databases, logs, or screenshots with private data.
- Do not run the bot in public groups unless access control and routing are explicitly reviewed.

## Reporting

For security issues, open a private advisory if GitHub Security Advisories are enabled. Otherwise, open an issue with minimal reproduction details and without secrets.

