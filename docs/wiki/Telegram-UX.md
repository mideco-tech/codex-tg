# Telegram UX

## Run Chronology

Foreign GUI/CLI runs render as:

```text
New run
[User]
[commentary]
[Tool]
[Output]
[Final]
```

`New run`, `[Tool]`, and `[Output]` are deleted best-effort when the final card is rendered. `[User]` remains as a historical marker.

## Final Card

The active commentary card becomes `[Final]`. Details pagination edits the same message instead of sending more messages.

## Exports

- `Tools file`: on-demand file for selected Details tool/output.
- `Get full log`: on-demand archive from Codex session JSONL.

Automatic tool-output document spam is intentionally forbidden.

