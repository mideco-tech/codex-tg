package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/config"
	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestResolveRoutePrecedenceExplicitThenReplyThenBinding(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()

	if err := service.store.SetBinding(ctx, 123456789, 0, "binding-thread", model.BindingModeBound); err != nil {
		t.Fatalf("SetBinding failed: %v", err)
	}
	if err := service.store.PutMessageRoute(ctx, model.MessageRoute{
		ChatID:    123456789,
		TopicID:   0,
		MessageID: 99,
		ThreadID:  "reply-thread",
		TurnID:    "reply-turn",
		CreatedAt: model.NowString(),
	}); err != nil {
		t.Fatalf("PutMessageRoute failed: %v", err)
	}

	explicit, err := service.resolveRoute(ctx, 123456789, 0, "explicit-thread", 99)
	if err != nil {
		t.Fatalf("resolveRoute(explicit) failed: %v", err)
	}
	if explicit.ThreadID != "explicit-thread" || explicit.Source != model.RouteSourceExplicit {
		t.Fatalf("explicit route = %#v, want explicit-thread / explicit", explicit)
	}

	reply, err := service.resolveRoute(ctx, 123456789, 0, "", 99)
	if err != nil {
		t.Fatalf("resolveRoute(reply) failed: %v", err)
	}
	if reply.ThreadID != "reply-thread" || reply.Source != model.RouteSourceReply {
		t.Fatalf("reply route = %#v, want reply-thread / reply", reply)
	}
	if reply.TurnID != "reply-turn" || reply.RequestID != "" {
		t.Fatalf("reply route turn/request = %#v, want reply-turn without request", reply)
	}

	binding, err := service.resolveRoute(ctx, 123456789, 0, "", 0)
	if err != nil {
		t.Fatalf("resolveRoute(binding) failed: %v", err)
	}
	if binding.ThreadID != "binding-thread" || binding.Source != model.RouteSourceBinding {
		t.Fatalf("binding route = %#v, want binding-thread / binding", binding)
	}
}

func TestResolveRouteExtractsPlanRequestIDOnlyFromPlanRequestEvent(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()

	if err := service.store.PutMessageRoute(ctx, model.MessageRoute{
		ChatID:    123456789,
		TopicID:   0,
		MessageID: 100,
		ThreadID:  "plan-thread",
		TurnID:    "plan-turn",
		EventID:   "plan_request:request-plan-1",
		CreatedAt: model.NowString(),
	}); err != nil {
		t.Fatalf("PutMessageRoute(plan request) failed: %v", err)
	}
	if err := service.store.PutMessageRoute(ctx, model.MessageRoute{
		ChatID:    123456789,
		TopicID:   0,
		MessageID: 101,
		ThreadID:  "synthetic-thread",
		TurnID:    "synthetic-turn",
		EventID:   "synthetic-plan-fp",
		CreatedAt: model.NowString(),
	}); err != nil {
		t.Fatalf("PutMessageRoute(synthetic) failed: %v", err)
	}

	real, err := service.resolveRoute(ctx, 123456789, 0, "", 100)
	if err != nil {
		t.Fatalf("resolveRoute(real plan request) failed: %v", err)
	}
	if real.ThreadID != "plan-thread" || real.TurnID != "plan-turn" || real.RequestID != "request-plan-1" {
		t.Fatalf("real plan route = %#v, want thread/turn/request", real)
	}

	synthetic, err := service.resolveRoute(ctx, 123456789, 0, "", 101)
	if err != nil {
		t.Fatalf("resolveRoute(synthetic plan) failed: %v", err)
	}
	if synthetic.ThreadID != "synthetic-thread" || synthetic.TurnID != "synthetic-turn" || synthetic.RequestID != "" {
		t.Fatalf("synthetic plan route = %#v, want thread/turn without request", synthetic)
	}
}

func TestCurrentBackgroundTargetDefaultsMovesAndDisables(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()

	target, err := service.currentBackgroundTarget(ctx)
	if err != nil {
		t.Fatalf("currentBackgroundTarget(default) failed: %v", err)
	}
	if target == nil || target.ChatID != 123456789 || target.TopicID != 0 || !target.Enabled {
		t.Fatalf("default background target = %#v, want enabled DM target for allowed user", target)
	}

	if err := service.store.SetGlobalObserverTarget(ctx, -1001234567890, 7, true); err != nil {
		t.Fatalf("SetGlobalObserverTarget(enable moved target) failed: %v", err)
	}
	target, err = service.currentBackgroundTarget(ctx)
	if err != nil {
		t.Fatalf("currentBackgroundTarget(moved) failed: %v", err)
	}
	if target == nil || target.ChatID != -1001234567890 || target.TopicID != 7 || !target.Enabled {
		t.Fatalf("moved global target = %#v, want -1001234567890:7 enabled", target)
	}

	if err := service.store.SetGlobalObserverTarget(ctx, -1001234567890, 7, false); err != nil {
		t.Fatalf("SetGlobalObserverTarget(disable) failed: %v", err)
	}
	target, err = service.currentBackgroundTarget(ctx)
	if err != nil {
		t.Fatalf("currentBackgroundTarget(disabled) failed: %v", err)
	}
	if target != nil {
		t.Fatalf("disabled global target = %#v, want nil", target)
	}
}

func TestObserveCommandsMoveAndDisableGlobalTarget(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()

	response, err := service.handleCommand(ctx, 42, 9, "/observe all", 0)
	if err != nil {
		t.Fatalf("handleCommand(/observe all) failed: %v", err)
	}
	if response == nil {
		t.Fatal("handleCommand(/observe all) returned nil response")
	}

	target, configured, err := service.store.GetGlobalObserverTarget(ctx)
	if err != nil {
		t.Fatalf("GetGlobalObserverTarget(after /observe all) failed: %v", err)
	}
	if !configured || target == nil {
		t.Fatalf("global target after /observe all = %#v configured=%t, want configured target", target, configured)
	}
	if target.ChatID != 42 || target.TopicID != 9 {
		t.Fatalf("global target after /observe all = %#v, want 42:9", target)
	}
	sinceUnix, ok, err := service.store.GetGlobalObserverSinceUnix(ctx)
	if err != nil {
		t.Fatalf("GetGlobalObserverSinceUnix(after /observe all) failed: %v", err)
	}
	if !ok || sinceUnix <= 0 {
		t.Fatalf("GetGlobalObserverSinceUnix(after /observe all) = %d ok=%t, want positive value", sinceUnix, ok)
	}

	response, err = service.handleCommand(ctx, 42, 9, "/observe off", 0)
	if err != nil {
		t.Fatalf("handleCommand(/observe off) failed: %v", err)
	}
	if response == nil {
		t.Fatal("handleCommand(/observe off) returned nil response")
	}

	target, configured, err = service.store.GetGlobalObserverTarget(ctx)
	if err != nil {
		t.Fatalf("GetGlobalObserverTarget(after /observe off) failed: %v", err)
	}
	if !configured {
		t.Fatal("GetGlobalObserverTarget(after /observe off) should remain configured")
	}
	if target != nil {
		t.Fatalf("global target after /observe off = %#v, want nil", target)
	}
}

func TestBindingAndGlobalObserverCanCoexistAtServiceLevel(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()

	if err := service.store.SetGlobalObserverTarget(ctx, 123456789, 0, true); err != nil {
		t.Fatalf("SetGlobalObserverTarget failed: %v", err)
	}
	if err := service.store.SetBinding(ctx, 123456789, 0, "thread-1", model.BindingModeBound); err != nil {
		t.Fatalf("SetBinding failed: %v", err)
	}

	target, configured, err := service.store.GetGlobalObserverTarget(ctx)
	if err != nil {
		t.Fatalf("GetGlobalObserverTarget failed: %v", err)
	}
	if !configured || target == nil || target.ChatID != 123456789 {
		t.Fatalf("global target = %#v configured=%t, want enabled target for bound chat", target, configured)
	}

	binding, err := service.store.GetBinding(ctx, 123456789, 0)
	if err != nil {
		t.Fatalf("GetBinding failed: %v", err)
	}
	if binding == nil || binding.ThreadID != "thread-1" {
		t.Fatalf("binding = %#v, want thread-1", binding)
	}
}

func TestResolveArmedSteerReturnsActiveStateAndExpires(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()

	if err := service.armSteer(ctx, 123456789, 0, "steer-thread", "turn-9", 77); err != nil {
		t.Fatalf("armSteer failed: %v", err)
	}
	state, err := service.resolveArmedSteer(ctx, 123456789, 0)
	if err != nil {
		t.Fatalf("resolveArmedSteer(active) failed: %v", err)
	}
	if state == nil || state.ThreadID != "steer-thread" || state.TurnID != "turn-9" || state.PanelID != 77 {
		t.Fatalf("active steer state = %#v, want steer-thread/turn-9/panel 77", state)
	}

	if err := service.store.ArmSteerState(ctx, model.SteerState{
		ChatKey:   model.ChatKey(123456789, 0),
		ChatID:    123456789,
		TopicID:   0,
		ThreadID:  "expired-thread",
		TurnID:    "turn-old",
		PanelID:   88,
		ExpiresAt: model.TimeString(time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)),
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	}); err != nil {
		t.Fatalf("ArmSteerState(expired) failed: %v", err)
	}

	state, err = service.resolveArmedSteer(ctx, 123456789, 0)
	if err != nil {
		t.Fatalf("resolveArmedSteer(expired) failed: %v", err)
	}
	if state != nil {
		t.Fatalf("expired steer state = %#v, want nil", state)
	}
	loaded, err := service.store.GetSteerState(ctx, 123456789, 0)
	if err != nil {
		t.Fatalf("GetSteerState(after expired resolve) failed: %v", err)
	}
	if loaded != nil {
		t.Fatalf("stored steer state after expired resolve = %#v, want nil", loaded)
	}
}

func TestTrackedThreadsSkipsIdleRecentHistoryWithoutBindingsOrPanels(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	now := time.Now().UTC().Unix()

	idle := model.Thread{
		ID:          "idle-thread",
		Title:       "Idle history",
		ProjectName: "Codex",
		UpdatedAt:   now - 600,
		Status:      "idle",
	}
	active := model.Thread{
		ID:           "active-thread",
		Title:        "Active now",
		ProjectName:  "Codex",
		UpdatedAt:    now,
		Status:       "idle",
		ActiveTurnID: "turn-1",
	}
	if err := service.store.UpsertThread(ctx, idle); err != nil {
		t.Fatalf("UpsertThread(idle) failed: %v", err)
	}
	if err := service.store.UpsertThread(ctx, active); err != nil {
		t.Fatalf("UpsertThread(active) failed: %v", err)
	}

	tracked := service.trackedThreads(ctx, 10)
	ids := map[string]bool{}
	for _, thread := range tracked {
		ids[thread.ID] = true
	}

	if ids[idle.ID] {
		t.Fatalf("tracked threads unexpectedly include stale idle history: %#v", tracked)
	}
	if !ids[active.ID] {
		t.Fatalf("tracked threads do not include active thread: %#v", tracked)
	}
}

func TestTrackedThreadsIncludesRecentlyChangedTerminalThreadForGlobalObserver(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	if err := service.store.SetGlobalObserverTarget(ctx, 123456789, 0, true); err != nil {
		t.Fatalf("SetGlobalObserverTarget failed: %v", err)
	}

	thread := model.Thread{
		ID:          "recent-terminal",
		Title:       "Recent terminal",
		ProjectName: "Codex",
		UpdatedAt:   time.Now().UTC().Unix(),
		Status:      "completed",
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	oldSnapshot := model.ThreadSnapshotState{
		ThreadUpdatedAt:      thread.UpdatedAt - 120,
		LastSeenThreadStatus: "completed",
		LastSeenTurnID:       "turn-old",
		LastSeenTurnStatus:   "completed",
	}
	if err := service.store.UpsertSnapshot(ctx, thread.ID, oldSnapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	tracked := service.trackedThreads(ctx, 10)
	found := false
	for _, candidate := range tracked {
		if candidate.ID == thread.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("tracked threads do not include recent terminal change: %#v", tracked)
	}
}

func TestTrackedThreadsSkipsRecentTerminalChangeThatPredatesObserveEnable(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	now := time.Now().UTC().Unix()
	if err := service.store.UpsertThread(ctx, model.Thread{
		ID:          "recent-before-enable",
		Title:       "Recent but old for observer",
		ProjectName: "Codex",
		UpdatedAt:   now - 30,
		Status:      "completed",
	}); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.SetGlobalObserverTarget(ctx, 123456789, 0, true); err != nil {
		t.Fatalf("SetGlobalObserverTarget failed: %v", err)
	}

	tracked := service.trackedThreads(ctx, 10)
	for _, thread := range tracked {
		if thread.ID == "recent-before-enable" {
			t.Fatalf("tracked threads unexpectedly include completion from before /observe all: %#v", tracked)
		}
	}
}

func TestCurrentPanelThreadIDsSkipTerminalGlobalObserverPanels(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()

	threadA := model.Thread{ID: "thread-a", Title: "A", ProjectName: "Codex", UpdatedAt: time.Now().UTC().Unix(), Status: "idle"}
	threadB := model.Thread{ID: "thread-b", Title: "B", ProjectName: "Codex", UpdatedAt: time.Now().UTC().Unix(), Status: "idle"}
	if err := service.store.UpsertThread(ctx, threadA); err != nil {
		t.Fatalf("UpsertThread(threadA) failed: %v", err)
	}
	if err := service.store.UpsertThread(ctx, threadB); err != nil {
		t.Fatalf("UpsertThread(threadB) failed: %v", err)
	}

	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		TopicID:          0,
		ProjectName:      "Codex",
		ThreadID:         threadA.ID,
		SourceMode:       "global_observer",
		SummaryMessageID: 1,
		ToolMessageID:    2,
		OutputMessageID:  3,
		CurrentTurnID:    "turn-a",
		Status:           "completed",
		ArchiveEnabled:   true,
	}); err != nil {
		t.Fatalf("CreateThreadPanel(global_observer terminal) failed: %v", err)
	}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		TopicID:          0,
		ProjectName:      "Codex",
		ThreadID:         threadB.ID,
		SourceMode:       "explicit",
		SummaryMessageID: 11,
		ToolMessageID:    12,
		OutputMessageID:  13,
		CurrentTurnID:    "turn-b",
		Status:           "completed",
		ArchiveEnabled:   true,
	}); err != nil {
		t.Fatalf("CreateThreadPanel(explicit terminal) failed: %v", err)
	}

	ids := service.currentPanelThreadIDs(ctx)
	foundA := false
	foundB := false
	for _, id := range ids {
		if id == threadA.ID {
			foundA = true
		}
		if id == threadB.ID {
			foundB = true
		}
	}

	if foundA {
		t.Fatalf("currentPanelThreadIDs unexpectedly include terminal global_observer panel: %#v", ids)
	}
	if foundB {
		t.Fatalf("currentPanelThreadIDs unexpectedly include terminal explicit panel: %#v", ids)
	}
}

func TestCurrentPanelThreadIDsSkipTerminalExplicitPanels(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()

	thread := model.Thread{ID: "thread-explicit-terminal", Title: "Explicit", ProjectName: "Codex", UpdatedAt: time.Now().UTC().Unix(), Status: "idle"}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}

	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		TopicID:          0,
		ProjectName:      "Codex",
		ThreadID:         thread.ID,
		SourceMode:       "explicit",
		SummaryMessageID: 1,
		ToolMessageID:    2,
		OutputMessageID:  3,
		CurrentTurnID:    "turn-explicit",
		Status:           "completed",
		ArchiveEnabled:   true,
	}); err != nil {
		t.Fatalf("CreateThreadPanel(explicit terminal) failed: %v", err)
	}

	ids := service.currentPanelThreadIDs(ctx)
	for _, id := range ids {
		if id == thread.ID {
			t.Fatalf("currentPanelThreadIDs unexpectedly include terminal explicit panel: %#v", ids)
		}
	}
}

func TestThreadNeedsLiveSyncSkipsTerminalGlobalObserverPanels(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()

	thread := model.Thread{ID: "thread-live", Title: "Live", ProjectName: "Codex", UpdatedAt: time.Now().UTC().Unix(), Status: "idle"}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		TopicID:          0,
		ProjectName:      "Codex",
		ThreadID:         thread.ID,
		SourceMode:       "global_observer",
		SummaryMessageID: 1,
		ToolMessageID:    2,
		OutputMessageID:  3,
		CurrentTurnID:    "turn-1",
		Status:           "completed",
		ArchiveEnabled:   true,
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}

	if service.threadNeedsLiveSync(ctx, thread.ID) {
		t.Fatal("threadNeedsLiveSync returned true for terminal global_observer panel")
	}

	if err := service.store.SetBinding(ctx, 123456789, 0, thread.ID, model.BindingModeBound); err != nil {
		t.Fatalf("SetBinding failed: %v", err)
	}
	if !service.threadNeedsLiveSync(ctx, thread.ID) {
		t.Fatal("threadNeedsLiveSync returned false for bound thread")
	}
}

func TestSnapshotHasPassiveChangeIgnoresIdenticalTerminalReplay(t *testing.T) {
	t.Parallel()

	thread := model.Thread{
		ID:          "thread-passive",
		Title:       "Passive",
		ProjectName: "Codex",
		Status:      "idle",
	}
	current := appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-1",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-fp-1",
		LatestFinalText:  "Done.",
	}
	previous := appserver.CompactSnapshot(nil, current, time.Now().UTC())

	if snapshotHasPassiveChange(&previous, &current) {
		t.Fatal("snapshotHasPassiveChange returned true for identical terminal replay")
	}

	current.LatestFinalFP = "upgraded-fingerprint"
	current.LatestFinalText = "Done."
	if snapshotHasPassiveChange(&previous, &current) {
		t.Fatal("snapshotHasPassiveChange returned true for same terminal turn with changed fingerprint")
	}

	current.LatestTurnID = "turn-2"
	current.LatestFinalFP = "final-fp-2"
	current.LatestFinalText = "Done again."
	if !snapshotHasPassiveChange(&previous, &current) {
		t.Fatal("snapshotHasPassiveChange returned false for new terminal turn")
	}
}

func TestPollTrackedSyncsFirstSeenRecentTerminalCatchup(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	now := time.Now().UTC().Unix()
	thread := model.Thread{
		ID:          "thread-catchup-terminal",
		Title:       "Catchup terminal",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   now,
		Status:      "completed",
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.SetGlobalObserverTarget(ctx, 123456789, 0, true); err != nil {
		t.Fatalf("SetGlobalObserverTarget failed: %v", err)
	}
	service.poll = &stubSession{
		threadReads: map[string]map[string]any{
			thread.ID: {
				"id":     thread.ID,
				"name":   thread.Title,
				"cwd":    thread.CWD,
				"status": "completed",
				"turns": []any{
					map[string]any{
						"id":     "turn-catchup",
						"status": "completed",
						"items": []any{
							map[string]any{
								"id":    "agent-final",
								"type":  "agentMessage",
								"phase": "final_answer",
								"text":  "CATCHUP_OK",
							},
						},
					},
				},
			},
		},
	}
	service.pollConnected = true

	service.pollTracked(ctx)

	if len(sender.messages) != 3 {
		t.Fatalf("message count = %d, want 3 live trio messages for first-seen terminal catchup; messages=%#v", len(sender.messages), sender.messages)
	}
	foundFinal := false
	for _, message := range sender.edits {
		if hasHeaderKind(message.text, "Final") && strings.Contains(message.text, "[Codex]") && strings.Contains(message.text, "[Catchup terminal]") && strings.Contains(message.text, "CATCHUP_OK") {
			foundFinal = true
			break
		}
	}
	if !foundFinal {
		t.Fatalf("final card edit not found: %#v", sender.edits)
	}
	if len(sender.deletes) != 2 {
		t.Fatalf("deletes = %#v, want tool/output cleanup after final", sender.deletes)
	}
}

func TestRefreshObserverIndexSyncsRecentThreadsWhenBackgroundObserverEnabled(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	if err := service.store.SetGlobalObserverTarget(ctx, 123456789, 0, true); err != nil {
		t.Fatalf("SetGlobalObserverTarget failed: %v", err)
	}

	thread := model.Thread{
		ID:           "thread-from-list",
		Title:        "From list",
		ProjectName:  "Codex",
		CWD:          `C:\Users\you\Projects\Codex`,
		UpdatedAt:    time.Now().UTC().Unix(),
		Status:       "inProgress",
		ActiveTurnID: "turn-1",
	}
	service.poll = &stubSession{
		threadListResult: map[string]any{
			"threads": []any{
				map[string]any{
					"id":           thread.ID,
					"name":         thread.Title,
					"cwd":          thread.CWD,
					"updatedAt":    float64(thread.UpdatedAt),
					"status":       thread.Status,
					"activeTurnId": thread.ActiveTurnID,
				},
			},
		},
	}
	service.pollConnected = true

	service.refreshObserverIndex(ctx)

	stored, err := service.store.GetThread(ctx, thread.ID)
	if err != nil {
		t.Fatalf("GetThread failed: %v", err)
	}
	if stored == nil {
		t.Fatal("expected thread from thread/list to be cached by refreshObserverIndex")
	}
	if stored.ID != thread.ID || stored.ActiveTurnID != thread.ActiveTurnID {
		t.Fatalf("stored thread = %#v, want id=%q activeTurn=%q", stored, thread.ID, thread.ActiveTurnID)
	}
}

func TestRefreshObserverIndexSkipsSyncWithoutBackgroundObserver(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	if err := service.store.SetGlobalObserverTarget(ctx, 123456789, 0, false); err != nil {
		t.Fatalf("SetGlobalObserverTarget(disabled) failed: %v", err)
	}
	stub := &stubSession{}
	service.poll = stub
	service.pollConnected = true

	service.refreshObserverIndex(ctx)

	if stub.threadListCalls != 0 {
		t.Fatalf("thread list calls = %d, want 0 without background observer", stub.threadListCalls)
	}
}

func TestPlainReplyToSyntheticPlanPromptUsesTurnSteer(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	thread := model.Thread{ID: "synthetic-plan-thread", Title: "Synthetic", ProjectName: "Codex", CWD: `C:\Users\you\Projects\Codex`}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.PutMessageRoute(ctx, model.MessageRoute{
		ChatID:    123456789,
		TopicID:   0,
		MessageID: 777,
		ThreadID:  thread.ID,
		TurnID:    "turn-synthetic",
		EventID:   "synthetic-fp",
		CreatedAt: model.NowString(),
	}); err != nil {
		t.Fatalf("PutMessageRoute failed: %v", err)
	}
	stub := &stubSession{}
	service.live = stub
	service.liveConnected = true

	response, err := service.handlePlainText(ctx, 123456789, 0, "Use option A", 777)
	if err != nil {
		t.Fatalf("handlePlainText failed: %v", err)
	}
	if response == nil || response.ThreadID != thread.ID || response.TurnID != "turn-synthetic" {
		t.Fatalf("response = %#v, want thread/turn synthetic", response)
	}
	if len(stub.turnSteerCalls) != 1 {
		t.Fatalf("turnSteerCalls = %#v, want one steer", stub.turnSteerCalls)
	}
	if got := stub.turnSteerCalls[0]; got.threadID != thread.ID || got.turnID != "turn-synthetic" || got.message != "Use option A" {
		t.Fatalf("turn steer call = %#v, want synthetic plan answer", got)
	}
	if len(stub.turnStartCalls) != 0 {
		t.Fatalf("turnStartCalls = %#v, want no fallback start", stub.turnStartCalls)
	}
}

func TestPlainReplyToSyntheticPlanPromptFallsBackToTurnStart(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	thread := model.Thread{ID: "synthetic-stale-thread", Title: "Synthetic stale", ProjectName: "Codex", CWD: `C:\Users\you\Projects\Codex`}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.PutMessageRoute(ctx, model.MessageRoute{
		ChatID:    123456789,
		TopicID:   0,
		MessageID: 778,
		ThreadID:  thread.ID,
		TurnID:    "turn-stale",
		EventID:   "synthetic-fp-stale",
		CreatedAt: model.NowString(),
	}); err != nil {
		t.Fatalf("PutMessageRoute failed: %v", err)
	}
	stub := &stubSession{turnSteerErr: errors.New("turn already completed")}
	service.live = stub
	service.liveConnected = true

	response, err := service.handlePlainText(ctx, 123456789, 0, "Start new turn instead", 778)
	if err != nil {
		t.Fatalf("handlePlainText failed: %v", err)
	}
	if response == nil || response.ThreadID != thread.ID || response.TurnID != "started-turn" {
		t.Fatalf("response = %#v, want fallback started turn", response)
	}
	if len(stub.turnSteerCalls) != 1 {
		t.Fatalf("turnSteerCalls = %#v, want one failed steer", stub.turnSteerCalls)
	}
	if len(stub.turnStartCalls) != 1 {
		t.Fatalf("turnStartCalls = %#v, want one fallback start", stub.turnStartCalls)
	}
	if got := stub.turnStartCalls[0]; got.threadID != thread.ID || got.message != "Start new turn instead" {
		t.Fatalf("turn start call = %#v, want fallback answer", got)
	}
}

func TestPlainReplyToRealPlanPromptUsesServerRequest(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	if err := service.store.SavePendingApproval(ctx, model.PendingApproval{
		RequestID:   "request-plan-reply",
		ThreadID:    "real-plan-thread",
		TurnID:      "real-plan-turn",
		PromptKind:  "user_input",
		Question:    "Need input.",
		PayloadJSON: `{"questions":[{"id":"choice","question":"Need input?","options":[{"label":"The answer","description":"Use answer."}]}]}`,
		Status:      "pending",
		UpdatedAt:   model.NowString(),
	}); err != nil {
		t.Fatalf("SavePendingApproval failed: %v", err)
	}
	if err := service.store.PutMessageRoute(ctx, model.MessageRoute{
		ChatID:    123456789,
		TopicID:   0,
		MessageID: 779,
		ThreadID:  "real-plan-thread",
		TurnID:    "real-plan-turn",
		EventID:   "plan_request:request-plan-reply",
		CreatedAt: model.NowString(),
	}); err != nil {
		t.Fatalf("PutMessageRoute failed: %v", err)
	}
	stub := &stubSession{}
	service.live = stub
	service.liveConnected = true

	response, err := service.handlePlainText(ctx, 123456789, 0, "The answer", 779)
	if err != nil {
		t.Fatalf("handlePlainText failed: %v", err)
	}
	if response == nil || response.ThreadID != "real-plan-thread" || response.TurnID != "real-plan-turn" {
		t.Fatalf("response = %#v, want real plan thread/turn", response)
	}
	if len(stub.respondRequestCalls) != 1 {
		t.Fatalf("respondRequestCalls = %#v, want one server request response", stub.respondRequestCalls)
	}
	got := stub.respondRequestCalls[0]
	answers, _ := got.result["answers"].(map[string]any)
	choice, _ := answers["choice"].(map[string]any)
	values, _ := choice["answers"].([]string)
	if got.requestID != "request-plan-reply" || len(values) != 1 || values[0] != "The answer" {
		t.Fatalf("respond request call = %#v, want request-plan-reply schema answers", got)
	}
	if len(stub.turnSteerCalls) != 0 || len(stub.turnStartCalls) != 0 {
		t.Fatalf("unexpected turn calls: steer=%#v start=%#v", stub.turnSteerCalls, stub.turnStartCalls)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()

	root := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Home:    root,
			DataDir: filepath.Join(root, "data"),
			LogDir:  filepath.Join(root, "logs"),
			DBPath:  filepath.Join(root, "data", "state.sqlite"),
		},
		AllowedUserIDs: []int64{123456789},
		DefaultCWD:     `C:\Users\you\Projects\Codex`,
	}
	service, err := New(cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}
	t.Cleanup(func() {
		_ = service.Close()
	})
	return service
}

type stubSession struct {
	threadReads         map[string]map[string]any
	threadListResult    map[string]any
	threadListCalls     int
	turnSteerErr        error
	turnSteerCalls      []turnCall
	turnStartCalls      []turnCall
	respondRequestCalls []respondRequestCall
}

type turnCall struct {
	threadID string
	turnID   string
	message  string
	cwd      string
}

type respondRequestCall struct {
	requestID string
	result    map[string]any
}

func (s *stubSession) Start(ctx context.Context) error { return nil }
func (s *stubSession) Close() error                    { return nil }
func (s *stubSession) Subscribe() <-chan appserver.Event {
	return nil
}
func (s *stubSession) ThreadList(ctx context.Context, limit int, cursor string) (map[string]any, error) {
	s.threadListCalls++
	return s.threadListResult, nil
}
func (s *stubSession) ThreadRead(ctx context.Context, threadID string, includeTurns bool) (map[string]any, error) {
	if payload, ok := s.threadReads[threadID]; ok {
		return payload, nil
	}
	return nil, nil
}
func (s *stubSession) ThreadResume(ctx context.Context, threadID, cwd string) (map[string]any, error) {
	return nil, nil
}
func (s *stubSession) ThreadStart(ctx context.Context, cwd string) (map[string]any, error) {
	return nil, nil
}
func (s *stubSession) TurnStart(ctx context.Context, threadID, message, cwd string) (map[string]any, error) {
	s.turnStartCalls = append(s.turnStartCalls, turnCall{threadID: threadID, message: message, cwd: cwd})
	return map[string]any{"turn": map[string]any{"id": "started-turn"}}, nil
}
func (s *stubSession) TurnSteer(ctx context.Context, threadID, turnID, message string) (map[string]any, error) {
	s.turnSteerCalls = append(s.turnSteerCalls, turnCall{threadID: threadID, turnID: turnID, message: message})
	if s.turnSteerErr != nil {
		return nil, s.turnSteerErr
	}
	return map[string]any{"turn": map[string]any{"id": turnID}}, nil
}
func (s *stubSession) TurnInterrupt(ctx context.Context, threadID, turnID string) error {
	return nil
}
func (s *stubSession) RespondServerRequest(ctx context.Context, requestID string, result map[string]any) error {
	s.respondRequestCalls = append(s.respondRequestCalls, respondRequestCall{requestID: requestID, result: result})
	return nil
}
func (s *stubSession) StderrTail() []string { return nil }
