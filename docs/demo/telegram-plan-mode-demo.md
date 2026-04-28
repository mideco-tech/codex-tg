# Telegram Plan Mode Screenshot Demo

This demo sends a public-safe English sequence to a real Telegram chat. It is intended for creating `docs/assets/telegram-plan-mode-demo.png`.

## Run

```powershell
$env:CTR_DEMO_TELEGRAM_E2E = "1"
$env:CTR_DEMO_TELEGRAM_CHAT_ID = "<telegram-chat-id>"
$env:CTR_GO_TELEGRAM_BOT_TOKEN = "<telegram-bot-token>"
$env:CTR_DEMO_KEEP_MESSAGES = "true"
go test -tags demo_e2e ./tests -run TestTelegramPlanModeScreenshotDemo -count=1 -v
```

## Demo Prompt

```text
I am preparing for relocation and want to work as an LLM engineer.
Please review this repository as a public portfolio project:
run the Go test suite, inspect the architecture, and tell me whether it is ready for a v0.1 GitHub release.
```

## Expected Telegram Story

1. `[User]` with the relocation / LLM engineer portfolio prompt.
2. `[Plan]` asking for validation depth with `Fast smoke` and `Full test suite` buttons.
3. `[commentary]` with a short progress update.
4. `[Tool]` showing `go test ./...`.
5. `[Output]` showing short successful output.
6. `[Final]` with release-readiness feedback and buttons.

## Screenshot Guidance

- Crop the screenshot to Telegram messages only.
- Keep the `[User]`, `[Plan]`, `[Tool]`, `[Output]`, and `[Final]` cards visible.
- Do not include private usernames, chat ids, local filesystem paths, or bot tokens.
- Review the screenshot manually before committing it.

