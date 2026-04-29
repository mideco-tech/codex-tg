# ADR-001: Go Greenfield Core

- Status: accepted
- Decision: build a separate Go daemon/runtime instead of rewriting the Python repository in place.
- Rationale:
  - current problems are dominated by lifecycle, subprocess orchestration, readiness, and durable delivery
  - Go is a better fit for long-running service orchestration on Windows than a full Rust rewrite
  - the Python repository remains the oracle for UX and acceptance behavior

## Consequences

- `codex app-server` remains the only backend integration surface; ADR-012 expands the local transport choices beyond the original stdio-only implementation
- the Go daemon owns bot polling, observer polling, delivery queue, and readiness
- compatibility is product-level, not storage-level; Go owns its own schema
- the Python oracle remains the behavior baseline, but Telegram observer/UI v2 may intentionally diverge where the product contract is changed by ADR
