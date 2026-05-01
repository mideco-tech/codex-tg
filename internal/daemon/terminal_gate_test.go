package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/config"
	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestTelegramEmptyInterruptedGateDefersAndKeepsHotPollingMetadata(t *testing.T) {
	t.Parallel()

	service := newTerminalGateTestService(t)
	service.cfg.ObserverPollInterval = 10 * time.Second
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	snapshot := terminalGateTestSnapshot("thread-defer", "turn-defer", "interrupted")
	snapshot.LatestUserMessageText = "sent from Telegram"
	snapshot.LatestUserMessageFP = "user-fp"
	snapshot.DetailItems = []model.DetailItem{{Kind: model.DetailItemUser, Text: "sent from Telegram", FP: "user-fp"}}
	if err := service.markTelegramOriginTurn(ctx, "thread-defer", "turn-defer"); err != nil {
		t.Fatalf("markTelegramOriginTurn failed: %v", err)
	}

	decision, err := service.decideTelegramOriginEmptyInterruptedTerminal(ctx, &snapshot, now)
	if err != nil {
		t.Fatalf("decideTelegramOriginEmptyInterruptedTerminal failed: %v", err)
	}
	if decision.Action != terminalGateDefer {
		t.Fatalf("Action = %q, want %q", decision.Action, terminalGateDefer)
	}
	if !decision.EmptyInterrupted || !decision.HotPoll {
		t.Fatalf("decision flags = empty:%t hot:%t, want true/true", decision.EmptyInterrupted, decision.HotPoll)
	}
	if decision.Grace != 120*time.Second {
		t.Fatalf("Grace = %v, want 120s", decision.Grace)
	}
	if got, want := string(decision.NextPollAfter), now.Add(10*time.Second).Format(time.RFC3339Nano); got != want {
		t.Fatalf("NextPollAfter = %q, want %q", got, want)
	}
	if got, want := decision.ExpiresAt, now.Add(120*time.Second); !got.Equal(want) {
		t.Fatalf("ExpiresAt = %s, want %s", got, want)
	}

	state := loadTerminalGateState(t, service, ctx, terminalGateDeferKey("thread-defer", "turn-defer"))
	if state.EmptyInterruptedSeenCount != 1 {
		t.Fatalf("EmptyInterruptedSeenCount = %d, want 1", state.EmptyInterruptedSeenCount)
	}
	if state.HotPollIntervalMillis != int64((10 * time.Second).Milliseconds()) {
		t.Fatalf("HotPollIntervalMillis = %d, want 10000", state.HotPollIntervalMillis)
	}
	if state.NextPollAfter != decision.NextPollAfter {
		t.Fatalf("state NextPollAfter = %q, want decision value %q", state.NextPollAfter, decision.NextPollAfter)
	}

	compact := model.ThreadSnapshotState{}
	applyTerminalGateHotPolling(&compact, decision)
	if compact.NextPollAfter != decision.NextPollAfter {
		t.Fatalf("compact NextPollAfter = %q, want %q", compact.NextPollAfter, decision.NextPollAfter)
	}
}

func TestTelegramEmptyInterruptedGateRecoversAndClearsDefer(t *testing.T) {
	t.Parallel()

	service := newTerminalGateTestService(t)
	service.cfg.ObserverPollInterval = 5 * time.Second
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	if err := service.markTelegramOriginTurn(ctx, "thread-recover", "turn-recover"); err != nil {
		t.Fatalf("markTelegramOriginTurn failed: %v", err)
	}
	empty := terminalGateTestSnapshot("thread-recover", "turn-recover", "interrupted")
	if decision, err := service.decideTelegramOriginEmptyInterruptedTerminal(ctx, &empty, now); err != nil || decision.Action != terminalGateDefer {
		t.Fatalf("initial decision = %#v err=%v, want defer", decision, err)
	}

	recovered := terminalGateTestSnapshot("thread-recover", "turn-recover", "inProgress")
	recovered.Thread.Status = "active"
	recovered.Thread.ActiveTurnID = "turn-recover"
	decision, err := service.decideTelegramOriginEmptyInterruptedTerminal(ctx, &recovered, now.Add(15*time.Second))
	if err != nil {
		t.Fatalf("decideTelegramOriginEmptyInterruptedTerminal(recovered) failed: %v", err)
	}
	if decision.Action != terminalGateRecover {
		t.Fatalf("Action = %q, want %q", decision.Action, terminalGateRecover)
	}
	if !decision.HotPoll {
		t.Fatal("HotPoll = false, want true for recovered active turn")
	}
	value, err := service.store.GetState(ctx, terminalGateDeferKey("thread-recover", "turn-recover"))
	if err != nil {
		t.Fatalf("GetState(defer key) failed: %v", err)
	}
	if value != "" {
		t.Fatalf("defer state = %q, want cleared", value)
	}
}

func TestTelegramEmptyInterruptedGateExplicitInterruptBypassesDefer(t *testing.T) {
	t.Parallel()

	service := newTerminalGateTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	if err := service.markTelegramOriginTurn(ctx, "thread-explicit", "turn-explicit"); err != nil {
		t.Fatalf("markTelegramOriginTurn failed: %v", err)
	}
	if err := service.markTelegramOriginExplicitInterrupt(ctx, "thread-explicit", "turn-explicit"); err != nil {
		t.Fatalf("markTelegramOriginExplicitInterrupt failed: %v", err)
	}
	snapshot := terminalGateTestSnapshot("thread-explicit", "turn-explicit", "interrupted")

	decision, err := service.decideTelegramOriginEmptyInterruptedTerminal(ctx, &snapshot, now)
	if err != nil {
		t.Fatalf("decideTelegramOriginEmptyInterruptedTerminal failed: %v", err)
	}
	if decision.Action != terminalGateAccept {
		t.Fatalf("Action = %q, want %q", decision.Action, terminalGateAccept)
	}
	if !decision.ExplicitInterrupt || decision.Reason != "explicit_interrupt" {
		t.Fatalf("explicit decision = %#v, want explicit_interrupt accept", decision)
	}
	explicitState, err := service.store.GetState(ctx, terminalGateExplicitInterruptKey("thread-explicit", "turn-explicit"))
	if err != nil {
		t.Fatalf("GetState(explicit key) failed: %v", err)
	}
	if explicitState == "" {
		t.Fatal("explicit interrupt state is empty")
	}
	deferState, err := service.store.GetState(ctx, terminalGateDeferKey("thread-explicit", "turn-explicit"))
	if err != nil {
		t.Fatalf("GetState(defer key) failed: %v", err)
	}
	if deferState != "" {
		t.Fatalf("defer state = %q, want empty", deferState)
	}
}

func TestTelegramEmptyInterruptedGateKeepsExplicitInterruptAfterTerminalLog(t *testing.T) {
	t.Parallel()

	service := newTerminalGateTestService(t)
	ctx := context.Background()
	threadID := "thread-explicit-terminal-log"
	turnID := "turn-explicit-terminal-log"
	if err := service.markTelegramOriginTurn(ctx, threadID, turnID); err != nil {
		t.Fatalf("markTelegramOriginTurn failed: %v", err)
	}
	if err := service.markTelegramOriginExplicitInterrupt(ctx, threadID, turnID); err != nil {
		t.Fatalf("markTelegramOriginExplicitInterrupt failed: %v", err)
	}
	snapshot := terminalGateTestSnapshot(threadID, turnID, "interrupted")

	service.maybeLogTelegramOriginTerminal(ctx, snapshot)

	explicitState, err := service.store.GetState(ctx, terminalGateExplicitInterruptKey(threadID, turnID))
	if err != nil {
		t.Fatalf("GetState(explicit key) failed: %v", err)
	}
	if explicitState == "" {
		t.Fatal("explicit interrupt state was cleared")
	}
	decision, err := service.decideTelegramOriginEmptyInterruptedTerminal(ctx, &snapshot, time.Now().UTC().Add(time.Second))
	if err != nil {
		t.Fatalf("decideTelegramOriginEmptyInterruptedTerminal failed: %v", err)
	}
	if decision.Action != terminalGateAccept || decision.Reason != "explicit_interrupt" {
		t.Fatalf("decision after terminal log = %#v, want explicit accept", decision)
	}
}

func TestTelegramEmptyInterruptedGateMeaningfulInterruptedNotDeferred(t *testing.T) {
	t.Parallel()

	service := newTerminalGateTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	if err := service.markTelegramOriginTurn(ctx, "thread-meaningful", "turn-meaningful"); err != nil {
		t.Fatalf("markTelegramOriginTurn failed: %v", err)
	}
	snapshot := terminalGateTestSnapshot("thread-meaningful", "turn-meaningful", "interrupted")
	snapshot.LatestFinalText = "Stopped after doing useful work."
	snapshot.LatestFinalFP = "final-fp"
	snapshot.DetailItems = []model.DetailItem{{Kind: model.DetailItemFinal, Text: "Stopped after doing useful work.", FP: "final-fp"}}

	decision, err := service.decideTelegramOriginEmptyInterruptedTerminal(ctx, &snapshot, now)
	if err != nil {
		t.Fatalf("decideTelegramOriginEmptyInterruptedTerminal failed: %v", err)
	}
	if decision.Action != terminalGateAccept {
		t.Fatalf("Action = %q, want %q", decision.Action, terminalGateAccept)
	}
	if decision.EmptyInterrupted {
		t.Fatal("EmptyInterrupted = true, want false for meaningful interrupted snapshot")
	}
	value, err := service.store.GetState(ctx, terminalGateDeferKey("thread-meaningful", "turn-meaningful"))
	if err != nil {
		t.Fatalf("GetState(defer key) failed: %v", err)
	}
	if value != "" {
		t.Fatalf("defer state = %q, want empty", value)
	}
}

func TestTelegramPartialInterruptedGateDefersUntilFinalOrGrace(t *testing.T) {
	t.Parallel()

	service := newTerminalGateTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	if err := service.markTelegramOriginTurn(ctx, "thread-partial", "turn-partial"); err != nil {
		t.Fatalf("markTelegramOriginTurn failed: %v", err)
	}
	snapshot := terminalGateTestSnapshot("thread-partial", "turn-partial", "interrupted")
	snapshot.LatestToolID = "tool-partial"
	snapshot.LatestToolKind = "commandExecution"
	snapshot.LatestToolLabel = "sleep 20; printf 'done\\n'"
	snapshot.LatestToolOutput = "done\n"
	snapshot.LatestToolFP = "tool-fp"
	snapshot.DetailItems = []model.DetailItem{
		{Kind: model.DetailItemUser, Text: "run command"},
		{Kind: model.DetailItemTool, Text: "sleep 20; printf 'done\\n'", FP: "tool-fp"},
		{Kind: model.DetailItemOutput, Text: "done\n", FP: "output-fp"},
	}

	decision, err := service.decideTelegramOriginEmptyInterruptedTerminal(ctx, &snapshot, now)
	if err != nil {
		t.Fatalf("decideTelegramOriginEmptyInterruptedTerminal failed: %v", err)
	}
	if decision.Action != terminalGateDefer {
		t.Fatalf("Action = %q, want %q", decision.Action, terminalGateDefer)
	}
	if decision.EmptyInterrupted {
		t.Fatal("EmptyInterrupted = true, want false for partial interrupted snapshot")
	}
	if !decision.DeferrableInterrupted || decision.Reason != "partial_interrupted" {
		t.Fatalf("decision = %#v, want partial_interrupted defer", decision)
	}
	state := loadTerminalGateState(t, service, ctx, terminalGateDeferKey("thread-partial", "turn-partial"))
	if state.LastReason != "partial_interrupted" || state.LastDecision != string(terminalGateDefer) {
		t.Fatalf("defer state = %#v, want partial_interrupted defer", state)
	}
}

func TestTelegramEmptyInterruptedGateGraceExpiryAccepts(t *testing.T) {
	t.Parallel()

	service := newTerminalGateTestService(t)
	service.cfg.ObserverPollInterval = time.Second
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	if err := service.markTelegramOriginTurn(ctx, "thread-expiry", "turn-expiry"); err != nil {
		t.Fatalf("markTelegramOriginTurn failed: %v", err)
	}
	snapshot := terminalGateTestSnapshot("thread-expiry", "turn-expiry", "interrupted")
	if decision, err := service.decideTelegramOriginEmptyInterruptedTerminal(ctx, &snapshot, now); err != nil || decision.Action != terminalGateDefer {
		t.Fatalf("initial decision = %#v err=%v, want defer", decision, err)
	}

	decision, err := service.decideTelegramOriginEmptyInterruptedTerminal(ctx, &snapshot, now.Add(91*time.Second))
	if err != nil {
		t.Fatalf("decideTelegramOriginEmptyInterruptedTerminal(expired) failed: %v", err)
	}
	if decision.Action != terminalGateAccept {
		t.Fatalf("Action = %q, want %q", decision.Action, terminalGateAccept)
	}
	if decision.Reason != "grace_expired" {
		t.Fatalf("Reason = %q, want grace_expired", decision.Reason)
	}
	if decision.Grace != 90*time.Second {
		t.Fatalf("Grace = %v, want 90s default", decision.Grace)
	}
	state := loadTerminalGateState(t, service, ctx, terminalGateDeferKey("thread-expiry", "turn-expiry"))
	if state.LastDecision != string(terminalGateAccept) || state.LastReason != "grace_expired" {
		t.Fatalf("expired state decision/reason = %q/%q, want accept/grace_expired", state.LastDecision, state.LastReason)
	}
	if state.EmptyInterruptedSeenCount != 2 {
		t.Fatalf("EmptyInterruptedSeenCount = %d, want 2", state.EmptyInterruptedSeenCount)
	}
}

func terminalGateTestSnapshot(threadID, turnID, status string) appserver.ThreadReadSnapshot {
	return appserver.ThreadReadSnapshot{
		Thread: model.Thread{
			ID:        threadID,
			Title:     threadID,
			Status:    "idle",
			UpdatedAt: time.Now().UTC().Unix(),
		},
		LatestTurnID:     turnID,
		LatestTurnStatus: status,
	}
}

func newTerminalGateTestService(t *testing.T) *Service {
	t.Helper()

	root := t.TempDir()
	service, err := New(config.Config{
		Paths: config.Paths{
			Home:    root,
			DataDir: filepath.Join(root, "data"),
			LogDir:  filepath.Join(root, "logs"),
			DBPath:  filepath.Join(root, "data", "state.db"),
		},
		AllowedUserIDs: []int64{123456789},
		DefaultCWD:     `C:\Users\you\Projects\Codex`,
	})
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}
	t.Cleanup(func() {
		_ = service.Close()
	})
	return service
}

func loadTerminalGateState(t *testing.T, service *Service, ctx context.Context, key string) terminalGateState {
	t.Helper()
	raw, err := service.store.GetState(ctx, key)
	if err != nil {
		t.Fatalf("GetState(%s) failed: %v", key, err)
	}
	if raw == "" {
		t.Fatalf("GetState(%s) returned empty state", key)
	}
	var state terminalGateState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("Unmarshal(%s) failed: %v; raw=%q", key, err, raw)
	}
	return state
}
