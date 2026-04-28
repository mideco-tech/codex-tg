# Vertical Slice Acceptance

## Scope

This Go runtime is considered ready for operator validation when the following path works end-to-end:

1. `ctr-go daemon run` starts without blocking on full thread sync.
2. `/status` answers immediately and shows readiness plus app-server connectivity state.
3. Global observer monitoring is on by default when the runtime can resolve one operator DM.
4. `/observe all` moves the global observer target to the current Telegram chat/topic.
5. A foreign thread updated outside Telegram, for example from `codex://threads/<threadId>` in Codex GUI, produces:
   - a stable summary panel for that `(chat, project, thread)`
   - passive tool/output messages without buttons
   - a final answer message with `Получить полный лог`
6. `/context` shows the current working tuple or explains that no active thread is selected.
7. `/observe off` disables global background monitoring.
8. `/repair` recreates sessions and leaves the daemon in a usable state.

## Operator checklist

- Export `CTR_GO_TELEGRAM_BOT_TOKEN` or `CTR_TELEGRAM_BOT_TOKEN`.
- Export `CTR_GO_ALLOWED_USER_IDS` or `CTR_ALLOWED_USER_IDS`.
- Ensure no other Telegram consumer is polling the same bot token.
- Start the daemon.
- Run `/status`.
- Confirm that default global observer delivery is visible in the operator DM.
- Run `/observe all` from the desired target chat/topic.
- Perform work in a foreign GUI thread.
- Confirm:
  - the summary panel appears once per `(chat, project, thread)`
  - `Stop` and `Steer` are available on the summary panel
  - tool/output messages carry no buttons
  - the final answer exposes `Получить полный лог`
- Run `/observe off` and confirm new background events stop arriving.

## Acceptance notes

- `409 Conflict` from Telegram means another consumer is using the same token; this is an operational conflict, not an app-server failure.
- Polling fallback is the deciding signal for foreign-thread monitoring. Local same-session notifications alone are not sufficient to pass acceptance.
- During migration, code may still expose the older additive observer-feed model. Acceptance for v2 should judge the target semantics above, not the older feed-only wording.
