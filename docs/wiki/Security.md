# Security

`codex-tg` is local-first.

## Defaults

- Codex App Server runs locally through `auto`, falling back to stdio when no shared local endpoint is available.
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

`ws://` transport is accepted only for loopback endpoints. Non-loopback WebSocket transport is out of scope until there is an authenticated upstream transport story.

Desktop Bridge runtime sockets and pairing payloads are local runtime artifacts. Do not commit them, and do not paste private socket paths from diagnostics into public issues.
