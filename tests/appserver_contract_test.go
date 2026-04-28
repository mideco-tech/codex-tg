package tests

import (
	"testing"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestThreadFromPayloadMapsProjectAndActiveTurn(t *testing.T) {
	t.Parallel()

	thread := appserver.ThreadFromPayload(map[string]any{
		"id":        "019db243-0fc6-7fe3-a0ca-7a54a2ca9c40",
		"name":      "Проверь версию Node.js",
		"cwd":       `C:\Users\you\Documents\Codex`,
		"status":    "notLoaded",
		"updatedAt": float64(123),
		"turns": []any{
			map[string]any{
				"id":     "turn-1",
				"status": "inProgress",
				"items": []any{
					map[string]any{"type": "userMessage", "text": "Проверь какая у меня сейчас стоит node"},
				},
			},
		},
	})

	if got, want := thread.ProjectName, "Shared/General"; got != want {
		t.Fatalf("ProjectName = %q, want %q", got, want)
	}
	if got, want := thread.ActiveTurnID, "turn-1"; got != want {
		t.Fatalf("ActiveTurnID = %q, want %q", got, want)
	}
	if got, want := thread.LastPreview, "Проверь какая у меня сейчас стоит node"; got != want {
		t.Fatalf("LastPreview = %q, want %q", got, want)
	}
}

func TestSnapshotFromThreadReadExtractsLatestProgressAndFinal(t *testing.T) {
	t.Parallel()

	snapshot := appserver.SnapshotFromThreadRead(map[string]any{
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
						"id":               "cmd-1",
						"type":             "commandExecution",
						"status":           "completed",
						"command":          "go test ./tests",
						"aggregatedOutput": "ok\t./tests\t0.123s\n",
					},
					map[string]any{
						"id":   "agent-1",
						"type": "agentMessage",
						"text": "Node.js version is 22.0.0.",
					},
				},
			},
		},
	})

	if got, want := snapshot.LatestTurnID, "turn-2"; got != want {
		t.Fatalf("LatestTurnID = %q, want %q", got, want)
	}
	if snapshot.LatestProgressFP == "" {
		t.Fatal("LatestProgressFP must not be empty")
	}
	if snapshot.LatestFinalFP == "" {
		t.Fatal("LatestFinalFP must not be empty")
	}
	if got, want := snapshot.LatestFinalText, "Node.js version is 22.0.0."; got != want {
		t.Fatalf("LatestFinalText = %q, want %q", got, want)
	}
}

func TestDiffSnapshotEmitsTurnProgressFinalAndCompletion(t *testing.T) {
	t.Parallel()

	previous := &model.ThreadSnapshotState{
		LastSeenTurnID:     "turn-1",
		LastSeenTurnStatus: "inProgress",
		LastProgressFP:     "old-progress",
		LastFinalFP:        "old-final",
	}
	current := appserver.ThreadReadSnapshot{
		Thread: model.Thread{
			ID:          "thread-1",
			Title:       "Observer smoke",
			ProjectName: "Codex",
		},
		LatestTurnID:       "turn-2",
		LatestTurnStatus:   "completed",
		LatestProgressFP:   "progress-2",
		LatestProgressText: "completed: go test ./tests",
		LatestFinalFP:      "final-2",
		LatestFinalText:    "All tests passed.",
	}

	events := appserver.DiffSnapshot(previous, current)
	if len(events) != 4 {
		t.Fatalf("len(events) = %d, want 4", len(events))
	}
	if events[0].Kind != "turn_started" {
		t.Fatalf("events[0].Kind = %q, want turn_started", events[0].Kind)
	}
	if events[1].Kind != "tool_activity" {
		t.Fatalf("events[1].Kind = %q, want tool_activity", events[1].Kind)
	}
	if events[2].Kind != "final_answer" {
		t.Fatalf("events[2].Kind = %q, want final_answer", events[2].Kind)
	}
	if events[3].Kind != "turn_completed" {
		t.Fatalf("events[3].Kind = %q, want turn_completed", events[3].Kind)
	}
}

func TestNormalizeLiveNotificationMapsObserverEvents(t *testing.T) {
	t.Parallel()

	thread := model.Thread{
		ID:          "thread-1",
		Title:       "Observer smoke",
		ProjectName: "Codex",
	}

	completed := appserver.NormalizeLiveNotification(appserver.Event{
		Channel: "notification",
		Method:  "item/completed",
		Params: map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-9",
			"item": map[string]any{
				"id":   "agent-1",
				"type": "agentMessage",
				"text": "Finished.",
			},
		},
	}, thread)
	if len(completed) != 1 || completed[0].Kind != "final_answer" {
		t.Fatalf("completed = %#v, want one final_answer event", completed)
	}

	waiting := appserver.NormalizeLiveNotification(appserver.Event{
		Channel: "notification",
		Method:  "thread/status/changed",
		Params: map[string]any{
			"threadId": "thread-1",
			"status":   "waitingOnUserInput",
		},
	}, thread)
	if len(waiting) != 1 || !waiting[0].NeedsReply {
		t.Fatalf("waiting = %#v, want one NeedsReply event", waiting)
	}
}

func TestNormalizeLiveNotificationKeepsCommentaryOutOfFinalAnswer(t *testing.T) {
	t.Parallel()

	events := appserver.NormalizeLiveNotification(appserver.Event{
		Channel: "notification",
		Method:  "item/completed",
		Params: map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-9",
			"item": map[string]any{
				"id":    "item-1",
				"type":  "agentMessage",
				"text":  "Still thinking.",
				"phase": "commentary",
			},
		},
	}, model.Thread{
		ID:          "thread-1",
		Title:       "Observer smoke",
		ProjectName: "Codex",
	})

	if len(events) > 1 {
		t.Fatalf("events = %#v, want at most one commentary event", events)
	}
	if len(events) == 1 && events[0].Kind == "final_answer" {
		t.Fatalf("commentary event was labeled final_answer: %#v", events[0])
	}
}

func TestPendingApprovalFromServerRequestRecognizesApprovalAndInput(t *testing.T) {
	t.Parallel()

	approval, ok := appserver.PendingApprovalFromServerRequest(appserver.Event{
		Channel: "server_request",
		Method:  "commandExecution/requestApproval",
		ID:      42,
		Params: map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"itemId":   "cmd-1",
			"question": "Approve command execution?",
		},
	})
	if !ok {
		t.Fatal("approval request was not recognized")
	}
	if approval.PromptKind != "approval" {
		t.Fatalf("PromptKind = %q, want approval", approval.PromptKind)
	}

	input, ok := appserver.PendingApprovalFromServerRequest(appserver.Event{
		Channel: "server_request",
		Method:  "item/tool/requestUserInput",
		ID:      "req-2",
		Params: map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-2",
			"itemId":   "input-1",
			"questions": []any{
				map[string]any{
					"id":       "route",
					"header":   "Route",
					"question": "Need more input",
					"options": []any{
						map[string]any{"label": "Continue", "description": "Proceed."},
					},
				},
			},
		},
	})
	if !ok {
		t.Fatal("user input request was not recognized")
	}
	if input.PromptKind != "user_input" {
		t.Fatalf("PromptKind = %q, want user_input", input.PromptKind)
	}
	if input.Question != "Route: Need more input" {
		t.Fatalf("Question = %q, want schema question text", input.Question)
	}
}
