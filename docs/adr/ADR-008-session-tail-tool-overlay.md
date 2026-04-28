# ADR-008: Session Tail Tool Overlay

- Status: accepted
- Decision: `thread/read` remains the primary observer state source, but the daemon may use a bounded read-only tail of the local Codex session JSONL as an overlay for in-flight GUI/CLI tool calls that are not yet materialized in App Server snapshots.

## Decisions

- The observer does not switch to `codex exec`, MCP, or a filesystem-first model.
- App Server `thread/read` remains authoritative for durable thread, turn, final-answer, plan, and completed tool state.
- The JSONL tail overlay is allowed only for known local threads and only for active tool calls missing from the latest `thread/read` snapshot.
- The daemon reads a bounded tail window, not the whole session file, during polling.
- A `response_item` `function_call` / `custom_tool_call` without a later matching `exec_command_end` or `function_call_output` is treated as the latest active tool.
- For shell calls, the rendered command comes from `arguments.command`, so the Telegram `[Tool]` panel shows the real command such as `Start-Sleep -Seconds 1800`.
- While an overlay active tool exists, the thread is treated as hot-tracked even if App Server currently reports a stale terminal status.

## Consequences

- The observer can show current GUI/CLI commands before App Server exposes them as `commandExecution` items.
- The overlay must never create separate tool files or additional Telegram messages; it only updates the current `[Tool]` / `[Output]` panel state.
- If App Server later catches up with a materialized completed tool, normal snapshot state supersedes the overlay.
- Tests must cover live/poll race cases where `thread/read` contains an old completed tool and JSONL tail contains a newer running call.

## Non-goals

- The overlay is not a full JSONL replay engine.
- The overlay does not replace full-log generation.
- The overlay does not parse arbitrary files outside known Codex session paths.
