# Regression Map

This map is the handoff index for agents changing Telegram routing, observer panels, lifecycle recovery, diagnostics, or Plan Mode.

When behavior changes, update the relevant ADR first, then update or add the tests named here. The tests are part of the architecture: they describe the contract that must survive App Server drift and daemon restarts.

## Plan Mode Routing

ADR: `docs/adr/ADR-006-plan-prompt-mode.md`

Primary tests:

- `internal/daemon/service_test.go::TestPlanCommandStartsPlanCollaborationMode`
- `internal/daemon/service_test.go::TestPlanCommandUsesBoundThreadWhenNoExplicitThread`
- `internal/daemon/service_test.go::TestPlanCommandUnknownHeadUsesBoundThreadAsPromptText`
- `internal/daemon/service_test.go::TestPlanCommandUnknownHeadWithoutImplicitRouteShowsUsage`
- `internal/daemon/service_test.go::TestPlanCommandUUIDLikeHeadStaysExplicit`
- `internal/daemon/service_test.go::TestPlanCommandKnownThreadHeadStaysExplicit`
- `internal/daemon/service_test.go::TestReplyPlanFlagStartsPlanCollaborationMode`
- `internal/daemon/service_test.go::TestPlanModeCommandCanRouteByReply`
- `internal/daemon/service_test.go::TestPlainReplyToSyntheticPlanPromptUsesTurnSteer`
- `internal/daemon/service_test.go::TestPlainReplyToSyntheticPlanPromptFallsBackToTurnStart`
- `internal/daemon/service_test.go::TestPlainReplyToRealPlanPromptUsesServerRequest`
- `internal/daemon/observer_ui_v2_test.go::TestSyncThreadPanelCreatesRouteablePlanPromptAndDedupes`
- `internal/daemon/observer_ui_v2_test.go::TestSyncThreadPanelCreatesServerRequestPlanPromptRoute`

Contract notes:

- `/plan <text>` uses reply, armed state, or bound thread routing.
- `/plan <thread> <text>` is explicit only for known or UUID-like thread ids.
- `/reply --plan <thread> <text>` remains strict.

## Full Thread ID Access

ADR: `docs/adr/ADR-007-parallel-thread-visual-identity.md`

Primary tests:

- `internal/daemon/service_test.go::TestContextCardBoundThreadIncludesFullThreadID`
- `internal/daemon/service_test.go::TestSummaryPanelGetThreadIDButtonSendsCopyableIDs`
- `internal/daemon/service_test.go::TestFinalSummaryPanelHasGetThreadIDButton`
- `internal/daemon/service_test.go::TestFinalCardGetThreadIDButtonSendsCopyableIDs`

Contract notes:

- Header chips like `T:d663` and `R:d9bc` are visual hints only.
- Operators must be able to retrieve copyable full ids from Telegram without SQLite/log access.

## Turn Lifecycle And Stale Active Recovery

ADR: `docs/adr/ADR-012-turn-lifecycle-normalization.md`

Primary tests:

- `internal/appserver/normalize_test.go::TestSnapshotFromThreadReadTreatsFinalAnswerAsCompletedWhenStatusIsStale`
- `internal/daemon/service_test.go::TestStaleActiveThreadWithFinalAnswerStartsNewTurn`
- `internal/daemon/service_test.go::TestNoActiveTurnSteerFailureFallsBackToTurnStart`
- `internal/daemon/service_test.go::TestReplyToActiveThreadDoesNotFallbackToTurnStartWhenSteerFails`
- `internal/daemon/service_test.go::TestReplyToActiveThreadSteersActiveTurn`
- `internal/daemon/observer_ui_v2_test.go::TestGlobalObserverDoesNotRecreateTelegramOriginPanelOnEditFailure`

Contract notes:

- A final answer is terminal evidence unless the turn is waiting for approval or user input.
- `no active turn to steer` means stale active state and may fall back to a new turn after re-read.
- Active or not-steerable failures still block fallback `turn/start`.
- A Telegram-origin panel must not be duplicated by global observer sync for the same marked turn.

## App Server Session Lifecycle

ADR: `docs/adr/ADR-012-turn-lifecycle-normalization.md`

Primary tests:

- `internal/daemon/service_test.go::TestEnsureLiveSessionSerializedAgainstReconcile`
- `internal/daemon/service_test.go::TestRepairInvalidatesOldLiveLoop`
- `internal/daemon/service_test.go::TestControlLoopProcessesRepairBeforeReconcile`
- `internal/appserver/client_test.go::TestClientStartConcurrentCallsShareInitializedSession`
- `internal/appserver/client_test.go::TestClientStartFailureLeavesClientRetryable`

Contract notes:

- One live App Server session has one live event loop per generation.
- Stale old live-loop closes must not clear newer session state.
- Repair is serialized with reconcile/startup and is processed before replacement reconcile.

## Transient Empty Interrupted Gating

ADR: `docs/adr/ADR-012-turn-lifecycle-normalization.md`

Primary tests:

- `internal/daemon/terminal_gate_test.go::TestTelegramEmptyInterruptedGateDefersAndKeepsHotPollingMetadata`
- `internal/daemon/terminal_gate_test.go::TestTelegramEmptyInterruptedGateRecoversAndClearsDefer`
- `internal/daemon/terminal_gate_test.go::TestTelegramEmptyInterruptedGateGraceExpiryAccepts`
- `internal/daemon/terminal_gate_test.go::TestTelegramEmptyInterruptedGateExplicitInterruptBypassesDefer`
- `internal/daemon/terminal_gate_test.go::TestTelegramEmptyInterruptedGateMeaningfulInterruptedNotDeferred`
- `internal/daemon/service_test.go::TestPollTrackedDefersTelegramOriginEmptyInterruptedAndKeepsActiveState`
- `internal/daemon/service_test.go::TestRefreshThreadForOperationDefersEmptyInterrupted`

Live E2E:

- local ignored runner: `~/.codex-tg/e2e/turn_lifecycle_e2e.py`
- requires `CODEX_TG_E2E_THREAD_ID`
- uses MTProto readback, edited messages, Details roundtrip, daemon log correlation, and read-only SQLite correlation

Contract notes:

- Empty Telegram-origin `interrupted` is ambiguous until it recovers, expires, or follows explicit `/stop`.
- Deferred empty terminal state must not collapse the live panel into a false Final Card.
- The daemon must keep polling deferred turns hot.

## Nil-Safe Telegram Rendering

ADR: `docs/adr/ADR-012-turn-lifecycle-normalization.md`

Primary tests:

- `internal/daemon/log_archive_test.go::TestValueFromMapSkipsNilLikeValues`
- `internal/daemon/log_archive_test.go::TestRenderCommandSkipsNilLikeValues`
- `internal/daemon/log_archive_test.go::TestRenderEventMsgWithoutCommandDoesNotPrintNil`
- `internal/daemon/session_tail_overlay_test.go::TestPollTrackedIgnoresStaleSessionTailTool`
- `internal/daemon/observer_ui_v2_test.go::TestSummaryPanelRemovesNilLiteralBeforeRendering`
- `internal/appserver/client_test.go::TestRPCStringSkipsNilLikeValues`
- `internal/appserver/normalize_test.go::TestStringValueTreatsNilLiteralAsMissing`

Live E2E:

- local ignored runner: `~/.codex-tg/e2e/nil_guard_e2e.py`
- run against a dedicated private test thread, not the working operator thread
- scenarios: fast text, fast command, and a command that stays active for about a minute
- acceptance: scan edited Telegram messages, Details views, and Final Cards for literal `"<nil>"` and stale commands from earlier runs

Contract notes:

- Missing App Server fields and literal `"<nil>"` are nil-like values, not display text.
- Telegram rendering must clean nil-like values before Markdown/entity conversion.
- Diagnostics for `telegram_render_contains_nil` are bounded and hash-only.

## Diagnostics And Sanitization

ADR: `docs/adr/ADR-012-turn-lifecycle-normalization.md`

Primary tests:

- `internal/daemon/service_test.go::TestTelegramTurnLifecycleLogsSuccessfulStart`
- `internal/daemon/service_test.go::TestTelegramTurnLifecycleLogsThreadResumeFailure`
- `internal/daemon/service_test.go::TestTelegramTurnLifecycleLogsTurnStartFailure`
- `internal/daemon/service_test.go::TestTelegramTurnLifecycleLogsRefreshFailuresAroundStart`
- `internal/daemon/service_test.go::TestDiagnosticLogsAreRateLimited`
- `internal/daemon/service_test.go::TestDiagnosticLoggerCanBeDisabled`
- `internal/daemon/service_test.go::TestObserverSyncResultLogsAreDebounced`
- `internal/daemon/service_test.go::TestGenericThreadReadDiagnosticsAreDebounced`
- `internal/daemon/service_test.go::TestThreadReadSkippedLogsAreDebounced`
- `internal/telegram/bot_test.go::TestSanitizeTelegramLogErrorRedactsBotTokenURL`
- `tests/config_env_test.go::TestFromEnvDefaultsLoggingOn`
- `tests/config_env_test.go::TestFromEnvInvalidLoggingFlagsFallBackToEnabled`
- `cmd/ctr-go/main_test.go::TestDiagnosticLoggerHonorsFlags`

Contract notes:

- Logs may include ids, route source, operation names, durations, item counts, and sanitized stderr tails.
- Logs must not include full prompt bodies, tokens, session files, SQLite paths, `.env` paths, or unbounded output.
- Diagnostic logging is rate-limited to avoid filesystem floods during app-server loops.
- `CTR_GO_LOG_ENABLED=off` discards daemon stdout logs; `CTR_GO_DIAGNOSTIC_LOGS=off` keeps normal bot logs but suppresses structured lifecycle diagnostics.

## Session Tail Overlay Retirement

ADR: `docs/adr/ADR-013-retire-session-tail-tool-overlay.md`

Primary tests:

- `internal/daemon/session_tail_overlay_test.go::TestPollTrackedIgnoresStaleSessionTailTool`
- `internal/appserver/normalize_test.go::TestToolSnapshotFromLiveNotificationMapsRunningCommand`
- `internal/daemon/service_test.go::TestLiveToolNotificationUpdatesRunningCommandBeforeThreadReadCompletion`

Contract notes:

- App Server `thread/read` snapshots remain the durable source.
- App Server live item notifications may update the same current tool snapshot for in-progress visibility.
- Session JSONL is not a live Telegram UI source.
- Missing App Server tool state renders as neutral absence, not as a guessed command.
- Session JSONL can still be used for explicit full-log export paths.

## Baseline Commands

Run before commit or publish:

```powershell
go test ./...
go build -buildvcs=false ./...
git diff --check
git grep -nE "BOT_TOKEN|TELEGRAM_BOT_TOKEN|api_hash|api_id|phone|password|secret|\\.session|\\.sqlite|\\.env|C:\\\\Users\\\\<private-user>" -- ':!go.sum' ':!.env' ':!.env.example' ':!.git'
```
