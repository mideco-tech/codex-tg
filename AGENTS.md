# AGENTS

## Repository purpose

This repository is the Go greenfield runtime for `codex-telegram-remote`.

The Python oracle is:

`..\codex-telegram-remote`

Use the Python project for behavior parity, command expectations, routing precedence, and acceptance scenarios. Do not treat it as a drop-in storage or architecture template.

## Core decisions

- Backend integration surface is only `codex app-server` over stdio.
- Durable identity is `threadId`, not Telegram chat id.
- Live observer events come from the daemon session; foreign GUI/CLI activity must also be covered by polling `thread/read`.
- Observer delivery must remain durable through SQLite-backed `delivery_queue`.
- Startup must remain non-blocking; do not move full thread sync into synchronous `Start()`.
- Product docs now target Telegram observer/UI v2:
  - one global observer target, default-on
  - `/observe all` moves that target
  - `/observe off` disables background monitoring
  - per-thread summary panel keyed by `(chat, project, thread)`
  - `Stop` and `Steer` live on the summary panel
  - active runs keep trio live visibility for current execution state, tool activity, and output activity
  - completed runs collapse into one Final Card with a Details view
  - Details is view-state of one Telegram message, not a message stream
  - tool/output messages are passive live messages; full tool details are exported only on demand through `Tools file`
  - `Tools file` and `Get full log` are product features, but exports must be generated in memory when the Telegram transport supports it, or as strict temp files with cleanup after send
  - automatic tool `.txt` messages are forbidden; do not reintroduce per-tool document spam
  - deletion of old live tool/output messages after final delivery is best-effort
  - foreign GUI/CLI runs start with separate `New run` and `[User]` messages before the live trio
  - if the user prompt is not known yet, create a `[User]` placeholder and later edit that placeholder into the prompt
  - Telegram-originated runs create `New run` and the live trio, but do not duplicate the user's Telegram message as `[User]`
  - Telegram-originated prompts are not duplicated; mark their `thread_id + turn_id` so observer resync does not create `[User]`
  - Plan Mode waiting input is rendered as a separate `[Plan]` prompt-card with reply-first routing and structured-only buttons
  - parallel thread readability is DM-first: every observer/card message must include the shared emoji + `[Project] [Thread] [T:...] [R:...] [Kind]` identity header
  - emoji markers are visual hints only; persisted message routes and callback tokens remain the routing source of truth

## Runtime contracts

Telegram commands:

- `/start`
- `/help`
- `/threads`
- `/projects`
- `/show`
- `/bind`
- `/reply`
- `/context`
- `/whereami`
- `/observe all|off`
- `/status`
- `/repair`
- `/stop`
- `/approve`
- `/deny`

Routing precedence:

1. explicit thread id
2. reply-to routed message
3. armed one-shot steer/answer state
4. bound thread

Observer targets:

- the target UI contract is a single global observer target, not additive observer feeds
- `/observe all` moves the global target to the current chat/topic
- `/observe off` disables background monitoring
- docs no longer assume observer delivery requires a separate read-only feed chat
- if runtime behavior differs during migration, document the delta in `coordination/HANDOFF.md`

Final Card lifecycle:

- while a run is active, preserve trio live visibility rather than hiding live state in the final card model
- once the final answer exists, render the completed run as one stable Final Card
- the Final Card is the durable completed-run review surface
- `Details` edits that same card message when Telegram allows it
- `Details` must not become a stream of replacement detail messages
- cleanup of old live tool/output messages after finalization is best-effort and must not block final delivery

Details pagination:

- pagination is view-state for the Final Card message
- callback payloads or persisted card metadata must be enough to reconstruct the selected Details page
- page changes should edit the existing card message instead of sending another page message
- tool/output content is opened on demand in Details tool mode
- large or inconvenient tool/output content must remain available through the explicit `Tools file` action
- `Tools file` should avoid persistent filesystem artifacts; prefer in-memory multipart upload, otherwise use a temp file scoped to one send and delete it after success or cleanup sweep

User request notices:

- `[User]` is a separate card before the live panel for GUI/CLI/poll-discovered prompts
- new global-observer panels must create `New run`, then `[User]`, then summary/tool/output messages
- if the prompt is missing at panel creation, render `[User]` as a placeholder and later edit the same message into the real prompt
- never append a late `[User]` below the live trio; old panels without `UserMessageID` are not retroactively repaired
- `[User]` remains as historical orientation after the run collapses into a Final Card
- `[User]` must be routeable to the exact `thread_id`, `turn_id`, and user item id
- do not emit `[User]` for `/reply` or plain Telegram input, even if a later poll sees the same turn
- real App Server user messages commonly store text in `userMessage.content[].text`; tests must cover that shape

Plan prompt lifecycle:

- waiting Plan Mode input is shown as a separate `[Plan]` prompt-card
- the `[Plan]` card is reply-first: replying to the card should route the operator response without requiring a command
- `[Plan]` buttons are structured actions only and must not carry arbitrary free text
- route Plan responses by exact `threadId`, `turnId`, and `requestId` when `requestId` exists
- if polling discovers waiting Plan input without `requestId`, synthesize a deduped thread/turn-scoped prompt-card without inventing a request id
- send the response with `turn/steer` first, then fall back to `turn/start` only when backend state rejects steering
- Hanif is only a product reference for expected UX, not an implementation or storage source

Parallel thread visual identity:

- the main Telegram observer surface remains the operator DM, not Telegram forum topics
- use the shared identity header helper for any new Telegram card or caption
- every observer-visible message must include source identity; `[Output]` must never be source-less
- marker assignment should avoid active-thread collisions while palette slots are available
- if the palette is exhausted, keep `T:` and `R:` chips mandatory and suffix repeated emoji markers with `#2`, `#3`, etc.
- `New run` is a separate status/source card for run start, not a replacement for `[User]`
- `New run` must be created before `[User]` and live trio, and deleted best-effort at finalization

## Repository layout

- `cmd/ctr-go/`
  daemon CLI entrypoint
- `internal/appserver/`
  JSON-RPC stdio client and observer normalization
- `internal/config/`
  env-driven runtime config
- `internal/daemon/`
  runtime orchestration, routing, observer loops, repair
- `internal/model/`
  shared types and helpers
- `internal/storage/`
  SQLite schema and repositories
- `internal/telegram/`
  Bot API client and long polling transport
- `tests/`
  black-box contract tests against exported packages
- `docs/`
  ADRs, research, acceptance, metrics

## Local toolchain

Portable Go on this machine:

`go`

Typical commands:

```powershell
$env:PATH = "<go-bin>;$env:PATH"
go build -buildvcs=false ./...
go test ./...
go run ./cmd/ctr-go status
```

## Editing rules

- Prefer small, contract-preserving changes.
- If you touch observer behavior, keep delivery queue semantics intact.
- If you change Telegram command or observer semantics, update `docs/research/contract-matrix.md`, `README.md`, ADRs, and tests in the same change.
- Observer/UI/routing behavior changes are not complete without an ADR update or superseding ADR, relevant tests, live e2e when available, and an intentional commit.
- If you change route resolution or observer targeting, add or update tests.
- Do not introduce a second backend path through `codex exec resume` or MCP.
- Do not commit `.env`, Telegram user sessions, Bot API tokens, API hashes, phone numbers, chat ids, proxy credentials, local database/log artifacts, or temporary live-e2e files.
- Before committing or preparing a public copy, run a targeted secret/local-identifier scan over the staged diff or destination tree.
- Do not mark observer, routing, or Telegram UX work as done on unit/integration tests alone when the live contour is available on this machine.
- For Telegram/Codex bridge changes, required acceptance is real end-to-end testing against:
  - a real local Codex thread (for example `codex://threads/...`)
  - the real Telegram bot chat from the user side, not only Bot API self-checks
- Bot API send/edit success is not enough for UI changes; verify user-side Telegram readback with the available user session or an equivalent client.
- Before closing such work, verify the exact user-facing scenario that changed, record the result, and only then report completion.
- If live testing is blocked, state the exact blocker explicitly and do not present the change as fully validated.
- UI and callback changes that affect Telegram cards, buttons, pagination, or Details require live Telegram end-to-end validation when the live contour is available.
- Plan UI changes require live Telegram end-to-end validation of the prompt-card, reply-first flow, structured buttons, and steer/start fallback when the live contour is available.
