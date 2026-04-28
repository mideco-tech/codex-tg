# Validation Notes

This file captures validation nuances that are useful for agents and maintainers. It is not a list of release blockers.

## Runtime and documented UX contract

The public docs describe the intended observer/UI contract:

- `/observe all` moves one global observer target.
- `/observe off` disables passive monitoring.
- foreign GUI/CLI runs render as `New run -> [User] -> [commentary] -> [Tool] -> [Output]`.
- completed runs collapse the summary card into `[Final]` with Details.
- `Tools file` and `Get full log` are explicit on-demand exports.

When changing this behavior, update the ADRs and run both unit tests and a live Telegram E2E path if a bot token is available.

## App Server drift

`codex app-server` is the integration surface, but its thread/read payloads and live notifications can drift across Codex versions. Tests should cover snapshot normalization, plan prompts, active tool overlay, and fallback parsing.

Important areas to re-check after Codex upgrades:

- `agentMessage.phase` classification for commentary versus final answers.
- `status.activeFlags` values such as `waitingOnUserInput` and `waitingOnInput`.
- tool call shape in live notifications and JSONL session tails.
- availability of `thread.raw_json.thread.path` for full-log exports.

## Routing precedence

The product contract remains:

1. explicit thread id in a command.
2. reply-to routed message.
3. armed one-shot steer or answer state.
4. bound thread for the current chat/topic.

Route correctness must be tested through persisted message routes and callback tokens, not by parsing rendered Telegram headers.

## Storage-backed tests

The runtime depends on SQLite through `modernc.org/sqlite`. Broad `go test ./...` coverage expects `go.sum` to be committed and the local Go toolchain to be available.

Validation commands:

```powershell
go test ./...
go build -buildvcs=false ./...
git diff --check
```

## Live Telegram validation

Bot API send/edit success is not enough for user-facing changes. For Telegram UI, routing, callbacks, Plan Mode, Details, Markdown formatting, or observer behavior, verify the rendered result with a real Telegram readback when possible.
