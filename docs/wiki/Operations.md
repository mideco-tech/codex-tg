# Operations

## Status

```powershell
go run ./cmd/ctr-go status
```

In Telegram:

```text
/status
/codex_status
/context
```

`/status` shows daemon/routing state. `/codex_status` shows App Server transport state, probe results, loaded-thread count when available, Desktop Bridge status, and the expected Codex GUI visibility mode.

## App Server Transport

In Telegram:

```text
/settings
/appserver_transport auto
/appserver_transport stdio
/appserver_transport unix
/appserver_transport ws ws://127.0.0.1:<port>
/appserver_transport desktop_bridge
```

The `/settings` App Server menu provides button selection for the same modes. Transport state is stored in SQLite daemon state and takes effect after the app-server sessions are repaired/recreated.

## Repair

```powershell
go run ./cmd/ctr-go repair
```

In Telegram:

```text
/repair
```

## Common Issue

Telegram `409 Conflict` means another process is polling the same bot token. Stop the other consumer before starting `codex-tg`.
