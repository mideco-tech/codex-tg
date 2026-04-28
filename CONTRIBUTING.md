# Contributing

Thanks for considering a contribution to `codex-tg`.

## Development Loop

```powershell
go test ./...
go build -buildvcs=false ./...
```

For Telegram UI changes, also validate the changed path against a real Telegram bot when you can. Unit tests are required, but UI behavior is only trustworthy after user-side readback.

## Pull Request Checklist

- The change keeps `codex app-server` stdio as the backend integration surface.
- Thread routing remains thread-first and does not depend on rendered Telegram text.
- Observer/UI behavior changes include tests and an ADR update or superseding ADR.
- `Tools file` and `Get full log` remain explicit on-demand exports.
- No `.env`, tokens, Telegram sessions, chat ids, local databases, logs, binaries, or private screenshots are committed.

## Commit Hygiene

Keep commits focused. A good commit changes one behavior, test surface, or documentation layer at a time.

