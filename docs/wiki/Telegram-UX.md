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

The active commentary card owns run status and becomes `[Final]` when the final answer is available. `[Final]` shows final-answer text/status only; completed commentary and tool/output history stay in Details. Details pagination edits the same message instead of sending more messages, and Details/Back buttons stay bound to the completed run card that created them.

## Plan Mode

Telegram can start a Codex Plan Mode turn with `/plan <thread> <text>`, `/plan_mode <thread> <text>`, or `/reply --plan <thread> <text>`. These commands use App Server `turn/start` with `collaborationMode.mode = plan`; prompt wording alone is not treated as Plan Mode.

When Codex asks for input, the bridge renders a separate routeable `[Plan]` prompt-card. Replying to that card answers the same run. Structured buttons appear only when Codex provides choices.

Plan answer buttons are scoped to their own turn. A pending Plan prompt from an
older turn must not appear under a newer `[commentary]` card for the same
thread.

## New Threads

`/projects` opens a project/workspace menu from cached Codex thread metadata.
The `New thread` action arms the current chat/topic so the next plain-text
message creates a new App Server thread in that project cwd and starts the first
turn with that text.

Cached threads whose cwd is under `Documents/Codex` or the configured
`CTR_GO_CODEX_CHATS_ROOT` are treated as Codex UI `Chats`, not normal projects.
The main `/projects` view shows recent project workspaces and the latest Chat
previews; `Open Chats` opens the full paginated Chat list. Selecting a Chat
opens and binds that single thread.

New Chat starts use `/newchat <prompt>`. The bridge creates a dated Chat folder
under the configured Chats root, passes that cwd to App Server `thread/start`,
and starts the first turn in that cwd. `/newthread <prompt>` is the separate
escape hatch for starting without project selection or Chat folder creation;
App Server may still report the daemon default cwd for that thread.

The Telegram bot does not accept arbitrary filesystem paths for this flow.
Creating or editing project work directories is a separate future feature.

## Codex Settings

`/settings`, `/model`, and `/effort` expose Telegram button menus for model selection and reasoning effort used by Telegram-started collaboration-mode turns. The selections are stored in SQLite daemon state and are not configured through public env vars.

After a model or reasoning-effort selection, the menu message is edited into a compact settings summary without inline choice buttons. Use `/settings`, `/model`, or `/effort` to reopen the menus.

## Exports

- `Tools file`: on-demand file for selected Details tool/output.
- `Get full log`: on-demand archive from Codex session JSONL.

Automatic tool-output document spam is intentionally forbidden.
