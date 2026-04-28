# ADR-004: Final Card and Details UX

- Status: accepted
- Decision: active runs keep trio live visibility, while completed runs collapse into one Final Card with a Details view.

## Decisions

- An active run remains live-visible through the trio of Telegram surfaces used for current execution state, tool activity, and output activity.
- After the final answer is available, the completed run collapses into one Final Card.
- `Details` is view-state of that one Telegram message, not a stream of new detail messages.
- Details mode may page through final text, tool calls, and output without creating a replacement message stream.
- Tool and output content remains available on demand in Details tool mode.
- Full tool and output content is also available as a `.txt` file when message-sized rendering is not practical.
- Deletion of old live tool/output messages after finalization is best-effort.

## Consequences

- The completed-run operator surface is one stable message with Final and Details states.
- Callback handlers must edit the existing card whenever Telegram permits it instead of emitting a new stream of messages.
- Pagination state belongs to the card view and must be recoverable from callback payloads or persisted card metadata.
- Tool/output live messages must not be treated as durable final history; the final card and on-demand `.txt` export are the durable review surface.
- Cleanup failures for old live tool/output messages must not fail final delivery.
