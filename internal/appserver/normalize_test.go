package appserver

import (
	"testing"
	"time"

	"codex-telegram-remote-go/internal/model"
)

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
