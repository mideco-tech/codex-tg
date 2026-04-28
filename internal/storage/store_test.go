package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestGlobalObserverTargetPersistsAndObserveOffDisablesMonitoring(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.sqlite")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%s) failed: %v", path, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	ctx := context.Background()

	if err := store.SetGlobalObserverTarget(ctx, 123456789, 9, true); err != nil {
		t.Fatalf("SetGlobalObserverTarget(enable) failed: %v", err)
	}
	enabledRaw, err := store.GetState(ctx, "observer.global_enabled")
	if err != nil {
		t.Fatalf("GetState(observer.global_enabled) failed: %v", err)
	}
	if enabledRaw != "true" {
		t.Fatalf("observer.global_enabled = %q, want true", enabledRaw)
	}
	chatIDRaw, err := store.GetState(ctx, "observer.global_chat_id")
	if err != nil {
		t.Fatalf("GetState(observer.global_chat_id) failed: %v", err)
	}
	if chatIDRaw != "123456789" {
		t.Fatalf("observer.global_chat_id = %q, want 123456789", chatIDRaw)
	}
	topicIDRaw, err := store.GetState(ctx, "observer.global_topic_id")
	if err != nil {
		t.Fatalf("GetState(observer.global_topic_id) failed: %v", err)
	}
	if topicIDRaw != "9" {
		t.Fatalf("observer.global_topic_id = %q, want 9", topicIDRaw)
	}
	sinceRaw, err := store.GetState(ctx, "observer.global_since_unix")
	if err != nil {
		t.Fatalf("GetState(observer.global_since_unix) failed: %v", err)
	}
	if sinceRaw == "" {
		t.Fatal("observer.global_since_unix must be set when global observer is enabled")
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open(reopened) failed: %v", err)
	}
	defer reopened.Close()

	target, configured, err := reopened.GetGlobalObserverTarget(ctx)
	if err != nil {
		t.Fatalf("GetGlobalObserverTarget(reopened) failed: %v", err)
	}
	if !configured {
		t.Fatal("GetGlobalObserverTarget(reopened) should report configured=true")
	}
	if target == nil {
		t.Fatal("GetGlobalObserverTarget(reopened) returned nil target")
	}
	if target.ChatID != 123456789 || target.TopicID != 9 || !target.Enabled {
		t.Fatalf("reopened global target = %#v, want enabled target 123456789:9", target)
	}
	sinceUnix, ok, err := reopened.GetGlobalObserverSinceUnix(ctx)
	if err != nil {
		t.Fatalf("GetGlobalObserverSinceUnix(reopened) failed: %v", err)
	}
	if !ok || sinceUnix <= 0 {
		t.Fatalf("GetGlobalObserverSinceUnix(reopened) = %d ok=%t, want positive value", sinceUnix, ok)
	}

	if err := reopened.SetGlobalObserverTarget(ctx, 123456789, 9, false); err != nil {
		t.Fatalf("SetGlobalObserverTarget(disable) failed: %v", err)
	}
	target, configured, err = reopened.GetGlobalObserverTarget(ctx)
	if err != nil {
		t.Fatalf("GetGlobalObserverTarget(disabled) failed: %v", err)
	}
	if !configured {
		t.Fatal("GetGlobalObserverTarget(disabled) should remain configured")
	}
	if target != nil {
		t.Fatalf("disabled global target = %#v, want nil", target)
	}
	enabledRaw, err = reopened.GetState(ctx, "observer.global_enabled")
	if err != nil {
		t.Fatalf("GetState(observer.global_enabled after disable) failed: %v", err)
	}
	if enabledRaw != "false" {
		t.Fatalf("observer.global_enabled after disable = %q, want false", enabledRaw)
	}
}

func TestBindingAndGlobalObserverCanCoexist(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	ctx := context.Background()

	if err := store.SetGlobalObserverTarget(ctx, 123456789, 0, true); err != nil {
		t.Fatalf("SetGlobalObserverTarget failed: %v", err)
	}
	if err := store.SetBinding(ctx, 123456789, 0, "thread-1", model.BindingModeBound); err != nil {
		t.Fatalf("SetBinding failed: %v", err)
	}

	binding, err := store.GetBinding(ctx, 123456789, 0)
	if err != nil {
		t.Fatalf("GetBinding failed: %v", err)
	}
	if binding == nil || binding.ThreadID != "thread-1" {
		t.Fatalf("binding = %#v, want thread-1", binding)
	}

	target, configured, err := store.GetGlobalObserverTarget(ctx)
	if err != nil {
		t.Fatalf("GetGlobalObserverTarget failed: %v", err)
	}
	if !configured || target == nil {
		t.Fatalf("global observer target = %#v configured=%t, want enabled target", target, configured)
	}
	if target.ChatID != 123456789 || target.TopicID != 0 {
		t.Fatalf("global observer target = %#v, want 123456789:0", target)
	}
}

func TestDeliveryQueueClaimRetryAndComplete(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	ctx := context.Background()
	item := model.DeliveryQueueItem{
		EventID:     "event-1",
		ChatKey:     model.ChatKey(123456789, 0),
		ChatID:      123456789,
		TopicID:     0,
		ThreadID:    "thread-1",
		Kind:        "observer",
		Status:      model.DeliveryStatusPending,
		AvailableAt: model.NowString(),
		PayloadJSON: `{"text":"hello"}`,
		CreatedAt:   model.NowString(),
		UpdatedAt:   model.NowString(),
	}
	if err := store.EnqueueDelivery(ctx, item); err != nil {
		t.Fatalf("EnqueueDelivery failed: %v", err)
	}

	batch, err := store.ClaimDeliveryBatch(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimDeliveryBatch failed: %v", err)
	}
	if len(batch) != 1 {
		t.Fatalf("ClaimDeliveryBatch len = %d, want 1", len(batch))
	}
	if batch[0].Status != model.DeliveryStatusPending {
		t.Fatalf("Claimed status = %q, want %q", batch[0].Status, model.DeliveryStatusPending)
	}

	retryAt := time.Now().UTC().Add(5 * time.Second)
	if err := store.FailDelivery(ctx, batch[0].ID, 1, retryAt, "temporary failure", false); err != nil {
		t.Fatalf("FailDelivery failed: %v", err)
	}
	if err := store.RecordDeliveryAttempt(ctx, batch[0].ID, 1, "send_error", "temporary failure"); err != nil {
		t.Fatalf("RecordDeliveryAttempt failed: %v", err)
	}

	backlog, err := store.DeliveryQueueBacklog(ctx)
	if err != nil {
		t.Fatalf("DeliveryQueueBacklog failed: %v", err)
	}
	if backlog != 1 {
		t.Fatalf("DeliveryQueueBacklog = %d, want 1", backlog)
	}
}

func TestThreadPanelLifecycleKeepsSingleCurrentPanelPerChatThread(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	ctx := context.Background()

	first, err := store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:              123456789,
		TopicID:             0,
		ProjectName:         "Codex",
		ThreadID:            "thread-1",
		SummaryMessageID:    101,
		ToolMessageID:       102,
		OutputMessageID:     103,
		CurrentTurnID:       "turn-1",
		Status:              "inProgress",
		ArchiveEnabled:      true,
		LastSummaryHash:     "summary-1",
		LastToolHash:        "tool-1",
		LastOutputHash:      "output-1",
		RunNoticeMessageID:  99,
		LastRunNoticeFP:     "run-fp-1",
		UserMessageID:       100,
		LastUserNoticeFP:    "user-fp-1",
		PlanPromptMessageID: 110,
		LastPlanPromptFP:    "plan-fp-1",
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel(first) failed: %v", err)
	}

	second, err := store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		TopicID:          0,
		ProjectName:      "Codex",
		ThreadID:         "thread-1",
		SummaryMessageID: 201,
		ToolMessageID:    202,
		OutputMessageID:  203,
		CurrentTurnID:    "turn-2",
		Status:           "completed",
		ArchiveEnabled:   true,
		LastSummaryHash:  "summary-2",
		LastToolHash:     "tool-2",
		LastOutputHash:   "output-2",
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel(second) failed: %v", err)
	}

	current, err := store.GetCurrentThreadPanel(ctx, 123456789, 0, "thread-1")
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if current == nil || current.ID != second.ID {
		t.Fatalf("current panel = %#v, want second panel %#v", current, second)
	}

	firstLoaded, err := store.GetThreadPanelByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetThreadPanelByID(first) failed: %v", err)
	}
	if firstLoaded == nil {
		t.Fatal("GetThreadPanelByID(first) returned nil")
	}
	if firstLoaded.IsCurrent {
		t.Fatalf("first panel should no longer be current: %#v", firstLoaded)
	}
	if firstLoaded.UserMessageID != 100 || firstLoaded.LastUserNoticeFP != "user-fp-1" {
		t.Fatalf("first user notice state = id %d fp %q, want 100/user-fp-1", firstLoaded.UserMessageID, firstLoaded.LastUserNoticeFP)
	}
	if firstLoaded.RunNoticeMessageID != 99 || firstLoaded.LastRunNoticeFP != "run-fp-1" {
		t.Fatalf("first run notice state = id %d fp %q, want 99/run-fp-1", firstLoaded.RunNoticeMessageID, firstLoaded.LastRunNoticeFP)
	}
	if firstLoaded.PlanPromptMessageID != 110 || firstLoaded.LastPlanPromptFP != "plan-fp-1" {
		t.Fatalf("first plan prompt state = id %d fp %q, want 110/plan-fp-1", firstLoaded.PlanPromptMessageID, firstLoaded.LastPlanPromptFP)
	}

	if err := store.UpdateThreadPanelUserNotice(ctx, second.ID, 250, "user-fp-2"); err != nil {
		t.Fatalf("UpdateThreadPanelUserNotice failed: %v", err)
	}
	secondLoaded, err := store.GetThreadPanelByID(ctx, second.ID)
	if err != nil {
		t.Fatalf("GetThreadPanelByID(second) failed: %v", err)
	}
	if secondLoaded.UserMessageID != 250 || secondLoaded.LastUserNoticeFP != "user-fp-2" {
		t.Fatalf("second user notice state = id %d fp %q, want 250/user-fp-2", secondLoaded.UserMessageID, secondLoaded.LastUserNoticeFP)
	}
	if err := store.UpdateThreadPanelRunNotice(ctx, second.ID, 240, "run-fp-2"); err != nil {
		t.Fatalf("UpdateThreadPanelRunNotice failed: %v", err)
	}
	secondLoaded, err = store.GetThreadPanelByID(ctx, second.ID)
	if err != nil {
		t.Fatalf("GetThreadPanelByID(second after run notice) failed: %v", err)
	}
	if secondLoaded.RunNoticeMessageID != 240 || secondLoaded.LastRunNoticeFP != "run-fp-2" {
		t.Fatalf("second run notice state = id %d fp %q, want 240/run-fp-2", secondLoaded.RunNoticeMessageID, secondLoaded.LastRunNoticeFP)
	}
	if err := store.UpdateThreadPanelPlanPrompt(ctx, second.ID, 260, "plan-fp-2"); err != nil {
		t.Fatalf("UpdateThreadPanelPlanPrompt failed: %v", err)
	}
	secondLoaded, err = store.GetThreadPanelByID(ctx, second.ID)
	if err != nil {
		t.Fatalf("GetThreadPanelByID(second after plan prompt) failed: %v", err)
	}
	if secondLoaded.PlanPromptMessageID != 260 || secondLoaded.LastPlanPromptFP != "plan-fp-2" {
		t.Fatalf("second plan prompt state = id %d fp %q, want 260/plan-fp-2", secondLoaded.PlanPromptMessageID, secondLoaded.LastPlanPromptFP)
	}
}

func TestThreadPanelSourceModePersistsStableAndPerRunValues(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	ctx := context.Background()

	stable, err := store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		TopicID:          0,
		ProjectName:      "Codex",
		ThreadID:         "thread-1",
		SourceMode:       "stable",
		SummaryMessageID: 301,
		ToolMessageID:    302,
		OutputMessageID:  303,
		CurrentTurnID:    "turn-1",
		Status:           "completed",
		ArchiveEnabled:   true,
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel(stable) failed: %v", err)
	}
	if stable.SourceMode != "stable" {
		t.Fatalf("stable panel SourceMode = %q, want stable", stable.SourceMode)
	}

	perRun, err := store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		TopicID:          0,
		ProjectName:      "Codex",
		ThreadID:         "thread-1",
		SourceMode:       "per_run",
		SummaryMessageID: 401,
		ToolMessageID:    402,
		OutputMessageID:  403,
		CurrentTurnID:    "turn-2",
		Status:           "completed",
		ArchiveEnabled:   true,
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel(per_run) failed: %v", err)
	}
	if perRun.SourceMode != "per_run" {
		t.Fatalf("per_run panel SourceMode = %q, want per_run", perRun.SourceMode)
	}

	current, err := store.GetCurrentThreadPanel(ctx, 123456789, 0, "thread-1")
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if current == nil {
		t.Fatal("GetCurrentThreadPanel returned nil")
	}
	if current.ID != perRun.ID {
		t.Fatalf("current panel ID = %d, want %d", current.ID, perRun.ID)
	}
	if current.SourceMode != "per_run" {
		t.Fatalf("current panel SourceMode = %q, want per_run", current.SourceMode)
	}

	stableLoaded, err := store.GetThreadPanelByID(ctx, stable.ID)
	if err != nil {
		t.Fatalf("GetThreadPanelByID(stable) failed: %v", err)
	}
	if stableLoaded == nil {
		t.Fatal("GetThreadPanelByID(stable) returned nil")
	}
	if stableLoaded.SourceMode != "stable" {
		t.Fatalf("stable panel reloaded SourceMode = %q, want stable", stableLoaded.SourceMode)
	}
}

func TestSteerStateArmLoadAndClear(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	ctx := context.Background()

	state := model.SteerState{
		ChatKey:   model.ChatKey(123456789, 0),
		ChatID:    123456789,
		TopicID:   0,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		PanelID:   77,
		ExpiresAt: model.NowString(),
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	}
	if err := store.ArmSteerState(ctx, state); err != nil {
		t.Fatalf("ArmSteerState failed: %v", err)
	}

	loaded, err := store.GetSteerState(ctx, 123456789, 0)
	if err != nil {
		t.Fatalf("GetSteerState failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("GetSteerState returned nil")
	}
	if loaded.ThreadID != "thread-1" || loaded.TurnID != "turn-1" || loaded.PanelID != 77 {
		t.Fatalf("loaded steer state = %#v, want thread-1/turn-1/panel 77", loaded)
	}

	if err := store.ClearSteerState(ctx, 123456789, 0); err != nil {
		t.Fatalf("ClearSteerState failed: %v", err)
	}
	loaded, err = store.GetSteerState(ctx, 123456789, 0)
	if err != nil {
		t.Fatalf("GetSteerState(after clear) failed: %v", err)
	}
	if loaded != nil {
		t.Fatalf("GetSteerState(after clear) = %#v, want nil", loaded)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()

	path := filepath.Join(t.TempDir(), "state.sqlite")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%s) failed: %v", path, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}
