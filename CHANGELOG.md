# Changelog

## Unreleased

- Added bounded daemon diagnostics for Telegram-originated turn lifecycle, app-server calls, session repair, transport failures, and first terminal status of Telegram-originated turns.
- Normalized App Server snapshots that contain a `final_answer` but still report `inProgress`, clearing stale active-turn state before Telegram routing.
- Treated `no active turn to steer` as stale active-turn evidence so Telegram input can start a new turn instead of returning a false parallel-turn warning.
- Prevented global observer sync from recreating duplicate `New run` panels for Telegram-originated turns already represented by a Telegram input panel.
- Added `Get thread id` to live summary and Final Card actions so operators can copy full thread/turn ids without SQLite or logs.
- Sanitized Telegram-visible rendering so missing App Server command/status/request fields never appear as literal `"<nil>"`, with unit coverage and a local live nil-guard E2E path documented.
- Serialized App Server session lifecycle repair/startup so stale old live loops cannot clear newer sessions or create duplicate live subscriptions.
- Gated transient Telegram-origin empty `interrupted` snapshots so Telegram does not collapse into a false terminal card before App Server catch-up.

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
