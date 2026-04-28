# codex-telegram-remote-go

Greenfield Go core for a thread-first Telegram remote UI over local `codex app-server`.

The Python oracle remains at:

`..\codex-telegram-remote`

This Go repository owns the daemon/runtime rewrite:

- non-blocking startup
- separate live and poll app-server sessions
- SQLite-backed thread cache, bindings, observer targets, approvals, delivery queue, and daemon state
- Telegram long polling transport
- durable observer delivery for foreign GUI/CLI threads
- routeable `[Plan]` prompt-cards for Codex Plan Mode / waiting-input states

## Runtime scope

Runtime commands:

- `ctr-go daemon run`
- `ctr-go status`
- `ctr-go doctor`
- `ctr-go repair`

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

Current runtime foundation:

- live app-server notifications for turns started in the current daemon session
- polling fallback through `thread/read` for foreign GUI/CLI activity
- SQLite-backed durable delivery queue

Target observer/UI v2 contract:

- global observer monitoring is default-on when the operator surface can be resolved automatically
- `/observe all` moves the single global observer target to the current chat/topic
- `/observe off` disables global monitoring instead of merely removing an extra feed
- the primary operator affordance is a summary panel keyed by `(chat, project, thread)`
- the summary panel owns actionable buttons such as `Stop` and `Steer`
- tool/output messages are passive stream messages and do not carry buttons
- final answers are delivered separately and expose on-demand log retrieval via `Получить полный лог`
- Codex Plan Mode waiting input is delivered as a separate `[Plan]` prompt-card
- `[Plan]` is reply-first; structured buttons are shown only when Codex provides explicit options
- polling-discovered Plan prompts without `requestId` are answered through `turn/steer`, with `turn/start` fallback when the active turn is gone
- every observer/card message uses a shared visual identity header:
  `emoji [Project] [Thread] [T:thread] [R:run] [Kind]`
- the emoji marker is stable per thread and avoids active collisions while the palette is available; `T:` and `R:` chips remain the unambiguous visual anchor
- foreign GUI/CLI runs start with separate `New run` and `[User]` cards before the live trio; if the prompt is late, `[User]` starts as a placeholder and is edited in-place

## Portable Go on this machine

Portable Go is installed at:

`go`

Typical session setup:

```powershell
$env:PATH = "<go-bin>;$env:PATH"
go version
```

## Environment

Primary env vars:

- `CTR_GO_HOME`
- `CTR_GO_CODEX_BIN`
- `CTR_GO_APP_SERVER_LISTEN`
- `CTR_GO_TELEGRAM_BOT_TOKEN`
- `CTR_GO_ALLOWED_USER_IDS`
- `CTR_GO_ALLOWED_CHAT_IDS`
- `CTR_GO_DEFAULT_CWD`
- `CTR_GO_OBSERVER_POLL_SECONDS`
- `CTR_GO_REQUEST_TIMEOUT_SECONDS`
- `CTR_GO_INDEX_REFRESH_SECONDS`
- `CTR_GO_ATTACH_REFRESH_SECONDS`
- `CTR_GO_DELIVERY_RETRY_SECONDS`
- `CTR_GO_DELIVERY_MAX_ATTEMPTS`

Compatibility fallbacks:

- `CTR_TELEGRAM_BOT_TOKEN`
- `CTR_ALLOWED_USER_IDS`
- `CTR_ALLOWED_CHAT_IDS`

Example:

```powershell
$env:CTR_GO_TELEGRAM_BOT_TOKEN = "<telegram-bot-token>"
$env:CTR_GO_ALLOWED_USER_IDS = "<telegram-user-id>"
$env:CTR_GO_DEFAULT_CWD = "C:\Users\you\Projects\Codex"
go run ./cmd/ctr-go daemon run
```

## Verification

Build and tests:

```powershell
go build -buildvcs=false ./...
go test ./...
```

CLI smoke:

```powershell
go run ./cmd/ctr-go doctor
go run ./cmd/ctr-go status
go run ./cmd/ctr-go repair
```

Live daemon smoke:

```powershell
go run ./cmd/ctr-go daemon run
```

Known operational caveat:

- Telegram long polling will return `409 Conflict` if another bot process is already consuming the same token. Stop the other consumer before validating this Go runtime against Telegram.

## Reference docs

- [Contract matrix](docs/research/contract-matrix.md)
- [ADR-003 Telegram observer/UI v2](docs/adr/ADR-003-telegram-observer-ui-v2.md)
- [ADR-006 Plan prompt mode](docs/adr/ADR-006-plan-prompt-mode.md)
- [ADR-010 Run chronology and user notice](docs/adr/ADR-010-run-chronology-and-user-notice.md)
- [Acceptance](docs/acceptance/vertical-slice.md)
- [Success metrics](docs/metrics/success-metrics.md)
