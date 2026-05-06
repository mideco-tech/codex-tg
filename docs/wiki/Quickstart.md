# Quickstart

## 1. Create a Telegram Bot

Create a bot with BotFather and keep the token private.

## 2. Configure Environment

```powershell
$env:CTR_GO_TELEGRAM_BOT_TOKEN = "<telegram-bot-token>"
$env:CTR_GO_ALLOWED_USER_IDS = "<telegram-user-id>"
$env:CTR_GO_DEFAULT_CWD = "C:\Users\you\Projects\Codex"
$env:CTR_GO_CODEX_CHATS_ROOT = "C:\Users\you\Documents\Codex"
# Optional: set to "off" to keep New run visible but silent.
$env:CTR_GO_NOTIFY_NEW_RUN = "on"
```

## 3. Run

```powershell
go run ./cmd/ctr-go daemon run
```

## 4. Enable Observer

In Telegram:

```text
/start
/observe all
/threads
/projects
```

Start or continue a Codex thread from GUI/CLI. The bot should render the run in Telegram.
Only `New run`, `[Plan]`, and `[Final]` use normal Telegram notifications. Live progress cards, menus, and exports are sent silently.

Use `/plan` or `/reply --plan` for Plan Mode. If a thread remains in Plan Mode,
start the next turn with `/default <thread> <prompt>` or `/reply --default
<thread> <prompt>` to force App Server Default Mode.

To start a new thread from Telegram, open `/projects`, choose a project, press
`New thread`, then send the first prompt as the next message. The selected
project must already exist in the cached Codex thread list.

Codex UI Chats stored under `Documents/Codex` appear under the `Chats` section
instead of as normal projects. Use `Open Chats` for the full paginated list, or
`/newchat <prompt>` to create a new Codex UI Chat under
`Documents/Codex/<date>/<prompt-slug>`. Use `/newthread <prompt>` for a thread
without choosing a project or creating a Chat folder; App Server may still
attach the daemon default cwd.
