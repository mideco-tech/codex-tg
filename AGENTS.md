# AGENTS

## Repository Purpose

`codex-tg` is a Go daemon that turns Telegram into a thread-first remote UI and observer for local OpenAI Codex App Server.

The repo is public-facing. Keep every change safe for open-source publication: no private paths, tokens, Telegram ids, local sessions, databases, logs, screenshots with private data, or environment-specific credentials.

## Core Decisions

- Backend integration surface is only `codex app-server` over local transports: `auto`, `stdio`, loopback `ws`, local `unix`, and experimental `desktop_bridge`.
- `stdio` remains the fallback transport. Desktop Bridge is a visibility/control-plane adapter, not a second runtime backbone.
- Durable identity is `threadId`; Telegram chat/topic is only an input and rendering surface.
- Live observer events come from the daemon session; foreign GUI/CLI activity must also be covered by polling `thread/read`.
- Startup must remain non-blocking; never put full thread sync into synchronous startup.
- SQLite is the local source of truth for bindings, routes, callbacks, panels, observer target, delivery metadata, and daemon state.
- Do not add a second runtime backbone through `codex exec resume`, SDK-only wrappers, or MCP.

## Telegram UX Invariants

- Global observer is a single target. `/observe all` moves it; `/observe off` disables passive monitoring.
- Foreign GUI/CLI runs must render in this order: `New run`, `[User]`, `[commentary]`, `[Tool]`, `[Output]`.
- If a foreign prompt is not available at panel creation, render a `[User]` placeholder and later edit the same message.
- Telegram-originated runs create `New run` and the live trio, but do not duplicate the user's Telegram message as `[User]`.
- Active runs keep live trio visibility. Completed runs collapse the summary message into one Final Card with Details view-state.
- `Details` pagination edits the same message; it must not create a stream of replacement pages.
- `Tools file` and `Get full log` are explicit on-demand exports. Automatic per-tool document spam is forbidden.
- Document exports should use in-memory multipart upload. Temp files are allowed only as fallback and must be cleaned up.
- Plan Mode waiting input is a separate routeable `[Plan]` prompt-card. Buttons are allowed only for structured choices provided by Codex.
- Telegram-originated Plan Mode starts must pass App Server `collaborationMode.mode = plan`; prompt wording alone is not Plan Mode.
- `/model` and `/effort` are Telegram button menus for collaboration-mode model settings. Persist selections in SQLite, not env-only local config. After a selection, remove the inline choice buttons from the edited message.
- `/settings` includes an App Server menu for `auto`, `stdio`, `unix`, `ws`, and `desktop_bridge`. Persist selections in SQLite, not env-only local config. After a selection, remove the inline choice buttons from the edited message.
- Replies to active turns should steer the active turn. If steering is rejected while the thread still looks active, do not start a parallel turn.
- Every observer-visible message must include the shared identity header: emoji marker, project, thread, `T:`, `R:`, and kind.
- Emoji markers are visual hints only. Persisted message routes and callback tokens are the routing authority.

## Routing Precedence

1. Explicit thread id in a command.
2. Reply-to routed message.
3. Armed one-shot steer/answer state.
4. Bound thread for the current chat/topic.

## Repository Layout

- `cmd/ctr-go/` daemon CLI entrypoint.
- `internal/appserver/` JSON-RPC client transports and snapshot normalization.
- `internal/config/` env-driven config.
- `internal/daemon/` runtime orchestration, observer, panels, callbacks, log exports.
- `internal/model/` shared types.
- `internal/storage/` SQLite schema and repositories.
- `internal/telegram/` Telegram Bot API transport.
- `internal/tgformat/` Markdown-to-Telegram entity renderer.
- `tests/` black-box and live-gated tests.
- `docs/` ADRs, wiki pages, demo docs, metrics, acceptance notes.

## Required Checks

Run before committing code changes:

```powershell
go test ./...
go build -buildvcs=false ./...
```

Run a targeted secret/local scan before committing or publishing:

```powershell
rg -n "BOT_TOKEN|TELEGRAM_BOT_TOKEN|api_hash|api_id|phone|password|secret|\\.session|\\.sqlite|\\.env|C:\\\\Users\\\\<private-user>" .
```

For Telegram UI, routing, callbacks, Plan Mode, Details, or observer behavior, unit tests are not enough when a live contour is available. Verify the changed user-facing path through real Telegram and record the result.

## Change Rules

- Keep changes small and contract-preserving.
- Update ADRs when behavior or architecture changes. Supersede old ADRs instead of silently contradicting them.
- Update README and docs when public behavior changes.
- Add or update tests for routing, observer target, panel lifecycle, callbacks, Markdown/entity rendering, and export behavior.
- Use real Telegram readback for UI changes when possible; Bot API send/edit success alone is not sufficient.
- If live testing is blocked, state the exact blocker and do not mark the change fully validated.
- Do not commit `.env`, Telegram user sessions, Bot API tokens, API hashes, phone numbers, chat ids, proxy credentials, local database/log artifacts, temporary live-e2e files, or private screenshots.

## Demo Rules

- Demo tests must be gated by explicit env vars and build tags.
- Screenshot demo content must be in English and safe for public display.
- Demo screenshots must be reviewed manually before committing.
- Do not fake product screenshots. Use the real Telegram renderer and Bot API.
