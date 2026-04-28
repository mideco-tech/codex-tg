# Demo

Use the live Telegram demo to create a screenshot for the README.

```powershell
$env:CTR_DEMO_TELEGRAM_E2E = "1"
$env:CTR_DEMO_TELEGRAM_CHAT_ID = "<telegram-chat-id>"
$env:CTR_GO_TELEGRAM_BOT_TOKEN = "<telegram-bot-token>"
$env:CTR_DEMO_KEEP_MESSAGES = "true"
go test -tags demo_e2e ./tests -run TestTelegramPlanModeScreenshotDemo -count=1 -v
```

The demo uses a public-safe English prompt and sends `[User]`, `[Plan]`, `[commentary]`, `[Tool]`, `[Output]`, and `[Final]` cards through the real Telegram Bot API.

Review the screenshot manually before committing `docs/assets/telegram-plan-mode-demo.png`.

