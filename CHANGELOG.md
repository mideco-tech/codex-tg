# Changelog

## Unreleased

## v0.2.6 - 2026-05-06

- Added a Plan Mode reset contract: Plan-like Final Cards expose `Turn off Plan`, and `/stop <thread>` arms the same one-shot reset.
- The next ordinary Telegram-origin `turn/start` after reset is sent with `collaborationMode.mode = default`, then the reset is cleared after a successful start.
- Kept `/default` and `/reply --default` as hidden compatibility fallbacks while removing `/default` from the Telegram command menu, `/help`, README, and user-facing docs.
- Hardened `/stop` so completed threads with stale `active_turn_id` are treated as idle instead of attempting a timed-out `turn/interrupt`.
- Added ADR-016, unit coverage for reset state/callback guards, and the opt-in live E2E `plan_mode_reset` case.

## v0.2.5 - 2026-05-06

- Added the Telegram notification contract: `New run` is configurable, `[Plan]` and `[Final]` notify, while live progress, direct responses, menus, callbacks, exports, and errors are sent silently.
- Changed Final rendering to send a new `[Final]` message and best-effort delete transient live cards, preserving Details routing on the new Final Card.
- Fixed tool-only runs so live current tools are not erased by stale polling snapshots and completed tool-only commands remain visible in Details and Tools file.
- Added explicit Default Mode escape hatches through `/default` and `/reply --default` for threads that remain in App Server Plan Mode.
- Added ADR-015, contract/regression documentation, release validation notes, Telegram transport tests for `disable_notification`, and live E2E coverage for notification and tool-only Details behavior.

## v0.2.4 - 2026-05-05

- Changed `/newchat <prompt>` to create a real Codex UI Chat folder under `CTR_GO_CODEX_CHATS_ROOT` before starting the new thread.
- Added `/newthread <prompt>` as the no-Chat-folder escape hatch for starting without project selection.
- Added `CTR_GO_CODEX_CHATS_ROOT`, command-menu coverage, unit coverage for prompt slug/collision behavior, and ADR-014 for the new Chat folder contract.
- Added an opt-in `newchat_folder` live Telegram E2E case and validated `/newchat`, `/newthread`, `/context`, and generated folder behavior on macOS.

## v0.2.3 - 2026-05-05

- Fixed the public CI secret-pattern smoke scan by replacing private-looking Windows path fixtures in tests with `C:\Users\you\...` examples.
- Added `/newchat` to the Telegram Bot API command menu registration so the UI command button exposes the existing new Chat flow.
- Added unit coverage for the Telegram command menu to keep `/newchat` registered and prevent duplicate command names.
- Rebuilt and restarted the macOS LaunchAgent daemon, then verified Telegram `getMyCommands` readback includes `newchat`.

## v0.2.2 - 2026-05-05

- Fixed a Details binding regression where pressing `Details` on an older completed run could render the latest run and `Back` could replace the older Final Card with the newer one.
- Bound Details callbacks to their original panel/card using `panel_id`, thread, turn, chat/topic, and message-id guards, with stale callbacks failing closed instead of falling back to the current panel.
- Restored Details rendering for older turns when App Server raw payloads use the nested `{ "thread": { "turns": [...] } }` shape.
- Added unit coverage for old/new completed run panels, missing `panel_id`, mismatched message ids, and mismatched `Tools file` routes.
- Added the checked-in `details_binding` live Telegram E2E case and validation notes for Details, `Tool on`, `Tools file`, and `Back`.

## v0.2.1 - 2026-05-05

- Preserved the live `Current tool:` display for Telegram-origin turns when durable polling temporarily reports an older completed tool from the same turn.
- Added a live E2E regression case that exercises two long-running progress-printing commands and fails if `[Tool]` reverts to an older completed command while the newer tool is still running.
- Split Codex UI `Documents/Codex` chats into a dedicated `Chats` navigation area under `/projects`, with recency sorting, pagination, `/newchat <prompt>`, and configurable preview/page limits.
- Improved `/projects` readability: project buttons now use `N. Project name`, Chat buttons use `Chat N. Thread name`, and visible internal `key:` rows were replaced with `last thread:`.
- Updated contract, regression, validation, quickstart, and Telegram UX docs for the Projects/Chats navigation and v0.2.1 live validation.

## v0.2.0 - 2026-05-04

- Added a normalized App Server live event layer for `item/*`, `turn/*`, `thread/status/changed`, and legacy `codex/event/*` notifications.
- Restored honest current-command visibility for Telegram-origin turns: `[Tool]` now shows `Current tool:` from live App Server events during execution, then returns to `Last completed tool:` after completion.
- Preserved v0.1.3 safety for foreign GUI/CLI observer panels: they still show only completed tool/output state from durable `thread/read`.
- Hardened App Server client lifecycle with per-process generations so stale stdout/stderr, responses, server requests, and notifications from a closed session cannot affect the next session.
- Preserved Telegram-origin live current tool state when a same-turn `thread/read` snapshot omits in-progress tool details, while still allowing completed/final snapshots to reconcile durable state.
- Upgraded live Telegram E2E harness with selectable cases, `sleep20_timing` current-tool acceptance, and a multi-tool current/completed transition scenario.
- Added docs, ADR/regression-map updates, validation notes, and public release notes for the v0.2.0 live event refactor.

## v0.1.3 - 2026-05-04

- Added project-first thread creation from Telegram: `/projects` opens cached workspaces by normalized `cwd`, project menus expose `New thread`, and the next message starts a new App Server thread in that project.
- Added `/new <project-key-or-number> <prompt>` as a direct new-thread shortcut for cached projects.
- Bound the chat/topic to the newly created thread after success, seeded the Telegram-origin snapshot, and started hot polling for the first turn.
- Preserved created thread recovery when the first `turn/start` fails, while refusing to start a turn if `thread/start` does not return a thread id.
- Scoped Plan answer buttons to the current turn so stale `user_input` choices from an older turn cannot appear under a newer `[commentary]` card.
- Made synthetic Plan fallback neutral (`Input required.`) instead of reusing stale thread preview text from a previous turn.
- Added docs, regression map entries, unit coverage, and live Telegram readback validation for the new project-thread flow and Plan fallback behavior.

## v0.1.2 - 2026-05-03

- Made the Telegram live trio honest about App Server visibility: `[commentary]` owns whole-run timing, `[Tool]` shows the last completed tool, and `[Output]` shows the last completed tool output.
- Added `Run active for: ...` while a run is active and `Run duration: ...` on the terminal `[Final]` card.
- Removed running-tool preservation from compact snapshots so missing App Server tool state cannot be rendered as an authoritative current command.
- Hardened late live-tool handling so older turn/tool updates cannot overwrite newer completed state.
- Retired session-tail overlay from live UI paths; session JSONL remains only for explicit exports/full-log flows.
- Added public-safe Telegram live E2E coverage for sequential commands, `sleep 20`, and a multi-command math run that verifies last-completed tool/output updates and run timing.
- Filtered internal/ephemeral App Server threads from public thread lists.
- Updated ADR/testing notes for the correctness-over-current-command-visibility contract.

## v0.1.1 - 2026-04-29

- Added bounded daemon diagnostics for Telegram-originated turn lifecycle, app-server calls, session repair, transport failures, and first terminal status of Telegram-originated turns.
- Normalized App Server snapshots that contain a `final_answer` but still report `inProgress`, clearing stale active-turn state before Telegram routing.
- Treated `no active turn to steer` as stale active-turn evidence so Telegram input can start a new turn instead of returning a false parallel-turn warning.
- Prevented global observer sync from recreating duplicate `New run` panels for Telegram-originated turns already represented by a Telegram input panel.
- Added `Get thread id` to live summary and Final Card actions so operators can copy full thread/turn ids without SQLite or logs.
- Sanitized Telegram-visible rendering so missing App Server command/status/request fields never appear as literal `"<nil>"`, with unit coverage and a local live nil-guard E2E path documented.
- Serialized App Server session lifecycle repair/startup so stale old live loops cannot clear newer sessions or create duplicate live subscriptions.
- Gated transient Telegram-origin empty `interrupted` snapshots so Telegram does not collapse into a false terminal card before App Server catch-up.
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
