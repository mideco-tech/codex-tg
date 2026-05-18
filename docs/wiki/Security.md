# Security

`codex-tg` is local-first.

## Defaults

- Codex App Server runs locally over stdio.
- Telegram access is restricted by allowed user/chat ids.
- SQLite state stays on the operator machine.
- `ctr-go init` stores local configuration in `~/.codex-tg/config.env` by default.
- `ctr-go service install` creates a user LaunchAgent whose environment contains
  only `CTR_GO_CONFIG`.
- If proxy variables are needed for network access, they are stored in the
  private `config.env` and applied by the process after startup; proxy URLs may
  contain credentials, so treat the config file as secret material.
- The macOS tray app controls service lifecycle and opens local files, but it
  does not read or display Telegram tokens.

## Never Commit

- `.env`
- Bot tokens
- `config.env`
- Telegram user sessions
- Chat ids from private deployments
- SQLite databases
- Logs
- Private screenshots

## Network Boundary

Do not expose Codex App Server on a public interface. App Server stays local or
private to the operator machine.

Future control-plane APIs must default to loopback-only or unix-socket-only
access. Public network listeners, cloud brokers, and unauthenticated local
control surfaces require a separate ADR.

Telegram, tray, voice, and future HTTP/mobile surfaces are adapters. They must
not bypass Codex approvals, sandboxing, allowlists, or App Server lifecycle
guards.

Voice wake-word adapters must treat transcription as untrusted input. A spoken
request can route to Codex, but it must not auto-approve sensitive file,
command, permission, or MCP requests.

Secrets stay in the local config file today. A future Keychain migration is
allowed, but runtime docs and logs must continue to avoid printing secrets in
full.
