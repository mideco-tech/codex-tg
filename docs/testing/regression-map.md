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
- Plan choice buttons must stay scoped to the same turn as the `[Plan]` card.
- Stale pending `user_input` from an older turn must not add `answer_choice` buttons to a newer `[commentary]` panel.

## Project Thread Creation

ADR: none yet; feature brief is `docs/process/create-thread-from-project-brief.md`.

Primary tests:

- `internal/daemon/service_test.go::TestProjectsCommandShowsProjectButtonsGroupedByCWD`
- `internal/daemon/service_test.go::TestProjectOpenShowsNewThreadMenu`
- `internal/daemon/service_test.go::TestProjectNewThreadArmsThenPlainTextCreatesThread`
- `internal/daemon/service_test.go::TestProjectNewThreadRejectsThreadStartWithoutID`
- `internal/daemon/service_test.go::TestProjectNewThreadTurnStartFailureSavesThread`
- `internal/daemon/service_test.go::TestSummaryPanelDoesNotShowStalePendingUserInputButtons`

Live E2E:

- Open `/projects`, choose a project, press `New thread`, send a prompt, and verify a new thread/run reaches `[Final]`.
- Send a plain reply after creation and verify it routes to the newly bound thread.
- Run a Plan Mode prompt with structured choices and verify choice buttons appear only on the current `[Plan]` card.

Contract notes:

- Project/workspace identity comes from cached thread `cwd`; this flow does not create or edit work directories.
- Telegram must not accept arbitrary local filesystem paths for thread creation.
- The first prompt is required; create-only threads are out of scope for this slice.

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

## Transient Interrupted Gating

ADR: `docs/adr/ADR-012-turn-lifecycle-normalization.md`

Primary tests:

- `internal/daemon/terminal_gate_test.go::TestTelegramEmptyInterruptedGateDefersAndKeepsHotPollingMetadata`
- `internal/daemon/terminal_gate_test.go::TestTelegramEmptyInterruptedGateRecoversAndClearsDefer`
- `internal/daemon/terminal_gate_test.go::TestTelegramEmptyInterruptedGateGraceExpiryAccepts`
- `internal/daemon/terminal_gate_test.go::TestTelegramEmptyInterruptedGateExplicitInterruptBypassesDefer`
- `internal/daemon/terminal_gate_test.go::TestTelegramFinalInterruptedGateDefersUntilRecovered`
- `internal/daemon/terminal_gate_test.go::TestTelegramPartialInterruptedGateDefersUntilFinalOrGrace`
- `internal/daemon/service_test.go::TestPollTrackedDefersTelegramOriginEmptyInterruptedAndKeepsActiveState`
- `internal/daemon/service_test.go::TestPollTrackedDefersTelegramOriginPartialInterruptedAndKeepsActiveState`
- `internal/daemon/service_test.go::TestPollTrackedDefersTelegramOriginFinalInterruptedAndKeepsActiveState`
- `internal/daemon/service_test.go::TestTelegramOriginHotPollCapturesRunningTool`
- `internal/daemon/service_test.go::TestLiveToolNotificationIgnoresOlderTurnAfterNewerCompletion`
- `internal/daemon/service_test.go::TestRefreshThreadForOperationDefersEmptyInterrupted`

Live E2E:

- checked-in public-safe harness: `tests/live_e2e/telegram_readback_e2e.py`
- requires `CODEX_TG_LIVE_E2E=1`, `CODEX_TG_E2E_THREAD_ID`, a local Telethon session, and bot identity from local env
- uses MTProto readback of edited messages and optional daemon log correlation
- exercises sequential commands plus a multi-command `/reply` math run to catch accidental self-interruption

Contract notes:

- Implicit Telegram-origin `interrupted` is ambiguous until it recovers, expires, or follows explicit `/stop`.
- Deferred terminal state must not collapse the live panel into a false Final Card.
- The daemon must keep polling deferred turns hot.
- Telegram-origin turns get a short App Server `thread/read` hot-poll window after start so `[Tool]` can become visible even when live events do not expose the running command.
- If App Server still has not exposed a tool for an active turn, `[Tool]` must show neutral active-run elapsed time instead of a static empty state.
- Late live tool notifications from older turns must not overwrite a newer completed turn or reintroduce stale `[Tool]` / `[Output]` content.

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

- checked-in public-safe harness: `tests/live_e2e/telegram_readback_e2e.py`
- run against a dedicated private test thread from local env, not the working operator thread
- scenarios: sequential `pwd`, `date`, `printf`, dedicated sleep-20 timing, slow command, and multi-command math through `/reply`
- acceptance: scan edited Telegram `[Tool]`, `[Output]`, and `[Final]` messages for literal `"<nil>"`, stale commands from earlier runs, false parallel-turn rejection, and visible non-final `interrupted`

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

Feature brief: `docs/process/v0.2.0-live-appserver-events-brief.md`

Primary tests:

- `internal/daemon/session_tail_overlay_test.go::TestPollTrackedIgnoresStaleSessionTailTool`
- `internal/appserver/normalize_test.go::TestToolSnapshotFromLiveNotificationMapsRunningCommand`
- `internal/appserver/normalize_test.go::TestCompactSnapshotStoresToolTimingOnFirstSeen`
- `internal/appserver/normalize_test.go::TestCompactSnapshotPreservesToolTimingWhenUnchanged`
- `internal/appserver/normalize_test.go::TestCompactSnapshotUpdatesToolLastUpdateWhenFingerprintChanges`
- `internal/appserver/normalize_test.go::TestCompactSnapshotDoesNotPreserveActiveLiveToolWhenThreadReadOmitsTool`
- `internal/appserver/normalize_test.go::TestCompactSnapshotDoesNotPreserveLiveToolAcrossTurns`
- `internal/daemon/service_test.go::TestLiveToolNotificationStoresRunningCommandWithoutRenderingItAsCurrent`
- `internal/daemon/service_test.go::TestPollSnapshotWithoutToolDoesNotPreserveSameTurnRunningToolAsCurrent`
- `internal/daemon/observer_ui_v2_test.go::TestRenderToolPanelShowsLastCompletedToolInsteadOfRunningTool`
- `internal/daemon/observer_ui_v2_test.go::TestRenderToolPanelShowsTelegramOriginCurrentTool`
- `internal/daemon/observer_ui_v2_test.go::TestRenderToolPanelKeepsForeignRunningToolHidden`
- `internal/daemon/observer_ui_v2_test.go::TestRenderSummaryPanelShowsActiveRunElapsedTimeAtBottom`
- `internal/daemon/service_test.go::TestFinalCardShowsRunDuration`

Contract notes:

- App Server `thread/read` snapshots remain the durable source.
- App Server live item notifications may update snapshot/detail history.
- Telegram-origin turns may render current command visibility from live `item/started` and `item/updated` only after matching the marked `thread_id + turn_id`.
- Foreign GUI/CLI runs do not promise authoritative current command visibility.
- Long-running active runs render elapsed runtime in `[commentary]`; completed Final Cards render total `Run duration`.
- `[Tool]` renders the current tool only for eligible Telegram-origin active turns; otherwise it renders the last completed tool, or `No completed tool yet.` when no completed tool is available.
- `[Output]` renders the last completed tool output when available.
- Session JSONL is not a live Telegram UI source.
- Missing App Server tool state renders as neutral absence, not as a guessed command.
- Session JSONL can still be used for explicit full-log export paths.

Slice gate:

- Each v0.2.0 live-event slice must add or update tests first, pass targeted checks, run the relevant live Telegram E2E case, and only then be committed.

## Baseline Commands

Run before commit or publish:

```powershell
go test ./...
go build -buildvcs=false ./...
git diff --check
git grep -nE "BOT_TOKEN|TELEGRAM_BOT_TOKEN|api_hash|api_id|phone|password|secret|\\.session|\\.sqlite|\\.env|C:\\\\Users\\\\<private-user>" -- ':!go.sum' ':!.env' ':!.env.example' ':!.git'
```
