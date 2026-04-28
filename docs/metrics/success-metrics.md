# Success Metrics

## Product metrics

- `/status` responds without waiting for full thread indexing.
- background monitoring is active by default when one operator target exists.
- `/observe all` moves the observer target instead of creating an additional feed.
- `/observe off` disables global monitoring.
- Observer messages always include source markers: project and thread.
- `/context` reliably reports the current working tuple or the absence of one.
- Each `(chat, project, thread)` has one actionable summary panel.
- Tool/output messages never carry action buttons.
- Final answers expose `Получить полный лог` on demand.

## Reliability metrics

- `delivery_queue` is the source of truth for observer delivery.
- Delivery retries back off and end in dead-letter state instead of silent loss.
- Restarting the daemon does not delete bindings or observer targets.
- `/repair` recreates sessions and re-marks unresolved approvals as `needs_recheck`.
- Moving the global observer target does not create duplicate active targets.

## Routing metrics

- Route precedence remains:
  1. explicit thread id
  2. reply-to message route
  3. bound thread
- Thread-scoped callback actions keep their `threadId`/`turnId` or `requestId` association.
- Summary-panel callbacks remain scoped to the corresponding `(chat, project, thread)`.

## Validation commands

```powershell
go build -buildvcs=false ./...
go test ./...
go run ./cmd/ctr-go doctor
go run ./cmd/ctr-go status
```
