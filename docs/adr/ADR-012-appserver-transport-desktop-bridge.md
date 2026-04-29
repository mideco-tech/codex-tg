# ADR-012: App Server Transport And Desktop Bridge

- Status: accepted
- Decision: `codex-tg` supports configurable local App Server transports and an experimental Desktop Bridge visibility adapter.

## Context

Telegram-originated turns are live in Telegram because `codex-tg` subscribes to the App Server process it uses. Codex Desktop GUI live visibility is different: live state is process and transport local, and persisted thread data alone is not enough to make Desktop show a bot-started run as active.

The public App Server integration surface supports JSON-RPC over local transports such as stdio, loopback WebSocket, and WebSocket-over-Unix-domain-socket. Desktop and official IDE integrations may also use a product bridge / IPC pairing layer for live attachment. `codex-tg` needs to stop treating persisted rollout state as a GUI live-sync mechanism.

References:

- [OpenAI Codex App Server docs](https://developers.openai.com/codex/app-server)
- [Upstream app-server README](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)
- [OpenAI Codex changelog](https://developers.openai.com/codex/changelog)

## Decisions

- App Server execution still goes only through `codex app-server`; no `codex exec resume`, SDK-only wrapper, MCP, or second runtime backbone is introduced.
- The JSON-RPC client is transport-independent. Supported modes are `auto`, `stdio`, `ws`, `unix`, and `desktop_bridge`.
- `stdio` preserves the previous child-process JSONL behavior and remains the fallback path.
- `ws` uses one JSON-RPC message per WebSocket text frame and is limited to loopback endpoints.
- `unix` uses WebSocket-over-UDS and defaults to `$CODEX_HOME/app-server-control/app-server-control.sock`, or `$HOME/.codex/app-server-control/app-server-control.sock` when `CODEX_HOME` is unset.
- `auto` probes `unix`, then an explicitly configured local `ws` or `unix` endpoint, then falls back to `stdio`.
- Runtime selection lives in SQLite daemon state:
  - `appserver.transport_mode`
  - `appserver.endpoint`
  - `desktop_bridge.enabled`
  - `desktop_bridge.mode`
- Telegram exposes selection through `/settings` and `/appserver_transport`; after a button selection, the same message is edited into a compact summary without inline choice buttons.
- `/codex_status` is the diagnostic command for transport mode, active endpoint class, probe failures, loaded thread count when available, Desktop Bridge state, and expected GUI visibility.
- Desktop Bridge is experimental. When enabled, `codex-tg` writes a local pairing payload and opens a local UDS server with a fake-desktop-testable JSONL message layer for `client-status-changed`, `thread-stream-state-changed`, `thread-read-state-changed`, and `thread-follower-*` requests.
- Desktop Bridge failures are diagnostic, not fatal. If Desktop does not attach or does not complete the experimental JSONL exchange, the daemon stays healthy and reports `registered/not connected`, `registered/protocol unverified`, or an error in `/codex_status`.

## Consequences

- Telegram remains live in all modes because it follows the daemon App Server session.
- Desktop GUI live visibility is expected only when a shared transport or working bridge attach exists. In stdio-only mode, Desktop may catch up after refresh, focus, restart, or turn completion.
- Local transport diagnostics must redact private socket paths and credentials.
- Non-loopback `ws://` is unsupported until upstream provides a safe authenticated remote transport story.
- Unit tests must cover fake JSON-RPC transport flows, WebSocket frames, WebSocket-over-UDS, auto fallback, Telegram settings persistence, `/codex_status` redaction, and fake Desktop Bridge messages.
