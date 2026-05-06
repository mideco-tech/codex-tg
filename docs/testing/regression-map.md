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
- `internal/daemon/service_test.go::TestReplyDefaultFlagStartsDefaultCollaborationMode`
- `internal/daemon/service_test.go::TestDefaultModeCommandStartsDefaultCollaborationMode`
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
- `/default <text>` and `/reply --default <thread> <text>` explicitly start Default Mode through App Server `collaborationMode.mode = default`.
- Plan choice buttons must stay scoped to the same turn as the `[Plan]` card.
- Stale pending `user_input` from an older turn must not add `answer_choice` buttons to a newer `[commentary]` panel.

## Project Thread Creation

ADR: `docs/adr/ADR-014-newchat-chat-folder-contract.md`; feature brief is `docs/process/create-thread-from-project-brief.md`.

Primary tests:

- `internal/daemon/service_test.go::TestProjectsCommandShowsProjectButtonsGroupedByCWD`
- `internal/daemon/service_test.go::TestIsCodexChatsCWDMatchesGenericMacAndWindowsPaths`
- `internal/daemon/service_test.go::TestProjectsCommandShowsChatsSectionAndSortsByRecency`
- `internal/daemon/service_test.go::TestProjectsPaginationUsesPreviewLimitsAndKeepsLatestChats`
- `internal/daemon/service_test.go::TestOpenChatsPaginatesAndChatSelectionBindsThread`
- `internal/daemon/service_test.go::TestProjectsCloseDeletesMenuMessage`
- `internal/daemon/service_test.go::TestProjectOpenShowsNewThreadMenu`
- `internal/daemon/service_test.go::TestProjectNewThreadArmsThenPlainTextCreatesThread`
- `internal/daemon/service_test.go::TestProjectNewThreadRejectsThreadStartWithoutID`
- `internal/daemon/service_test.go::TestProjectNewThreadTurnStartFailureSavesThread`
- `internal/daemon/service_test.go::TestNewChatCommandCreatesCodexUIChatCWDAndBinds`
- `internal/daemon/service_test.go::TestNewChatCWDUsesFallbackSlugAndCollisionSuffix`
- `internal/daemon/service_test.go::TestNewThreadCommandCreatesThreadWithoutCWDAndBinds`
- `internal/daemon/service_test.go::TestNewChatCommandRejectsMissingThreadID`
- `internal/daemon/service_test.go::TestNewChatCommandTurnStartFailureSavesAndBindsThread`
- `internal/daemon/service_test.go::TestNewThreadCommandTurnStartFailureSavesAndBindsThread`
- `internal/config/config_test.go::TestFromEnvReadsCodexChatsRoot`
- `internal/telegram/bot_test.go::TestDefaultCommandsExposeNewChatMenuCommand`
- `internal/daemon/service_test.go::TestSummaryPanelDoesNotShowStalePendingUserInputButtons`
- `tests/config_env_test.go::TestFromEnvProjectChatLimitsClampInvalidValues`

Live E2E:

- Open `/projects`, choose a project, press `New thread`, send a prompt, and verify a new thread/run reaches `[Final]`.
- Open `/projects`, verify normal projects are sorted by recent activity, `Documents/Codex` threads are shown only as latest Chat previews, then open full `Chats` pagination and select a Chat.
- Run `/newchat <prompt>`, verify the new thread reaches `[Final]`, the generated cwd exists under the configured Chats root, `/projects -> Open Chats` shows it, and a plain follow-up routes to the newly bound Chat thread.
- Run `/newthread <prompt>`, verify the new thread reaches `[Final]` without creating a Chat cwd under the configured Chats root.
- Send a plain reply after creation and verify it routes to the newly bound thread.
- Run a Plan Mode prompt with structured choices and verify choice buttons appear only on the current `[Plan]` card.

Contract notes:

- Project/workspace identity comes from cached thread `cwd`; this flow does not create or edit work directories.
- Threads under generic `Documents/Codex` cwd roots or configured `CTR_GO_CODEX_CHATS_ROOT` are Codex UI `Chats`, not normal projects. A Chat selection opens and binds its single thread; Chat lists do not expose project `New thread`.
- Main `/projects` uses project pagination with configurable preview limits and keeps latest Chat previews newest-first. Full Chat pagination lives behind `Open Chats`.
- Project buttons use `N. Project name`; Chat buttons use `Chat N. Thread name`. The menu must not render internal `key:` rows and must show each project row's `last thread:`.
- `/newchat <prompt>` creates a dated Chat cwd from a prompt slug and passes that cwd to App Server `thread/start`.
- `/newthread <prompt>` creates a new App Server thread without a Telegram-selected cwd parameter. It must not create a Chat folder; App Server may still attach the daemon default cwd.
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

## Final Card Details Binding

ADR: `docs/adr/ADR-004-final-card-details-ux.md`

Primary tests:

- `internal/daemon/observer_ui_v2_test.go::TestFinalCardDetailsCallbacksEditSameMessageAndExportToolsFile`
- `internal/daemon/observer_ui_v2_test.go::TestFinalCardDetailsShowsToolOnlyTurnWithoutCommentary`
- `internal/daemon/observer_ui_v2_test.go::TestDetailsCallbacksUsePanelTurnInsteadOfLatestThreadTurn`
- `internal/daemon/observer_ui_v2_test.go::TestDetailsCallbacksStayBoundToOriginalPanelAfterNewerRunCompletes`
- `internal/daemon/observer_ui_v2_test.go::TestDetailsCallbackWithoutPanelIDDoesNotFallbackToCurrentPanel`
- `internal/daemon/observer_ui_v2_test.go::TestDetailsCallbackRejectsMismatchedMessageID`
- `internal/daemon/observer_ui_v2_test.go::TestDetailsToolsFileRejectsMismatchedPanelRoute`

Contract notes:

- `Details`, pagination, `Tool on`, `Tools file`, and `Back` are bound to the completed panel/card that produced the callback.
- A Details callback without a valid `panel_id`, with a mismatched thread/turn, or from another Telegram message is stale and must not edit/export current run data.
- Pressing `Back` on an older completed run must restore that older Final Card in the same message, not duplicate or replace it with the latest run.
- Finalization sends a new Final Card and moves the panel summary message id to it; Details/Back must edit that new card, not the deleted live commentary card.
- Tool-only turns with no commentary and empty output still expose completed command/status in Details, Tool mode, and Tools file under `Tool activity`.

## Telegram Notification Contract

ADR: `docs/adr/ADR-015-telegram-notification-contract.md`

Primary tests:

- `internal/telegram/api_test.go::TestClientSendMessageSilentSetsDisableNotification`
- `internal/telegram/api_test.go::TestClientSendDocumentSilentSetsDisableNotification`
- `internal/telegram/bot_test.go::TestBotDeliverDirectResponseSendsSilentMessage`
- `internal/config/config_test.go::TestMarshalJSONIncludesNotifyNewRun`
- `tests/config_env_test.go::TestFromEnvDefaultsLoggingOn`
- `tests/config_env_test.go::TestFromEnvPrefersGoScopedEnvVars`
- `internal/daemon/observer_ui_v2_test.go::TestSyncThreadPanelCreatesRouteablePlanPromptAndDedupes`
- `internal/daemon/observer_ui_v2_test.go::TestRunNoticeNotificationFlagCanSilenceNewRun`
- `internal/daemon/observer_ui_v2_test.go::TestFinalTransitionDeletesRunNoticeToolAndOutputButKeepsUser`
- `internal/daemon/observer_ui_v2_test.go::TestFinalCardDetailsCallbacksEditSameMessageAndExportToolsFile`

Live E2E:

- Run a Telegram-origin command with `CTR_GO_NOTIFY_NEW_RUN=on`; verify `New run`, live silent cards, new `[Final]`, and Details/Back.
- Repeat with `CTR_GO_NOTIFY_NEW_RUN=off`; verify `New run` remains visible and `[Final]` still arrives.
- Run a Plan Mode structured-choice prompt and verify `[Plan]` is the only question card with answer buttons.

Contract notes:

- All new bot messages are silent except `New run`, `[Plan]`, and `[Final]`.
- `New run` notification is configurable and enabled by default.
- Explicit exports and direct command/menu responses are silent.
- Old live commentary routes may remain in SQLite after deletion, but active Details routing uses the new Final card message id.

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
- `internal/appserver/normalize_test.go::TestSnapshotFromThreadReadKeepsToolOnlyTurnDetailsWithoutCommentary`
- `internal/daemon/service_test.go::TestLiveToolNotificationStoresRunningCommandWithoutRenderingItAsCurrent`
- `internal/daemon/service_test.go::TestPollSnapshotWithoutToolDoesNotPreserveSameTurnRunningToolAsCurrent`
- `internal/daemon/service_test.go::TestPollTrackedDeferredInterruptedDoesNotOverwriteFreshLiveToolSnapshot`
- `internal/daemon/service_test.go::TestPollSnapshotWithOlderCompletedToolPreservesTelegramOriginLiveCurrentTool`
- `internal/daemon/service_test.go::TestRefreshThreadForOperationTerminalCompletedToolReplacesLiveCurrent`
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
- While a Telegram-origin live current tool is active, older completed tool evidence from same-turn `thread/read` may update `[Output]`, but must not make `[Tool]` revert from the current command to the older completed command.
- Empty/interrupted polling snapshots must not overwrite a fresher stored live current tool for the same Telegram-origin turn.
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
