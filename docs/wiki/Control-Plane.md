# Control Plane

`codex-tg` is moving toward a local Codex Control Plane: a reusable layer for
controlling Codex threads, turns, approvals, events, notifications, and logs.
Telegram remains the first adapter, but the architecture should also support a
future router agent, voice assistant, tray workflow, local HTTP bridge, or
mobile UI.

## Layers

```text
Channel adapters
  - Telegram
  - tray
  - future voice
  - future local HTTP/mobile
        |
        v
Control Core
  - thread and turn lifecycle
  - event normalization
  - approvals and user input
  - notification policy
  - adapter routing
        |
        v
Codex connectors
  - App Server
  - optional SDK/MCP orchestration adapters
  - Skills, Hooks, Automations, MCP status
        |
        v
Local Codex environment
```

## Control Core

The control core should expose Codex operations without depending on Telegram
message ids, callback tokens, or chat topics. Telegram-specific routing stays in
the Telegram adapter.

Core responsibilities:

- list, search, read, start, resume, fork, rename, archive, compact, and
  rollback threads;
- start, steer, interrupt, approve, deny, and answer turns;
- subscribe to normalized lifecycle, tool, final, approval, and input events;
- discover skills and ecosystem state;
- classify notifications as `urgent`, `normal`, `silent`, or `digest`.

## Codex App Server Connector

App Server remains the authoritative interactive control surface. It owns
thread history, turn state, approvals, user input requests, live events, and
durable snapshots.

The current implementation starts App Server over stdio. Future work may add
`unix://` or `app-server proxy` support when that improves lifecycle safety, but
it must not introduce a second live source of truth.

## Orchestration Adapters

SDK and MCP integrations may be used for router-agent workflows, handoffs,
traces, or multi-agent experiments. They complement App Server; they do not
replace App Server state for live rendering, approvals, or Details in v0.5.

## Channel Adapters

Telegram remains the first production adapter:

- high-signal notifications;
- replies and Plan input;
- approvals;
- Details, Tools file, and full log exports;
- project and thread navigation.

Future adapters should consume the same control core rather than duplicating
Telegram command behavior.

## Router Agent

A future router agent can sit above the control core. Its job is to interpret
user intent, choose the right thread or project, select skills, start or steer
turns, and decide where to notify the user.

## Experimental Local API

The first router-agent API surface is intentionally conservative:

- disabled by default;
- enabled with `CTR_GO_CONTROL_API_LISTEN`;
- accepts only loopback TCP listeners such as `127.0.0.1:8765`;
- exposes read-only health/status/thread list/thread read endpoints first.

This is a local adapter over the same daemon/control core. It is not a public
network service and must not expose App Server directly.

Voice is expected to be a separate adapter for that router. The first voice
prototype should use a chained pipeline: wake word, transcription, router
decision, Codex action, and spoken summary.
