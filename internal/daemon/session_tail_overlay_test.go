package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestPollTrackedIgnoresStaleSessionTailTool(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	if err := service.store.SetGlobalObserverTarget(ctx, 123456789, 0, true); err != nil {
		t.Fatalf("SetGlobalObserverTarget failed: %v", err)
	}
	sessionPath := writeSessionTailFixture(t, []string{
		`{"timestamp":"2026-04-28T08:47:00Z","type":"turn_context","payload":{"turn_id":"turn-stale"}}`,
		`{"timestamp":"2026-04-28T08:47:10Z","type":"response_item","payload":{"type":"function_call","call_id":"call_sleep","name":"shell_command","arguments":"{\"command\":\"Start-Sleep -Seconds 1800\",\"timeout_ms\":1860000}"}}`,
	})
	threadID := "thread-foreign-active"
	thread := model.Thread{
		ID:          threadID,
		Title:       "Foreign active",
		ProjectName: "Codex",
		CWD:         `/Users/example/Projects/Codex`,
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
						"items":  []any{},
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
	if strings.Contains(string(snapshot.CompactJSON), "Start-Sleep -Seconds 1800") {
		t.Fatalf("compact snapshot leaked stale session-tail command: %s", snapshot.CompactJSON)
	}
	if strings.Contains(sender.allText(), "Start-Sleep -Seconds 1800") {
		t.Fatalf("telegram messages leaked stale session-tail command: %s", sender.allText())
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

func (s *recordingSender) allText() string {
	if s == nil {
		return ""
	}
	parts := make([]string, 0, len(s.messages)+len(s.edits))
	for _, message := range s.messages {
		parts = append(parts, message.text)
	}
	for _, edit := range s.edits {
		parts = append(parts, edit.text)
	}
	return strings.Join(parts, "\n")
}
