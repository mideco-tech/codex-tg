package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-telegram-remote-go/internal/appserver"
	"codex-telegram-remote-go/internal/model"
)

func TestLatestActiveToolFromSessionLinesDetectsRunningShellCommand(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"timestamp":"2026-04-28T08:47:00Z","type":"turn_context","payload":{"turn_id":"turn-active"}}`,
		`{"timestamp":"2026-04-28T08:47:10Z","type":"response_item","payload":{"type":"function_call","call_id":"call_sleep","name":"shell_command","arguments":"{\"command\":\"Start-Sleep -Seconds 1800\\n$ErrorActionPreference='Stop'\",\"timeout_ms\":1860000}"}}`,
	}

	overlay, ok := latestActiveToolFromSessionLines(lines, "")
	if !ok {
		t.Fatal("overlay not detected")
	}
	if got, want := overlay.CallID, "call_sleep"; got != want {
		t.Fatalf("CallID = %q, want %q", got, want)
	}
	if got, want := overlay.TurnID, "turn-active"; got != want {
		t.Fatalf("TurnID = %q, want %q", got, want)
	}
	if got := overlay.Command; !strings.Contains(got, "Start-Sleep -Seconds 1800") {
		t.Fatalf("Command = %q, want Start-Sleep command", got)
	}
	if got, want := overlay.Status, "running"; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
}

func TestLatestActiveToolFromSessionLinesIgnoresCompletedShellCommand(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"timestamp":"2026-04-28T08:47:00Z","type":"turn_context","payload":{"turn_id":"turn-active"}}`,
		`{"timestamp":"2026-04-28T08:47:10Z","type":"response_item","payload":{"type":"function_call","call_id":"call_done","name":"shell_command","arguments":"{\"command\":\"Write-Output done\"}"}}`,
		`{"timestamp":"2026-04-28T08:47:11Z","type":"event_msg","payload":{"type":"exec_command_end","call_id":"call_done","status":"completed","aggregated_output":"done\n"}}`,
	}

	if overlay, ok := latestActiveToolFromSessionLines(lines, "turn-active"); ok {
		t.Fatalf("overlay = %#v, want no active tool", overlay)
	}
}

func TestApplySessionTailOverlayReplacesOlderThreadReadTool(t *testing.T) {
	t.Parallel()

	sessionPath := writeSessionTailFixture(t, []string{
		`{"timestamp":"2026-04-28T08:47:00Z","type":"turn_context","payload":{"turn_id":"turn-active"}}`,
		`{"timestamp":"2026-04-28T08:47:10Z","type":"response_item","payload":{"type":"function_call","call_id":"call_sleep","name":"shell_command","arguments":"{\"command\":\"Start-Sleep -Seconds 1800\",\"timeout_ms\":1860000}"}}`,
	})
	thread := model.Thread{
		ID:          "thread-session-tail",
		Title:       "Session tail",
		ProjectName: "Codex",
		Raw:         rawThreadPath(t, sessionPath),
	}
	snapshot := appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-old",
		LatestTurnStatus: "completed",
		LatestToolID:     "old-tool",
		LatestToolKind:   "commandExecution",
		LatestToolLabel:  "curl.exe http://gitlab/pipeline",
		LatestToolStatus: "completed",
		LatestToolOutput: "pipeline created",
		LatestToolFP:     "old-fp",
	}

	if !applySessionTailToolOverlay(thread, &snapshot) {
		t.Fatal("applySessionTailToolOverlay = false, want true")
	}
	if got := snapshot.LatestToolLabel; !strings.Contains(got, "Start-Sleep -Seconds 1800") {
		t.Fatalf("LatestToolLabel = %q, want Start-Sleep", got)
	}
	if got, want := snapshot.LatestToolStatus, "running"; got != want {
		t.Fatalf("LatestToolStatus = %q, want %q", got, want)
	}
	if got, want := snapshot.LatestTurnStatus, "inProgress"; got != want {
		t.Fatalf("LatestTurnStatus = %q, want %q", got, want)
	}
	if !snapshot.SessionTailActiveTool {
		t.Fatal("SessionTailActiveTool = false, want true")
	}
	previous := &model.ThreadSnapshotState{
		LastSeenTurnID:     "turn-active",
		LastSeenTurnStatus: "interrupted",
		LastProgressFP:     "old-fp",
	}
	if !snapshotHasPassiveChange(previous, &snapshot) {
		t.Fatal("snapshotHasPassiveChange = false, want true for active session-tail tool over stale terminal snapshot")
	}
}

func TestPollTrackedUsesSessionTailOverlayForForeignActiveTool(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	if err := service.store.SetGlobalObserverTarget(ctx, 123456789, 0, true); err != nil {
		t.Fatalf("SetGlobalObserverTarget failed: %v", err)
	}
	sessionPath := writeSessionTailFixture(t, []string{
		`{"timestamp":"2026-04-28T08:47:00Z","type":"turn_context","payload":{"turn_id":"turn-active"}}`,
		`{"timestamp":"2026-04-28T08:47:10Z","type":"response_item","payload":{"type":"function_call","call_id":"call_sleep","name":"shell_command","arguments":"{\"command\":\"Start-Sleep -Seconds 1800\",\"timeout_ms\":1860000}"}}`,
	})
	threadID := "thread-foreign-active"
	thread := model.Thread{
		ID:          threadID,
		Title:       "Foreign active",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
		Status:      "completed",
		Raw:         rawThreadPath(t, sessionPath),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	service.poll = &stubSession{
		threadReads: map[string]map[string]any{
			threadID: {
				"id":        threadID,
				"name":      thread.Title,
				"cwd":       thread.CWD,
				"status":    "completed",
				"path":      sessionPath,
				"updatedAt": float64(thread.UpdatedAt),
				"turns": []any{
					map[string]any{
						"id":     "turn-old",
						"status": "completed",
						"items": []any{
							map[string]any{
								"id":               "old-tool",
								"type":             "commandExecution",
								"command":          "curl.exe http://gitlab/pipeline",
								"status":           "completed",
								"aggregatedOutput": "pipeline created\n",
							},
						},
					},
				},
			},
		},
	}
	service.pollConnected = true

	service.pollTracked(ctx)

	snapshot, err := service.store.GetSnapshot(ctx, threadID)
	if err != nil {
		t.Fatalf("GetSnapshot failed: %v", err)
	}
	if snapshot == nil {
		t.Fatal("snapshot = nil")
	}
	var compact appserver.ThreadReadSnapshot
	if err := json.Unmarshal(snapshot.CompactJSON, &compact); err != nil {
		t.Fatalf("decode compact snapshot failed: %v", err)
	}
	if got := compact.LatestToolLabel; !strings.Contains(got, "Start-Sleep -Seconds 1800") {
		t.Fatalf("LatestToolLabel = %q, want active Start-Sleep", got)
	}
	if got, want := compact.LatestToolStatus, "running"; got != want {
		t.Fatalf("LatestToolStatus = %q, want %q", got, want)
	}
	if got, want := compact.LatestTurnStatus, "inProgress"; got != want {
		t.Fatalf("LatestTurnStatus = %q, want %q", got, want)
	}
	if snapshot.NextPollAfter == "" {
		t.Fatal("NextPollAfter is empty, want hot tracking")
	}
}

func TestCurrentPanelThreadIDsKeepsTerminalPanelWithActiveSessionTailTool(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	sessionPath := writeSessionTailFixture(t, []string{
		`{"timestamp":"2026-04-28T08:47:00Z","type":"turn_context","payload":{"turn_id":"turn-active"}}`,
		`{"timestamp":"2026-04-28T08:47:10Z","type":"response_item","payload":{"type":"function_call","call_id":"call_sleep","name":"shell_command","arguments":"{\"command\":\"Start-Sleep -Seconds 1800\"}"}}`,
	})
	thread := model.Thread{
		ID:          "thread-current-panel-active-tail",
		Title:       "Current panel active tail",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Add(-2 * time.Hour).Unix(),
		Status:      "interrupted",
		Raw:         rawThreadPath(t, sessionPath),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:        123456789,
		TopicID:       0,
		ProjectName:   thread.ProjectName,
		ThreadID:      thread.ID,
		SourceMode:    model.PanelSourceGlobalObserver,
		CurrentTurnID: "turn-active",
		Status:        "interrupted",
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}

	ids := service.currentPanelThreadIDs(ctx)
	if len(ids) != 1 || ids[0] != thread.ID {
		t.Fatalf("currentPanelThreadIDs = %#v, want %q", ids, thread.ID)
	}
}

func writeSessionTailFixture(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rollout-2026-04-28T08-47-00-thread.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write session fixture failed: %v", err)
	}
	return path
}

func rawThreadPath(t *testing.T, path string) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"path": path})
	if err != nil {
		t.Fatalf("marshal raw path failed: %v", err)
	}
	return payload
}
