# Security

`codex-tg` is local-first.

## Defaults

- Codex App Server runs locally over stdio.
- Telegram access is restricted by allowed user/chat ids.
- SQLite state stays on the operator machine.

## Never Commit

- `.env`
- Bot tokens
- Telegram user sessions
- Chat ids from private deployments
- SQLite databases
- Logs
- Private screenshots

## Network Boundary

Do not expose Codex App Server on a public interface. Telegram is the remote surface; App Server stays local.

