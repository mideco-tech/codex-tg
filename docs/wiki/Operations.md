# Operations

## Status

```powershell
go run ./cmd/ctr-go status
```

In Telegram:

```text
/status
/context
```

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

