# ADR-002: Durable Observer Delivery

- Status: accepted
- Decision: observer events are persisted to a delivery queue before Telegram send.

## Rules

- live same-session events and polling fallback events share the same delivery queue
- queue delivery is retried with backoff
- Telegram message routes are written only after successful delivery
- foreign-thread approvals may degrade to waiting-state notifications when no actionable `request_id` exists
- observer delivery now resolves against one global observer target at enqueue/send time
- summary-panel updates, passive tool/output events, final answers, and on-demand full-log replies must remain distinguishable in queue payloads
