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

`New run` identifies the source of the run. `[User]` carries the request text. Neither card shows run status.

## Final Card

The active commentary card owns run status and becomes `[Final]` when the final answer is available. `[Final]` shows final-answer text/status only; completed commentary and tool/output history stay in Details. Details pagination edits the same message instead of sending more messages.

## Exports

- `Tools file`: on-demand file for selected Details tool/output.
- `Get full log`: on-demand archive from Codex session JSONL.

Automatic tool-output document spam is intentionally forbidden.
