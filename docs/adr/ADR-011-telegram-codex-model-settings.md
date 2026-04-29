# ADR-011: Telegram Codex Model Settings

- Status: accepted
- Decision: model and reasoning-effort selection for Telegram-started collaboration-mode turns is a Telegram UI feature backed by SQLite state.

## Context

Telegram can start Codex Plan Mode turns through `/plan`, `/plan_mode`, and `/reply --plan`. App Server `collaborationMode` requires concrete settings such as model and optional reasoning effort, but these settings should not be hardcoded in environment variables because operators need to see and change them from the same Telegram control surface they use to start runs.

The settings are operator preferences for future Telegram-started collaboration-mode turns, not per-message content and not observer-rendered history.

## Decisions

- `/settings` shows the current Codex model and reasoning effort used for Telegram-started collaboration-mode turns.
- `/model` opens a button menu populated from App Server `model/list`.
- `/effort` opens a button menu for reasoning effort. When a selected model advertises supported reasoning efforts, the menu is narrowed to that model.
- Selecting a model or reasoning effort persists the value in SQLite daemon state.
- Selecting `Auto` clears the explicit value and lets App Server defaults or discovered defaults apply.
- After a selection, the menu message is edited into a compact settings summary without inline choice buttons. The operator can reopen menus with `/settings`, `/model`, or `/effort`.
- These settings are not public env configuration and must not introduce local-only secrets or private identifiers into docs or commits.

## Consequences

- Settings survive daemon restarts because SQLite remains the local source of truth.
- Telegram UX avoids stale/replayable choice menus after a selection.
- App Server remains the source of model availability; the bridge does not maintain a static model catalog.
- Unit tests must cover menu rendering, persistence, and button removal after selection.
- Live Telegram validation should verify rendered menus and callback behavior when a live contour is available.
