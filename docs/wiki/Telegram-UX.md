# Telegram UX

## Run Chronology

Foreign GUI/CLI runs render as:

```text
New run
[User]
[commentary]
[Tool]
[Output]
[Final]
```

`New run`, `[Tool]`, and `[Output]` are deleted best-effort when the final card is rendered. `[User]` remains as a historical marker.

`New run` identifies the source of the run. `[User]` carries the request text. Neither card shows run status.

## Final Card

The active commentary card owns run status and becomes `[Final]` when the final answer is available. `[Final]` shows final-answer text/status only; completed commentary and tool/output history stay in Details. Details pagination edits the same message instead of sending more messages.

## Plan Mode

Telegram can start a Codex Plan Mode turn with `/plan <thread> <text>`, `/plan_mode <thread> <text>`, or `/reply --plan <thread> <text>`. These commands use App Server `turn/start` with `collaborationMode.mode = plan`; prompt wording alone is not treated as Plan Mode.

When Codex asks for input, the bridge renders a separate routeable `[Plan]` prompt-card. Replying to that card answers the same run. Structured buttons appear only when Codex provides choices.

## Codex Settings

`/settings`, `/model`, and `/effort` expose Telegram button menus for model selection and reasoning effort used by Telegram-started collaboration-mode turns. `/settings` also includes an App Server menu for selecting `auto`, `stdio`, `unix`, `ws`, or experimental `desktop_bridge`.

The selections are stored in SQLite daemon state and are not configured through public env vars.

After a model, reasoning-effort, or App Server transport selection, the menu message is edited into a compact settings summary without inline choice buttons. Use `/settings`, `/model`, `/effort`, or `/appserver_transport` to reopen or change the menus.

`/codex_status` is the detailed transport diagnostic. It reports active transport, safe probe results, Desktop Bridge state, and whether Codex GUI live visibility is expected from the current mode.

## Exports

- `Tools file`: on-demand file for selected Details tool/output.
- `Get full log`: on-demand archive from Codex session JSONL.

Automatic tool-output document spam is intentionally forbidden.
