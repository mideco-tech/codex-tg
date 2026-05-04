# Quickstart

## 1. Create a Telegram Bot

Create a bot with BotFather and keep the token private.

## 2. Configure Environment

```powershell
$env:CTR_GO_TELEGRAM_BOT_TOKEN = "<telegram-bot-token>"
$env:CTR_GO_ALLOWED_USER_IDS = "<telegram-user-id>"
$env:CTR_GO_DEFAULT_CWD = "C:\Users\you\Projects\Codex"
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

To start a new thread from Telegram, open `/projects`, choose a project, press
`New thread`, then send the first prompt as the next message. The selected
project must already exist in the cached Codex thread list.
