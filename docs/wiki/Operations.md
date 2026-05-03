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

## Restart Safety

Avoid forced daemon restarts while a Telegram-originated run is active. The daemon owns the local App Server stdio session used by that run; killing the daemon closes that transport and can make the active turn appear as `interrupted`.

Until a safe restart command exists, prefer this order:

1. Check the active run state in Telegram or with `go run ./cmd/ctr-go status`.
2. Wait for the active Telegram-originated turn to finish.
3. Rebuild or reinstall the binary.
4. Restart the daemon.

Future restart work should implement a drain/guard path that refuses or delays restart while Telegram-origin turns are active. More invasive designs, such as a separate App Server broker process or reattaching to a turn after daemon death, require a separate ADR.

## Common Issue

Telegram `409 Conflict` means another process is polling the same bot token. Stop the other consumer before starting `codex-tg`.
