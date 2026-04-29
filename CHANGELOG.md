# Changelog

## Unreleased

- Added configurable App Server transport modes: `auto`, `stdio`, loopback `ws`, local `unix`, and experimental `desktop_bridge`.
- Added `/settings` App Server menu, `/appserver_transport`, and `/codex_status` diagnostics backed by SQLite daemon state.
- Added WebSocket and WebSocket-over-UDS JSON-RPC transports with auto-probe fallback to stdio.
- Added experimental Desktop Bridge pairing and fake-client-testable JSONL message layer for bridge status, stream notifications, and `thread-follower-*` control requests.
- Documented that Desktop GUI live visibility requires a shared transport or bridge; stdio-only remains Telegram-live but may only appear in Desktop after catch-up.

## v0.1.1 - 2026-04-29

- Changed observer chronology so `New run` is an orientation card without run status.
- Kept run status only on live `[commentary]` and terminal `[Final]` cards.
- Stopped `[User]` cards from showing or updating run status after the prompt is delivered.
- Made terminal catch-up collapse directly into `[Final]` when final text is available, while preserving the existing guard against historical observer fan-out.
- Kept completed commentary/tool/output history in Details instead of the final card body.
- Added Telegram-originated Plan Mode starts through `/plan`, `/plan_mode`, and `/reply --plan`, using App Server `collaborationMode: plan`.
- Added `/settings`, `/model`, and `/effort` Telegram button menus for Telegram-started collaboration-mode model settings, with choice buttons removed after a selection.
- Guarded active-thread replies so Telegram does not start a parallel turn when an active turn cannot be steered.
- Added `ctr-go version`.
- Verified the macOS daemon path on macOS 26.3.1 arm64 with Go 1.26.2, LaunchAgent startup, build, and Telegram readback/status check.
