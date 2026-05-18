# codex-tg: local Codex control plane

Local control plane for OpenAI Codex App Server, built in Go, with Telegram as
the first production adapter.

`codex-tg` watches local Codex threads, keeps durable thread identity visible,
routes operator input back to the right turn, and exposes high-signal controls
such as Plan Mode prompts, Stop, Steer, Details, Tools file, and Get full log.
The current adapter is Telegram; the v0.5 direction is a reusable local
`codex-control` layer for future router agents, voice assistants, tray flows,
and other private adapters.

## Why codex-tg?

- Local Codex control plane for OpenAI Codex App Server.
- Control and observe Codex threads through adapters without exposing App Server to the internet.
- Use Telegram today as a high-signal notification, reply, approval, and Details surface.
- Reuse your existing Codex setup: skills, MCP servers, plugins, repo instructions, and local workflows.
- Thread-first routing keeps replies, tools, Plan Mode, Details, and Final cards attached to the right run.
- Built toward long-running local coding-agent orchestration and future router-agent workflows.

Current release: `v0.5.0`.

![codex-tg Telegram Plan Mode demo](docs/assets/telegram-plan-mode-demo.png)

The demo flow is documented in [docs/demo/telegram-plan-mode-demo.md](docs/demo/telegram-plan-mode-demo.md).

## Why It Matters

- Keep local Codex work observable and controllable without exposing Codex App Server to the internet.
- Give future router agents a stable local control surface for Codex threads, turns, approvals, events, and skills.
- Use Telegram as a low-friction fallback and notification surface on unreliable or constrained networks.
- Preserve local-first ownership: Codex sessions, workspaces, SQLite state, and tokens stay on your machine.

## Remote Connections

Official Codex Remote Connections cover the broad mobile remote-control
workflow for Codex. `codex-tg` is not trying to replace that feature. The
project direction is a local control layer and adapter system: Telegram remains
useful for operator notifications, replies, approvals, Details, and logs, while
future adapters can consume the same Codex control core.

## Demo Screenshots

**1. User request and Plan Mode**

![User request and Plan Mode](docs/assets/telegram-plan-mode-demo-1-user-plan.png)

**2. Tool execution and output**

![Tool execution and output](docs/assets/telegram-plan-mode-demo-2-tool-output.png)

**3. Final answer and Details**

![Final answer and Details](docs/assets/telegram-plan-mode-demo-3-final-details.png)

## Features

- Thread-first routing over local Codex App Server.
- Global observer for foreign GUI/CLI runs, with polling fallback through `thread/read`.
- Telegram-origin live current tool rendering from App Server `item/*` events, while foreign GUI/CLI panels stay completed-tool only.
- Stable visual identity per thread: emoji marker plus project/thread/run chips.
- Explicit `New run -> [User] -> [commentary] -> [Tool] -> [Output] -> [Final]` chronology with status on the live commentary/final card.
- Low-noise Telegram notifications: only `New run` (configurable), `[Plan]`, and `[Final]` are audible; live progress and exports are sent silently.
- Plan Mode starts from Telegram via `/plan` or `/reply --plan`; if a thread remains in Plan Mode, the Plan Final Card offers `Turn off Plan` and `/stop` also arms the next normal turn to leave Plan Mode. `[Plan]` prompt-cards keep reply-first routing and structured buttons when Codex provides choices.
- Final Card with Details pagination and on-demand Tools file export.
- On-demand full log archive from Codex session JSONL.
- SQLite-backed durable state for bindings, routes, callbacks, observer target, panels, and delivery metadata.
- macOS service installer with friendly first-run setup, user LaunchAgent management, and menu bar tray control.
- v0.5 architecture direction: adapter-independent Codex Control Plane for router agents, voice adapters, and local private APIs.
- Cross-platform Go daemon foundation for Windows, macOS, and Linux.

## Platform Status

- Windows: actively tested with the local Codex App Server, Telegram Bot API, observer flows, and live E2E demo.
- macOS: `v0.5.0` preserves the `v0.4.0` verified service/runtime path on macOS 26.3.1 arm64 with Go 1.26.2, and adds validated Codex Control Plane architecture, internal control interfaces, capability mapping, normalized event contracts, and notification severity policy.
- Linux: CI runs tests/builds on Ubuntu; full local daemon/runtime validation is still pending.

## Quickstart

Prerequisites:

- OpenAI Codex CLI with `codex app-server`.
- A Telegram bot token from BotFather.
- Your Telegram numeric user id.

On macOS, download the latest `.pkg` from
[GitHub Releases](https://github.com/mideco-tech/codex-tg/releases/latest),
install it, then run:

```powershell
ctr-go service install --start --start-at-login
ctr-go doctor
```

`ctr-go service install` starts a friendly first-run setup wizard when required.
The same values can be passed with flags for scripted installs. It writes a
private local config file at `~/.codex-tg/config.env` by default, creates a
user LaunchAgent, and starts the daemon when `--start` is present.
If your shell uses proxy variables such as `HTTPS_PROXY` or `NO_PROXY`, the
installer preserves them in the private config so the LaunchAgent can reach the
same network without putting secrets or user ids into the plist.

For Linux, Windows, or manual macOS setup, download the latest `ctr-go` archive,
unpack it, then run:

```powershell
ctr-go init
ctr-go doctor
ctr-go daemon run
```

Use `CTR_GO_CONFIG` to point at another config file. Explicit environment
variables still override config file values.

Build from source:

```powershell
git clone https://github.com/mideco-tech/codex-tg.git
cd codex-tg
go run ./cmd/ctr-go init
go run ./cmd/ctr-go doctor
go run ./cmd/ctr-go daemon run
```

Environment-only setup remains supported:

```powershell
$env:CTR_GO_TELEGRAM_BOT_TOKEN = "<telegram-bot-token>"
$env:CTR_GO_ALLOWED_USER_IDS = "<telegram-user-id>"
$env:CTR_GO_DEFAULT_CWD = "C:\Users\you\Projects\Codex"
```

In Telegram:

```text
/start
/observe all
/threads
/context
```

Start or continue a Codex thread from Codex GUI/CLI. `codex-tg` should create a `New run` card, a `[User]` card, live progress cards, and then send a final answer card with Details while cleaning up transient live cards.

Set `CTR_GO_NOTIFY_NEW_RUN=off` to keep `New run` visible but silent. `[Plan]` prompts and `[Final]` cards still use normal Telegram notifications.

## Runtime Commands

```powershell
ctr-go init
ctr-go service install
ctr-go service start
ctr-go service stop
ctr-go service restart
ctr-go service status
ctr-go doctor
ctr-go status
ctr-go repair
ctr-go daemon run
```

Source-build equivalents:

```powershell
go run ./cmd/ctr-go init
go run ./cmd/ctr-go service install
go run ./cmd/ctr-go doctor
go run ./cmd/ctr-go status
go run ./cmd/ctr-go repair
go run ./cmd/ctr-go daemon run
```

Telegram commands:

- `/start`, `/help`
- `/threads`, `/projects`, `/new`, `/newchat`, `/newthread`, `/show`, `/bind`, `/reply`, `/plan`
- `/settings`, `/model`, `/effort`
- `/context`, `/whereami`
- `/observe all`, `/observe off`
- `/status`, `/repair`, `/stop`, `/approve`, `/deny`

`/projects` opens cached project/workspace navigation sorted by the latest
thread activity. Codex UI Chats from `Documents/Codex` are grouped under
`Chats`: the main projects view shows recent Chat previews, `Open Chats` opens
the full paginated Chat list, and choosing a Chat opens and binds its thread.
Use `New thread` in a normal project menu to create a new thread in that
project cwd. Use `/newchat <prompt>` to create a Codex UI Chat under
`Documents/Codex/<date>/<prompt-slug>`. Use `/newthread <prompt>` when you need
a thread without choosing a project or creating a Chat folder; App Server may
still attach the daemon default cwd.

## Configuration

Primary environment variables:

- `CTR_GO_HOME`
- `CTR_GO_CONFIG` (`~/.codex-tg/config.env` by default)
- `CTR_GO_CODEX_BIN`
- `CTR_GO_APP_SERVER_LISTEN`
- `CTR_GO_CONTROL_API_LISTEN` (empty/off by default; experimental local router-agent API, loopback TCP only)
- `CTR_GO_TELEGRAM_BOT_TOKEN`
- `CTR_GO_ALLOWED_USER_IDS`
- `CTR_GO_ALLOWED_CHAT_IDS`
- `CTR_GO_DEFAULT_CWD`
- `CTR_GO_CODEX_CHATS_ROOT` (`~/Documents/Codex` by default)
- `CTR_GO_NOTIFY_NEW_RUN` (`true` by default; set `false`/`off`/`0` to send `New run` silently)
- `CTR_GO_LOG_ENABLED` (`true` by default; set `false`/`off`/`0` to discard daemon stdout logs)
- `CTR_GO_DIAGNOSTIC_LOGS` (`true` by default; set `false`/`off`/`0` to keep normal bot logs but suppress structured `daemon_event` diagnostics)
- `CTR_GO_OBSERVER_POLL_SECONDS`
- `CTR_GO_REQUEST_TIMEOUT_SECONDS`
- `CTR_GO_PROJECTS_PROJECT_PREVIEW_LIMIT` (`7` by default)
- `CTR_GO_PROJECTS_CHAT_PREVIEW_LIMIT` (`3` by default)
- `CTR_GO_CHATS_PAGE_SIZE` (`8` by default)
- `CTR_GO_INDEX_REFRESH_SECONDS`
- `CTR_GO_ATTACH_REFRESH_SECONDS`
- `CTR_GO_DELIVERY_RETRY_SECONDS`
- `CTR_GO_DELIVERY_MAX_ATTEMPTS`

Compatibility fallbacks:

- `CTR_TELEGRAM_BOT_TOKEN`
- `CTR_ALLOWED_USER_IDS`
- `CTR_ALLOWED_CHAT_IDS`

## Verification

```powershell
go test ./...
go build -buildvcs=false ./...
```

Live Telegram readback E2E is documented in
[tests/live_e2e/README.md](tests/live_e2e/README.md). It is intentionally
gated by local env and is not part of `go test ./...`.

Live demo for a screenshot:

```powershell
$env:CTR_DEMO_TELEGRAM_E2E = "1"
$env:CTR_DEMO_TELEGRAM_CHAT_ID = "<telegram-chat-id>"
$env:CTR_GO_TELEGRAM_BOT_TOKEN = "<telegram-bot-token>"
$env:CTR_DEMO_KEEP_MESSAGES = "true"
go test -tags demo_e2e ./tests -run TestTelegramPlanModeScreenshotDemo -count=1 -v
```

See [docs/demo/telegram-plan-mode-demo.md](docs/demo/telegram-plan-mode-demo.md) for the screenshot checklist.

## GitHub Metadata

Suggested repository description:

```text
Local Codex control plane with a Telegram adapter. Observe, approve, steer, and route Codex App Server threads without exposing App Server publicly.
```

Suggested topics:

```text
codex telegram telegram-bot telegram-ui openai-codex codex-cli
codex-app-server codex-control-plane ai-agents coding-agent remote-control developer-tools
local-first go macos windows linux plan-mode agent-observer router-agent
```

## Documentation

- [Architecture](docs/wiki/Architecture.md)
- [Control Plane](docs/wiki/Control-Plane.md)
- [Quickstart](docs/wiki/Quickstart.md)
- [Telegram UX](docs/wiki/Telegram-UX.md)
- [Plan Mode](docs/wiki/Plan-Mode.md)
- [Security](docs/wiki/Security.md)
- [Operations](docs/wiki/Operations.md)
- [Demo](docs/wiki/Demo.md)
- [Changelog](CHANGELOG.md)
- [Contract matrix](docs/research/contract-matrix.md)
- [Validation notes](docs/testing/validation-notes.md)
- [ADRs](docs/adr/)

## License

Apache License 2.0. This keeps the project permissive for the community while also providing an explicit patent grant that large companies usually expect from infrastructure and developer-tooling projects.

## Operational Notes

- Telegram long polling returns `409 Conflict` when another process consumes the same bot token.
- Do not expose Codex App Server on a public interface. `codex-tg` is designed around local/private App Server connectivity.
- Keep bot tokens, Telegram sessions, SQLite databases, logs, and `.env` files out of git.
