package appserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestStringValueTreatsNilLiteralAsMissing(t *testing.T) {
	t.Parallel()

	if got := stringValue("<nil>", "fallback"); got != "fallback" {
		t.Fatalf("stringValue(<nil>) = %q, want fallback", got)
	}
	if got := stringValue("ok", "fallback"); got != "ok" {
		t.Fatalf("stringValue(ok) = %q, want ok", got)
	}
}

func TestThreadsFromListSkipsInternalSubAgentThreads(t *testing.T) {
	t.Parallel()

	threads := ThreadsFromList(map[string]any{
		"data": []any{
			map[string]any{
				"id":      "visible-thread",
				"name":    "Visible",
				"cwd":     "/Users/example/work",
				"preview": "user work",
			},
			map[string]any{
				"id":        "ephemeral-thread",
				"cwd":       "/Users/example/.codex/memories",
				"ephemeral": true,
			},
			map[string]any{
				"id":  "sub-agent-thread",
				"cwd": "/Users/example/.codex/memories",
				"source": map[string]any{
					"subAgent": "memory_consolidation",
				},
			},
		},
	})

	if len(threads) != 1 {
		t.Fatalf("len(threads) = %d, want 1: %#v", len(threads), threads)
	}
	if threads[0].ID != "visible-thread" {
		t.Fatalf("thread id = %q, want visible-thread", threads[0].ID)
	}
}

func TestDiffSnapshotEmitsCompletionForNewTerminalTurn(t *testing.T) {
	previous := &model.ThreadSnapshotState{
		LastSeenTurnID:     "old-turn",
		LastSeenTurnStatus: "interrupted",
		LastCompletionFP:   fingerprint("old-turn", "interrupted"),
	}
	current := ThreadReadSnapshot{
		Thread: model.Thread{
			ID:          "thread-1",
			Title:       "Node check",
			ProjectName: "Project",
		},
		LatestTurnID:     "new-turn",
		LatestTurnStatus: "interrupted",
	}

	events := DiffSnapshot(previous, current)

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %#v", len(events), events)
	}
	if events[0].Kind != "turn_started" {
		t.Fatalf("expected first event to be turn_started, got %q", events[0].Kind)
	}
	if events[1].Kind != "turn_completed" {
		t.Fatalf("expected second event to be turn_completed, got %q", events[1].Kind)
	}
	if events[1].Status != "interrupted" {
		t.Fatalf("expected interrupted completion, got %q", events[1].Status)
	}
}

func TestCompactSnapshotStoresCompletionFingerprint(t *testing.T) {
	current := ThreadReadSnapshot{
		Thread: model.Thread{
			ID:          "thread-1",
			Title:       "Node check",
			ProjectName: "Project",
		},
		LatestTurnID:     "turn-1",
		LatestTurnStatus: "completed",
	}

	snapshot := CompactSnapshot(nil, current, time.Date(2026, time.April, 22, 15, 0, 0, 0, time.UTC))

	want := fingerprint("turn-1", "completed")
	if snapshot.LastCompletionFP != want {
		t.Fatalf("expected completion fingerprint %q, got %q", want, snapshot.LastCompletionFP)
	}
}

func TestCompactSnapshotStoresToolTimingOnFirstSeen(t *testing.T) {
	t.Parallel()

	polledAt := time.Date(2026, time.May, 1, 22, 39, 21, 0, time.UTC)
	current := ThreadReadSnapshot{
		Thread: model.Thread{
			ID:     "thread-tool-timing",
			Status: "inProgress",
		},
		LatestTurnID:     "turn-tool-timing",
		LatestTurnStatus: "inProgress",
		LatestToolID:     "tool-1",
		LatestToolKind:   "commandExecution",
		LatestToolLabel:  "sleep 20",
		LatestToolStatus: "running",
		LatestToolFP:     fingerprint("tool", "tool-1", "sleep 20", "running"),
	}

	state := CompactSnapshot(nil, current, polledAt)

	var compact ThreadReadSnapshot
	if err := json.Unmarshal(state.CompactJSON, &compact); err != nil {
		t.Fatalf("unmarshal compact snapshot: %v", err)
	}
	want := polledAt.Format(time.RFC3339Nano)
	if compact.LatestToolStartedAt != want {
		t.Fatalf("LatestToolStartedAt = %q, want %q", compact.LatestToolStartedAt, want)
	}
	if compact.LatestToolUpdatedAt != want {
		t.Fatalf("LatestToolUpdatedAt = %q, want %q", compact.LatestToolUpdatedAt, want)
	}
}

func TestCompactSnapshotPreservesTurnStartedAtWhenToolIsMissing(t *testing.T) {
	t.Parallel()

	firstSeen := time.Date(2026, time.May, 1, 22, 39, 21, 0, time.UTC)
	later := firstSeen.Add(20 * time.Second)
	current := ThreadReadSnapshot{
		Thread: model.Thread{
			ID:     "thread-turn-timing",
			Status: "inProgress",
		},
		LatestTurnID:     "turn-timing",
		LatestTurnStatus: "inProgress",
	}
	previous := CompactSnapshot(nil, current, firstSeen)
	next := CompactSnapshot(&previous, current, later)

	var compact ThreadReadSnapshot
	if err := json.Unmarshal(next.CompactJSON, &compact); err != nil {
		t.Fatalf("unmarshal compact snapshot: %v", err)
	}
	if compact.LatestTurnStartedAt != firstSeen.Format(time.RFC3339Nano) {
		t.Fatalf("LatestTurnStartedAt = %q, want %q", compact.LatestTurnStartedAt, firstSeen.Format(time.RFC3339Nano))
	}
	if compact.LatestTurnUpdatedAt != later.Format(time.RFC3339Nano) {
		t.Fatalf("LatestTurnUpdatedAt = %q, want %q", compact.LatestTurnUpdatedAt, later.Format(time.RFC3339Nano))
	}
}

func TestCompactSnapshotPreservesToolTimingWhenUnchanged(t *testing.T) {
	t.Parallel()

	firstSeen := time.Date(2026, time.May, 1, 22, 39, 21, 0, time.UTC)
	later := firstSeen.Add(10 * time.Minute)
	current := ThreadReadSnapshot{
		Thread: model.Thread{
			ID:     "thread-tool-timing",
			Status: "inProgress",
		},
		LatestTurnID:     "turn-tool-timing",
		LatestTurnStatus: "inProgress",
		LatestToolID:     "tool-1",
		LatestToolKind:   "commandExecution",
		LatestToolLabel:  "sleep 20",
		LatestToolStatus: "running",
		LatestToolFP:     fingerprint("tool", "tool-1", "sleep 20", "running"),
	}
	previous := CompactSnapshot(nil, current, firstSeen)

	state := CompactSnapshot(&previous, current, later)

	var compact ThreadReadSnapshot
	if err := json.Unmarshal(state.CompactJSON, &compact); err != nil {
		t.Fatalf("unmarshal compact snapshot: %v", err)
	}
	want := firstSeen.Format(time.RFC3339Nano)
	if compact.LatestToolStartedAt != want {
		t.Fatalf("LatestToolStartedAt = %q, want %q", compact.LatestToolStartedAt, want)
	}
	if compact.LatestToolUpdatedAt != want {
		t.Fatalf("LatestToolUpdatedAt = %q, want %q", compact.LatestToolUpdatedAt, want)
	}
}

func TestCompactSnapshotUpdatesToolLastUpdateWhenFingerprintChanges(t *testing.T) {
	t.Parallel()

	firstSeen := time.Date(2026, time.May, 1, 22, 39, 21, 0, time.UTC)
	later := firstSeen.Add(10 * time.Minute)
	initial := ThreadReadSnapshot{
		Thread: model.Thread{
			ID:     "thread-tool-timing",
			Status: "inProgress",
		},
		LatestTurnID:     "turn-tool-timing",
		LatestTurnStatus: "inProgress",
		LatestToolID:     "tool-1",
		LatestToolKind:   "commandExecution",
		LatestToolLabel:  "sleep 20",
		LatestToolStatus: "running",
		LatestToolFP:     fingerprint("tool", "tool-1", "sleep 20", "running"),
	}
	previous := CompactSnapshot(nil, initial, firstSeen)
	updated := initial
	updated.LatestToolOutput = "still running"
	updated.LatestToolFP = fingerprint("tool", "tool-1", "sleep 20", "running", updated.LatestToolOutput)

	state := CompactSnapshot(&previous, updated, later)

	var compact ThreadReadSnapshot
	if err := json.Unmarshal(state.CompactJSON, &compact); err != nil {
		t.Fatalf("unmarshal compact snapshot: %v", err)
	}
	if compact.LatestToolStartedAt != firstSeen.Format(time.RFC3339Nano) {
		t.Fatalf("LatestToolStartedAt = %q, want %q", compact.LatestToolStartedAt, firstSeen.Format(time.RFC3339Nano))
	}
	if compact.LatestToolUpdatedAt != later.Format(time.RFC3339Nano) {
		t.Fatalf("LatestToolUpdatedAt = %q, want %q", compact.LatestToolUpdatedAt, later.Format(time.RFC3339Nano))
	}
}

func TestCompactSnapshotDoesNotPreserveActiveLiveToolWhenThreadReadOmitsTool(t *testing.T) {
	t.Parallel()

	firstSeen := time.Date(2026, time.May, 1, 23, 46, 1, 0, time.UTC)
	later := firstSeen.Add(8 * time.Second)
	previousCurrent := ThreadReadSnapshot{
		Thread: model.Thread{
			ID:     "thread-live-tool",
			Status: "inProgress",
		},
		LatestTurnID:        "turn-live-tool",
		LatestTurnStatus:    "inProgress",
		LatestToolID:        "tool-sleep",
		LatestToolKind:      "commandExecution",
		LatestToolLabel:     "sleep 20; printf 'done\\n'",
		LatestToolStatus:    "running",
		LatestToolFP:        fingerprint("tool", "tool-sleep", "running"),
		LatestToolStartedAt: firstSeen.Format(time.RFC3339Nano),
		LatestToolUpdatedAt: firstSeen.Format(time.RFC3339Nano),
	}
	previous := CompactSnapshot(nil, previousCurrent, firstSeen)
	pollWithoutTool := ThreadReadSnapshot{
		Thread: model.Thread{
			ID:     "thread-live-tool",
			Status: "inProgress",
		},
		LatestTurnID:     "turn-live-tool",
		LatestTurnStatus: "inProgress",
	}

	state := CompactSnapshot(&previous, pollWithoutTool, later)

	var compact ThreadReadSnapshot
	if err := json.Unmarshal(state.CompactJSON, &compact); err != nil {
		t.Fatalf("unmarshal compact snapshot: %v", err)
	}
	if compact.LatestToolLabel != "" {
		t.Fatalf("LatestToolLabel = %q, want omitted live tool to stay omitted", compact.LatestToolLabel)
	}
	if compact.LatestToolStatus != "" {
		t.Fatalf("LatestToolStatus = %q, want omitted live tool status to stay omitted", compact.LatestToolStatus)
	}
	if compact.LatestTurnStartedAt != firstSeen.Format(time.RFC3339Nano) {
		t.Fatalf("LatestTurnStartedAt = %q, want %q", compact.LatestTurnStartedAt, firstSeen.Format(time.RFC3339Nano))
	}
}

func TestCompactSnapshotDoesNotPreserveLiveToolAcrossTurns(t *testing.T) {
	t.Parallel()

	firstSeen := time.Date(2026, time.May, 1, 23, 46, 1, 0, time.UTC)
	previous := CompactSnapshot(nil, ThreadReadSnapshot{
		Thread: model.Thread{
			ID:     "thread-live-tool",
			Status: "inProgress",
		},
		LatestTurnID:     "turn-old",
		LatestTurnStatus: "inProgress",
		LatestToolID:     "tool-old",
		LatestToolKind:   "commandExecution",
		LatestToolLabel:  "old command",
		LatestToolStatus: "running",
		LatestToolFP:     fingerprint("tool", "tool-old", "running"),
	}, firstSeen)
	nextTurnWithoutTool := ThreadReadSnapshot{
		Thread: model.Thread{
			ID:     "thread-live-tool",
			Status: "inProgress",
		},
		LatestTurnID:     "turn-new",
		LatestTurnStatus: "inProgress",
	}

	state := CompactSnapshot(&previous, nextTurnWithoutTool, firstSeen.Add(time.Second))

	var compact ThreadReadSnapshot
	if err := json.Unmarshal(state.CompactJSON, &compact); err != nil {
		t.Fatalf("unmarshal compact snapshot: %v", err)
	}
	if compact.LatestToolLabel != "" {
		t.Fatalf("LatestToolLabel = %q, want no stale tool from previous turn", compact.LatestToolLabel)
	}
}

func TestSnapshotFromThreadReadKeepsAgentMessagePhasesAndFinalAnswerOnly(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotFromThreadRead(map[string]any{
		"id":     "thread-1",
		"name":   "Observer smoke",
		"cwd":    `C:\Users\you\Projects\Codex`,
		"status": "inProgress",
		"turns": []any{
			map[string]any{
				"id":     "turn-2",
				"status": "completed",
				"items": []any{
					map[string]any{
						"id":   "user-1",
						"type": "userMessage",
						"content": []any{
							map[string]any{"type": "text", "text": "Check Node.js.\n"},
						},
					},
					map[string]any{
						"id":    "agent-1",
						"type":  "agentMessage",
						"phase": "commentary",
						"text":  "I will check the version first.",
					},
					map[string]any{
						"id":    "agent-2",
						"type":  "agentMessage",
						"phase": "final_answer",
						"text":  "Node.js version is 22.0.0.",
					},
				},
			},
		},
	})

	if got, want := snapshot.LatestFinalText, "Node.js version is 22.0.0."; got != want {
		t.Fatalf("LatestFinalText = %q, want %q", got, want)
	}
	if got, want := snapshot.LatestFinalFP, fingerprint("agentMessage", "agent-2", "final_answer", "Node.js version is 22.0.0."); got != want {
		t.Fatalf("LatestFinalFP = %q, want %q", got, want)
	}
	if got, want := snapshot.LatestUserMessageID, "user-1"; got != want {
		t.Fatalf("LatestUserMessageID = %q, want %q", got, want)
	}
	if got, want := snapshot.LatestUserMessageText, "Check Node.js."; got != want {
		t.Fatalf("LatestUserMessageText = %q, want %q", got, want)
	}
	if got, want := snapshot.LatestUserMessageFP, fingerprint("userMessage", "user-1", "Check Node.js."); got != want {
		t.Fatalf("LatestUserMessageFP = %q, want %q", got, want)
	}
	if len(snapshot.LatestAgentMessageEntries) != 2 {
		t.Fatalf("len(LatestAgentMessageEntries) = %d, want 2", len(snapshot.LatestAgentMessageEntries))
	}
	if got, want := snapshot.LatestAgentMessageEntries[0].Phase, "final_answer"; got != want {
		t.Fatalf("LatestAgentMessageEntries[0].Phase = %q, want %q", got, want)
	}
	if got, want := snapshot.LatestAgentMessageEntries[1].Phase, "commentary"; got != want {
		t.Fatalf("LatestAgentMessageEntries[1].Phase = %q, want %q", got, want)
	}
	if len(snapshot.DetailItems) != 3 {
		t.Fatalf("len(DetailItems) = %d, want 3", len(snapshot.DetailItems))
	}
	if got, want := snapshot.DetailItems[0].Kind, model.DetailItemUser; got != want {
		t.Fatalf("DetailItems[0].Kind = %q, want %q", got, want)
	}
	if got, want := snapshot.DetailItems[1].Kind, model.DetailItemCommentary; got != want {
		t.Fatalf("DetailItems[1].Kind = %q, want %q", got, want)
	}
	if got, want := snapshot.DetailItems[2].Kind, model.DetailItemFinal; got != want {
		t.Fatalf("DetailItems[2].Kind = %q, want %q", got, want)
	}
}

func TestSnapshotFromThreadReadTreatsFinalAnswerAsCompletedWhenStatusIsStale(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotFromThreadRead(map[string]any{
		"id":           "thread-stale",
		"name":         "Stale live state",
		"cwd":          "/Users/example/project",
		"status":       "inProgress",
		"activeTurnId": "turn-stale",
		"turns": []any{
			map[string]any{
				"id":     "turn-stale",
				"status": "inProgress",
				"items": []any{
					map[string]any{
						"id":   "user-1",
						"type": "userMessage",
						"content": []any{
							map[string]any{"type": "text", "text": "Run it."},
						},
					},
					map[string]any{
						"id":    "agent-final",
						"type":  "agentMessage",
						"phase": "final_answer",
						"text":  "Done.",
					},
				},
			},
		},
	})

	if got, want := snapshot.LatestTurnStatus, "completed"; got != want {
		t.Fatalf("LatestTurnStatus = %q, want %q", got, want)
	}
	if got := snapshot.Thread.ActiveTurnID; got != "" {
		t.Fatalf("Thread.ActiveTurnID = %q, want empty", got)
	}
	if got, want := snapshot.Thread.Status, "completed"; got != want {
		t.Fatalf("Thread.Status = %q, want %q", got, want)
	}
	if got, want := snapshot.LatestFinalText, "Done."; got != want {
		t.Fatalf("LatestFinalText = %q, want %q", got, want)
	}
}

func TestSnapshotFromThreadReadBuildsOrderedDetailsAndLinksToolsToCommentary(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotFromThreadRead(map[string]any{
		"id":     "thread-1",
		"name":   "Observer details",
		"cwd":    `C:\Users\you\Projects\Codex`,
		"status": "completed",
		"turns": []any{
			map[string]any{
				"id":     "turn-2",
				"status": "completed",
				"items": []any{
					map[string]any{"id": "user-1", "type": "userMessage", "content": []any{
						map[string]any{"type": "text", "text": "Run node.\n"},
					}},
					map[string]any{"id": "agent-1", "type": "agentMessage", "phase": "commentary", "text": "First step."},
					map[string]any{"id": "cmd-1", "type": "commandExecution", "command": "pwsh -Command node -v", "status": "completed", "aggregatedOutput": "v22.22.2\n"},
					map[string]any{"id": "agent-2", "type": "agentMessage", "phase": "commentary", "text": "Second step."},
					map[string]any{"id": "agent-3", "type": "agentMessage", "phase": "final_answer", "text": "Done."},
				},
			},
		},
	})

	if len(snapshot.DetailItems) != 6 {
		t.Fatalf("len(DetailItems) = %d, want 6: %#v", len(snapshot.DetailItems), snapshot.DetailItems)
	}
	if got, want := snapshot.DetailItems[0].Kind, model.DetailItemUser; got != want {
		t.Fatalf("DetailItems[0].Kind = %q, want %q", got, want)
	}
	if got, want := snapshot.DetailItems[1].Kind, model.DetailItemCommentary; got != want {
		t.Fatalf("DetailItems[1].Kind = %q, want %q", got, want)
	}
	if got, want := snapshot.DetailItems[2].Kind, model.DetailItemTool; got != want {
		t.Fatalf("DetailItems[2].Kind = %q, want %q", got, want)
	}
	if got, want := snapshot.DetailItems[2].CommentaryIndex, 1; got != want {
		t.Fatalf("tool CommentaryIndex = %d, want %d", got, want)
	}
	if got, want := snapshot.DetailItems[3].Kind, model.DetailItemOutput; got != want {
		t.Fatalf("DetailItems[3].Kind = %q, want %q", got, want)
	}
	if got, want := snapshot.DetailItems[3].CommentaryIndex, 1; got != want {
		t.Fatalf("output CommentaryIndex = %d, want %d", got, want)
	}
	if got, want := snapshot.DetailItems[4].CommentaryIndex, 2; got != want {
		t.Fatalf("second commentary index = %d, want %d", got, want)
	}
}

func TestSnapshotFromThreadReadKeepsToolOnlyTurnDetailsWithoutCommentary(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotFromThreadRead(map[string]any{
		"id":     "thread-tool-only",
		"name":   "Tool only",
		"cwd":    "/Users/example/project",
		"status": "completed",
		"turns": []any{
			map[string]any{
				"id":     "turn-tool-only",
				"status": "completed",
				"items": []any{
					map[string]any{"id": "user-1", "type": "userMessage", "content": []any{
						map[string]any{"type": "text", "text": "Run sleep 10.\n"},
					}},
					map[string]any{"id": "cmd-sleep", "type": "commandExecution", "command": "sleep 10", "status": "completed"},
					map[string]any{"id": "agent-final", "type": "agentMessage", "phase": "final_answer", "text": "Done."},
				},
			},
		},
	})

	if len(snapshot.DetailItems) != 3 {
		t.Fatalf("len(DetailItems) = %d, want 3: %#v", len(snapshot.DetailItems), snapshot.DetailItems)
	}
	if got, want := snapshot.DetailItems[1].Kind, model.DetailItemTool; got != want {
		t.Fatalf("DetailItems[1].Kind = %q, want %q", got, want)
	}
	if got, want := snapshot.DetailItems[1].Label, "sleep 10"; got != want {
		t.Fatalf("tool label = %q, want %q", got, want)
	}
	if got := snapshot.DetailItems[1].CommentaryIndex; got != 0 {
		t.Fatalf("tool CommentaryIndex = %d, want orphan index 0", got)
	}
	if got, want := snapshot.LatestToolLabel, "sleep 10"; got != want {
		t.Fatalf("LatestToolLabel = %q, want %q", got, want)
	}
	if got, want := snapshot.LatestToolStatus, "completed"; got != want {
		t.Fatalf("LatestToolStatus = %q, want %q", got, want)
	}
}

func TestSnapshotFromThreadReadLabelsDynamicToolWithoutEmptyArguments(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotFromThreadRead(map[string]any{
		"id":     "thread-dynamic-tool",
		"name":   "Dynamic tool",
		"cwd":    "/Users/example/project",
		"status": "inProgress",
		"turns": []any{
			map[string]any{
				"id":     "turn-dynamic-tool",
				"status": "inProgress",
				"items": []any{
					map[string]any{"id": "user-1", "type": "userMessage", "content": []any{
						map[string]any{"type": "text", "text": "Read terminal."},
					}},
					map[string]any{
						"id":        "call-read-terminal",
						"type":      "dynamicToolCall",
						"tool":      "read_thread_terminal",
						"arguments": map[string]any{},
						"status":    "inProgress",
					},
				},
			},
		},
	})

	if got, want := snapshot.LatestToolLabel, "read_thread_terminal"; got != want {
		t.Fatalf("LatestToolLabel = %q, want %q", got, want)
	}
	if strings.Contains(snapshot.LatestToolLabel, "{}") || strings.Contains(snapshot.LatestProgressText, "{}") {
		t.Fatalf("snapshot leaked empty arguments as text: label=%q progress=%q", snapshot.LatestToolLabel, snapshot.LatestProgressText)
	}
	if len(snapshot.DetailItems) != 2 || snapshot.DetailItems[1].Label != "read_thread_terminal" {
		t.Fatalf("DetailItems = %#v, want dynamic tool label", snapshot.DetailItems)
	}
}

func TestSnapshotFromThreadReadFallsBackToLegacyFinalWithoutPhase(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotFromThreadRead(map[string]any{
		"id":     "thread-1",
		"name":   "Observer smoke",
		"cwd":    `C:\Users\you\Projects\Codex`,
		"status": "inProgress",
		"turns": []any{
			map[string]any{
				"id":     "turn-2",
				"status": "completed",
				"items": []any{
					map[string]any{
						"id":   "agent-1",
						"type": "agentMessage",
						"text": "Legacy final answer.",
					},
				},
			},
		},
	})

	if got, want := snapshot.LatestFinalText, "Legacy final answer."; got != want {
		t.Fatalf("LatestFinalText = %q, want %q", got, want)
	}
	if snapshot.LatestFinalFP == "" {
		t.Fatal("LatestFinalFP must not be empty for legacy payloads")
	}
}

func TestSnapshotFromThreadReadBuildsSyntheticPlanPromptFromActiveFlagsObject(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotFromThreadRead(map[string]any{
		"id":     "thread-plan",
		"name":   "Plan prompt",
		"cwd":    `C:\Users\you\Projects\Codex`,
		"status": map[string]any{"type": "active", "activeFlags": []any{"waitingOnUserInput"}},
		"turns": []any{
			map[string]any{
				"id":     "turn-plan",
				"status": map[string]any{"type": "active", "activeFlags": []any{"waitingOnUserInput"}},
				"items": []any{
					map[string]any{"id": "user-1", "type": "userMessage", "content": []any{
						map[string]any{"type": "text", "text": "Implement plan mode."},
					}},
					map[string]any{
						"id":          "plan-1",
						"type":        "plan",
						"text":        "Which route should I use?",
						"suggestions": []any{"Continue", map[string]any{"label": "Revise"}},
					},
				},
			},
		},
	})

	if !snapshot.WaitingOnReply {
		t.Fatal("WaitingOnReply = false, want true")
	}
	if got, want := snapshot.Thread.ActiveTurnID, "turn-plan"; got != want {
		t.Fatalf("Thread.ActiveTurnID = %q, want %q", got, want)
	}
	if snapshot.PlanPrompt == nil {
		t.Fatal("PlanPrompt = nil, want synthetic prompt")
	}
	if got, want := snapshot.PlanPrompt.Source, model.PromptSourceSyntheticPoll; got != want {
		t.Fatalf("PlanPrompt.Source = %q, want %q", got, want)
	}
	if got, want := snapshot.PlanPrompt.TurnID, "turn-plan"; got != want {
		t.Fatalf("PlanPrompt.TurnID = %q, want %q", got, want)
	}
	if got, want := snapshot.PlanPrompt.Question, "Which route should I use?"; got != want {
		t.Fatalf("PlanPrompt.Question = %q, want %q", got, want)
	}
	if got, want := snapshot.PlanPrompt.Options, []string{"Continue", "Revise"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("PlanPrompt.Options = %#v, want %#v", got, want)
	}
	if got, want := snapshot.DetailItems[1].Kind, model.DetailItemPlan; got != want {
		t.Fatalf("DetailItems[1].Kind = %q, want %q", got, want)
	}
	if got, want := snapshot.LatestAgentMessageEntries[0].Phase, "plan"; got != want {
		t.Fatalf("LatestAgentMessageEntries[0].Phase = %q, want %q", got, want)
	}
	if snapshot.LatestFinalText != "" {
		t.Fatalf("LatestFinalText = %q, want empty for plan item", snapshot.LatestFinalText)
	}
}

func TestSnapshotFromThreadReadTreatsWaitingOnInputAsPlanPrompt(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotFromThreadRead(map[string]any{
		"id":     "thread-plan-waiting-on-input",
		"name":   "Plan prompt waitingOnInput",
		"cwd":    `C:\Users\you\Projects\Codex`,
		"status": "waitingOnInput",
		"turns": []any{
			map[string]any{
				"id":     "turn-plan",
				"status": "waitingOnInput",
				"items": []any{
					map[string]any{"id": "agent-1", "type": "agentMessage", "phase": "commentary", "text": "Need your answer?"},
				},
			},
		},
	})

	if !snapshot.WaitingOnReply {
		t.Fatal("WaitingOnReply = false, want true for waitingOnInput")
	}
	if snapshot.PlanPrompt == nil {
		t.Fatal("PlanPrompt = nil, want prompt for waitingOnInput")
	}
	if got, want := snapshot.PlanPrompt.Question, "Need your answer?"; got != want {
		t.Fatalf("PlanPrompt.Question = %q, want %q", got, want)
	}
	if len(snapshot.PlanPrompt.Options) != 0 {
		t.Fatalf("PlanPrompt.Options = %#v, want none", snapshot.PlanPrompt.Options)
	}
}

func TestSnapshotFromThreadReadDoesNotUseStalePreviewForSyntheticPlanPrompt(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotFromThreadRead(map[string]any{
		"id":      "thread-plan-stale-preview",
		"name":    "Plan prompt stale preview",
		"cwd":     `C:\Users\you\Projects\Codex`,
		"preview": "Old completed prompt from a previous turn.",
		"status":  "waitingOnInput",
		"turns": []any{
			map[string]any{
				"id":     "turn-old",
				"status": "completed",
				"items": []any{
					map[string]any{"id": "user-old", "type": "userMessage", "text": "Old completed prompt from a previous turn."},
				},
			},
			map[string]any{
				"id":     "turn-plan",
				"status": "waitingOnInput",
				"items": []any{
					map[string]any{"id": "user-new", "type": "userMessage", "text": "Start a plan flow without a structured question yet."},
				},
			},
		},
	})

	if snapshot.PlanPrompt == nil {
		t.Fatal("PlanPrompt = nil, want fallback prompt")
	}
	if got, want := snapshot.PlanPrompt.Question, "Input required."; got != want {
		t.Fatalf("PlanPrompt.Question = %q, want %q", got, want)
	}
}

func TestSnapshotFromThreadReadSummaryUsesLatestTurnOnly(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotFromThreadRead(map[string]any{
		"id":     "thread-1",
		"name":   "Observer smoke",
		"cwd":    `C:\Users\you\Projects\Codex`,
		"status": "completed",
		"turns": []any{
			map[string]any{
				"id":     "turn-1",
				"status": "completed",
				"items": []any{
					map[string]any{
						"id":    "agent-old",
						"type":  "agentMessage",
						"phase": "final_answer",
						"text":  "OLD",
					},
				},
			},
			map[string]any{
				"id":     "turn-2",
				"status": "completed",
				"items": []any{
					map[string]any{
						"id":    "agent-new",
						"type":  "agentMessage",
						"phase": "final_answer",
						"text":  "NEW",
					},
				},
			},
		},
	})

	if len(snapshot.LatestAgentMessageEntries) != 1 {
		t.Fatalf("len(LatestAgentMessageEntries) = %d, want 1", len(snapshot.LatestAgentMessageEntries))
	}
	if got, want := snapshot.LatestAgentMessageEntries[0].Text, "NEW"; got != want {
		t.Fatalf("LatestAgentMessageEntries[0].Text = %q, want %q", got, want)
	}
	if got, want := snapshot.LatestFinalText, "NEW"; got != want {
		t.Fatalf("LatestFinalText = %q, want %q", got, want)
	}
}

func TestNormalizeLiveNotificationIgnoresCommentaryAgentMessageFinalClassification(t *testing.T) {
	t.Parallel()

	thread := model.Thread{
		ID:          "thread-1",
		Title:       "Observer smoke",
		ProjectName: "Codex",
	}

	events := NormalizeLiveNotification(Event{
		Channel: "notification",
		Method:  "item/completed",
		Params: map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-9",
			"item": map[string]any{
				"id":    "agent-2",
				"type":  "agentMessage",
				"phase": "commentary",
				"text":  "Checking.",
			},
		},
	}, thread)

	if len(events) != 0 {
		t.Fatalf("events = %#v, want no observer events", events)
	}
}

func TestToolSnapshotFromLiveNotificationMapsRunningCommand(t *testing.T) {
	t.Parallel()

	thread := model.Thread{
		ID:          "thread-1",
		Title:       "Observer smoke",
		ProjectName: "Codex",
	}
	snapshot, ok := ToolSnapshotFromLiveNotification(Event{
		Channel: "notification",
		Method:  "item/started",
		Params: map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-9",
			"item": map[string]any{
				"id":      "cmd-slow",
				"type":    "commandExecution",
				"command": "sleep 20; printf 'slow-command-done\\n'",
				"status":  "running",
			},
		},
	}, thread)
	if !ok {
		t.Fatal("ToolSnapshotFromLiveNotification returned ok=false")
	}
	if got, want := snapshot.Thread.ID, "thread-1"; got != want {
		t.Fatalf("Thread.ID = %q, want %q", got, want)
	}
	if got, want := snapshot.Thread.ActiveTurnID, "turn-9"; got != want {
		t.Fatalf("Thread.ActiveTurnID = %q, want %q", got, want)
	}
	if got, want := snapshot.LatestTurnStatus, "inProgress"; got != want {
		t.Fatalf("LatestTurnStatus = %q, want %q", got, want)
	}
	if got, want := snapshot.LatestToolLabel, "sleep 20; printf 'slow-command-done\\n'"; got != want {
		t.Fatalf("LatestToolLabel = %q, want %q", got, want)
	}
	if got, want := snapshot.LatestToolStatus, "running"; got != want {
		t.Fatalf("LatestToolStatus = %q, want %q", got, want)
	}
	if got := strings.TrimSpace(snapshot.LatestToolOutput); got != "" {
		t.Fatalf("LatestToolOutput = %q, want empty until command writes output", got)
	}
	if len(snapshot.DetailItems) != 1 || snapshot.DetailItems[0].Kind != model.DetailItemTool {
		t.Fatalf("DetailItems = %#v, want one tool item", snapshot.DetailItems)
	}
}

func TestNormalizeAppServerLiveEventMapsToolLifecycle(t *testing.T) {
	t.Parallel()

	thread := model.Thread{ID: "thread-1", Title: "Observer smoke", ProjectName: "Codex"}
	live, ok := NormalizeAppServerLiveEvent(Event{
		Channel: "notification",
		Method:  "item/completed",
		Params: map[string]any{
			"threadId": "thread-1",
			"turn":     map[string]any{"id": "turn-9"},
			"item": map[string]any{
				"id":               "cmd-done",
				"type":             "commandExecution",
				"command":          "printf 'done\\n'",
				"status":           "completed",
				"aggregatedOutput": "done\n",
			},
		},
	}, thread)
	if !ok {
		t.Fatal("NormalizeAppServerLiveEvent returned ok=false")
	}
	if got, want := live.Kind, LiveEventToolCompleted; got != want {
		t.Fatalf("Kind = %q, want %q", got, want)
	}
	if got, want := live.TurnID, "turn-9"; got != want {
		t.Fatalf("TurnID = %q, want %q", got, want)
	}
	if got, want := live.Label, "printf 'done\\n'"; got != want {
		t.Fatalf("Label = %q, want %q", got, want)
	}
	if got, want := live.Output, "done\n"; got != want {
		t.Fatalf("Output = %q, want %q", got, want)
	}
	snapshot, ok := live.ToolSnapshot(thread)
	if !ok {
		t.Fatal("ToolSnapshot returned ok=false")
	}
	if got, want := snapshot.LatestToolStatus, "completed"; got != want {
		t.Fatalf("LatestToolStatus = %q, want %q", got, want)
	}
	if len(snapshot.DetailItems) != 2 || snapshot.DetailItems[1].Kind != model.DetailItemOutput {
		t.Fatalf("DetailItems = %#v, want tool and output", snapshot.DetailItems)
	}
}

func TestLiveToolSnapshotMarksCurrentOnlyForStartedOrUpdatedWithTurn(t *testing.T) {
	t.Parallel()

	thread := model.Thread{ID: "thread-current"}
	started, ok := NormalizeAppServerLiveEvent(Event{
		Channel: "notification",
		Method:  "item/started",
		Params: map[string]any{
			"threadId": "thread-current",
			"turnId":   "turn-current",
			"item": map[string]any{
				"id":      "cmd-current",
				"type":    "commandExecution",
				"command": "sleep 20",
				"status":  "running",
			},
		},
	}, thread)
	if !ok {
		t.Fatal("NormalizeAppServerLiveEvent(item/started) returned ok=false")
	}
	startedSnapshot, ok := started.ToolSnapshot(thread)
	if !ok {
		t.Fatal("ToolSnapshot(started) returned ok=false")
	}
	if !startedSnapshot.LatestToolLiveCurrent {
		t.Fatal("LatestToolLiveCurrent = false, want true for live started event with turn id")
	}

	completed, ok := NormalizeAppServerLiveEvent(Event{
		Channel: "notification",
		Method:  "item/completed",
		Params: map[string]any{
			"threadId": "thread-current",
			"turnId":   "turn-current",
			"item": map[string]any{
				"id":      "cmd-current",
				"type":    "commandExecution",
				"command": "sleep 20",
				"status":  "completed",
			},
		},
	}, thread)
	if !ok {
		t.Fatal("NormalizeAppServerLiveEvent(item/completed) returned ok=false")
	}
	completedSnapshot, ok := completed.ToolSnapshot(thread)
	if !ok {
		t.Fatal("ToolSnapshot(completed) returned ok=false")
	}
	if completedSnapshot.LatestToolLiveCurrent {
		t.Fatal("LatestToolLiveCurrent = true, want false for completed event")
	}

	withoutTurn, ok := NormalizeAppServerLiveEvent(Event{
		Channel: "notification",
		Method:  "item/started",
		Params: map[string]any{
			"threadId": "thread-current",
			"item": map[string]any{
				"id":      "cmd-current",
				"type":    "commandExecution",
				"command": "sleep 20",
				"status":  "running",
			},
		},
	}, thread)
	if !ok {
		t.Fatal("NormalizeAppServerLiveEvent(item/started without turn) returned ok=false")
	}
	withoutTurnSnapshot, ok := withoutTurn.ToolSnapshot(thread)
	if !ok {
		t.Fatal("ToolSnapshot(withoutTurn) returned ok=false")
	}
	if withoutTurnSnapshot.LatestToolLiveCurrent {
		t.Fatal("LatestToolLiveCurrent = true, want false when live event has no turn id")
	}
}

func TestNormalizeAppServerLiveEventMapsTurnAndStatus(t *testing.T) {
	t.Parallel()

	thread := model.Thread{ID: "thread-1", ActiveTurnID: "turn-prev"}
	started, ok := NormalizeAppServerLiveEvent(Event{
		Channel: "notification",
		Method:  "turn/started",
		Params: map[string]any{
			"threadId": "thread-1",
			"turn":     map[string]any{"id": "turn-10"},
		},
	}, thread)
	if !ok {
		t.Fatal("NormalizeAppServerLiveEvent(turn/started) returned ok=false")
	}
	if got, want := started.Kind, LiveEventTurnStarted; got != want {
		t.Fatalf("Kind = %q, want %q", got, want)
	}
	if got, want := started.TurnID, "turn-10"; got != want {
		t.Fatalf("TurnID = %q, want %q", got, want)
	}
	if got, want := started.TurnStatus, "inProgress"; got != want {
		t.Fatalf("TurnStatus = %q, want %q", got, want)
	}

	completed, ok := NormalizeAppServerLiveEvent(Event{
		Channel: "notification",
		Method:  "turn/completed",
		Params: map[string]any{
			"threadId": "thread-1",
			"turn": map[string]any{
				"id":     "turn-10",
				"status": "completed",
			},
		},
	}, thread)
	if !ok {
		t.Fatal("NormalizeAppServerLiveEvent(turn/completed) returned ok=false")
	}
	if got, want := completed.Kind, LiveEventTurnCompleted; got != want {
		t.Fatalf("Kind = %q, want %q", got, want)
	}
	if got, want := completed.TurnStatus, "completed"; got != want {
		t.Fatalf("TurnStatus = %q, want %q", got, want)
	}
}

func TestNormalizeAppServerLiveEventMapsLegacyExecEvent(t *testing.T) {
	t.Parallel()

	live, ok := NormalizeAppServerLiveEvent(Event{
		Channel: "notification",
		Method:  "codex/event/exec_command_begin",
		Params: map[string]any{
			"msg": map[string]any{
				"type":      "exec_command_begin",
				"turn_id":   "turn-legacy",
				"thread_id": "thread-legacy",
				"call_id":   "call-legacy",
				"command":   []any{"/bin/zsh", "-lc", "pwd"},
			},
		},
	}, model.Thread{})
	if !ok {
		t.Fatal("NormalizeAppServerLiveEvent(legacy) returned ok=false")
	}
	if got, want := live.Kind, LiveEventToolStarted; got != want {
		t.Fatalf("Kind = %q, want %q", got, want)
	}
	if got, want := live.Label, "/bin/zsh -lc pwd"; got != want {
		t.Fatalf("Label = %q, want %q", got, want)
	}
}
