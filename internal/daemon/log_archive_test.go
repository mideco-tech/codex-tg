package daemon

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/config"
	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestBuildThreadLogArchiveUsesPrimaryPathAndIncludesHumanAndRawLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	cfgPaths := config.Paths{
		Home:    filepath.Join(home, ".ctr-go"),
		DataDir: filepath.Join(home, ".ctr-go", "data"),
		LogDir:  filepath.Join(home, ".ctr-go", "logs"),
		DBPath:  filepath.Join(home, ".ctr-go", "data", "state.sqlite"),
	}
	if err := cfgPaths.Ensure(); err != nil {
		t.Fatalf("Ensure() failed: %v", err)
	}

	threadID := "019db243-0fc6-7fe3-a0ca-7a54a2ca9c40"
	primaryPath := filepath.Join(home, ".codex", "sessions", "2026", "04", "22", "rollout-2026-04-22T01-57-12-"+threadID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(primaryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(primary) failed: %v", err)
	}
	writeTranscriptFixture(t, primaryPath, threadID, fixtureTranscriptOptions{
		CWD:          `C:\Users\you\Documents\Codex\2026-04-22-node`,
		SessionStart: time.Date(2026, 4, 21, 22, 57, 12, 0, time.UTC),
		LastTime:     time.Date(2026, 4, 21, 23, 4, 30, 0, time.UTC),
		TurnID:       "turn-primary",
		UserText:     "Check node version",
		Commentary:   "Collecting the current Node.js version.",
		FinalText:    "Node.js version is 22.22.2.",
		ToolName:     "shell_command",
		ToolArgs:     `{"command":"node -v"}`,
		ToolOutput:   "v22.22.2\n",
		Reasoning:    []string{"Inspect the local Node.js binary.", "Return the exact version."},
		ApprovalQ:    "Approve command execution?",
		InputQ:       "Need the target directory.",
	})

	thread := model.Thread{
		ID:          threadID,
		Title:       "Check node version",
		CWD:         `C:\Users\you\Documents\Codex\2026-04-22-node`,
		ProjectName: "Shared/General",
		UpdatedAt:   time.Date(2026, 4, 21, 23, 4, 29, 0, time.UTC).Unix(),
		Raw:         json.RawMessage(model.MustJSON(map[string]any{"thread": map[string]any{"path": primaryPath}})),
	}

	result, err := BuildThreadLogArchive(context.Background(), cfgPaths, thread, LogArchiveHint{PreferredTurnID: "turn-primary"})
	if err != nil {
		t.Fatalf("BuildThreadLogArchive() failed: %v", err)
	}
	if got, want := result.SourceJSONLPath, primaryPath; got != want {
		t.Fatalf("SourceJSONLPath = %q, want %q", got, want)
	}
	if _, err := os.Stat(result.FilePath); err != nil {
		t.Fatalf("zip does not exist: %v", err)
	}
	if _, err := os.Stat(result.HumanLogPath); err != nil {
		t.Fatalf("human log does not exist: %v", err)
	}

	humanLogBytes, err := os.ReadFile(result.HumanLogPath)
	if err != nil {
		t.Fatalf("ReadFile(human log) failed: %v", err)
	}
	humanLog := string(humanLogBytes)
	for _, needle := range []string{
		"USER",
		"ASSISTANT (commentary)",
		"ASSISTANT (final_answer)",
		"REASONING SUMMARY",
		"TOOL CALL",
		"TOOL OUTPUT",
		"APPROVAL REQUEST",
		"INPUT REQUEST",
	} {
		if !strings.Contains(humanLog, needle) {
			t.Fatalf("human log does not contain %q\n%s", needle, humanLog)
		}
	}

	archive, err := zip.OpenReader(result.FilePath)
	if err != nil {
		t.Fatalf("zip.OpenReader failed: %v", err)
	}
	defer archive.Close()

	entries := map[string]bool{}
	for _, file := range archive.File {
		entries[file.Name] = true
	}
	if !entries["human-log.txt"] {
		t.Fatal("zip archive does not include human-log.txt")
	}
	if !entries[filepath.Base(primaryPath)] {
		t.Fatalf("zip archive does not include raw jsonl %q", filepath.Base(primaryPath))
	}

	dataResult, err := BuildThreadLogArchiveData(context.Background(), thread, LogArchiveHint{PreferredTurnID: "turn-primary"})
	if err != nil {
		t.Fatalf("BuildThreadLogArchiveData() failed: %v", err)
	}
	if dataResult.FileName == "" || len(dataResult.Data) == 0 {
		t.Fatalf("in-memory archive result = file %q len %d, want data", dataResult.FileName, len(dataResult.Data))
	}
	dataArchive, err := zip.NewReader(bytes.NewReader(dataResult.Data), int64(len(dataResult.Data)))
	if err != nil {
		t.Fatalf("zip.NewReader(in-memory) failed: %v", err)
	}
	dataEntries := map[string]bool{}
	for _, file := range dataArchive.File {
		dataEntries[file.Name] = true
	}
	if !dataEntries["human-log.txt"] || !dataEntries[filepath.Base(primaryPath)] {
		t.Fatalf("in-memory archive entries = %#v, want human-log and raw jsonl", dataEntries)
	}
}

func TestBuildThreadLogArchiveFallsBackAndChoosesBestCandidate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	cfgPaths := config.Paths{
		Home:    filepath.Join(home, ".ctr-go"),
		DataDir: filepath.Join(home, ".ctr-go", "data"),
		LogDir:  filepath.Join(home, ".ctr-go", "logs"),
		DBPath:  filepath.Join(home, ".ctr-go", "data", "state.sqlite"),
	}
	if err := cfgPaths.Ensure(); err != nil {
		t.Fatalf("Ensure() failed: %v", err)
	}

	threadID := "thread-fallback-123"
	root := filepath.Join(home, ".codex", "sessions", "2026", "04", "22")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(root) failed: %v", err)
	}
	older := filepath.Join(root, "rollout-2026-04-22T10-00-00-"+threadID+".jsonl")
	newer := filepath.Join(root, "rollout-2026-04-22T11-00-00-"+threadID+".jsonl")
	writeTranscriptFixture(t, older, threadID, fixtureTranscriptOptions{
		CWD:          `C:\Users\you\Projects\Codex`,
		SessionStart: time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC),
		LastTime:     time.Date(2026, 4, 22, 10, 10, 0, 0, time.UTC),
		TurnID:       "turn-old",
		UserText:     "old request",
		FinalText:    "old answer",
	})
	writeTranscriptFixture(t, newer, threadID, fixtureTranscriptOptions{
		CWD:          `C:\Users\you\Projects\Codex`,
		SessionStart: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		LastTime:     time.Date(2026, 4, 22, 11, 6, 0, 0, time.UTC),
		TurnID:       "turn-best",
		UserText:     "new request",
		FinalText:    "new answer",
	})

	thread := model.Thread{
		ID:          threadID,
		Title:       "Fallback candidate choice",
		CWD:         `C:\Users\you\Projects\Codex`,
		ProjectName: "Codex",
		UpdatedAt:   time.Date(2026, 4, 22, 11, 5, 30, 0, time.UTC).Unix(),
	}

	result, err := BuildThreadLogArchive(context.Background(), cfgPaths, thread, LogArchiveHint{
		PreferredTurnID: "turn-best",
		ThreadUpdatedAt: time.Date(2026, 4, 22, 11, 5, 30, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildThreadLogArchive() failed: %v", err)
	}
	if got, want := result.SourceJSONLPath, newer; got != want {
		t.Fatalf("SourceJSONLPath = %q, want %q", got, want)
	}
}

func TestBuildThreadLogArchiveDeduplicatesAdjacentMirrorMessageEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	cfgPaths := config.Paths{
		Home:    filepath.Join(home, ".ctr-go"),
		DataDir: filepath.Join(home, ".ctr-go", "data"),
		LogDir:  filepath.Join(home, ".ctr-go", "logs"),
		DBPath:  filepath.Join(home, ".ctr-go", "data", "state.sqlite"),
	}
	if err := cfgPaths.Ensure(); err != nil {
		t.Fatalf("Ensure() failed: %v", err)
	}

	threadID := "thread-dedupe-1"
	primaryPath := filepath.Join(home, ".codex", "sessions", "2026", "04", "21", "rollout-2026-04-21T16-58-54-"+threadID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(primaryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(primary) failed: %v", err)
	}
	writeTranscriptFixture(t, primaryPath, threadID, fixtureTranscriptOptions{
		CWD:                 `C:\Users\you\Projects\Codex`,
		SessionStart:        time.Date(2026, 4, 21, 16, 58, 54, 977000000, time.UTC),
		LastTime:            time.Date(2026, 4, 21, 16, 59, 10, 0, time.UTC),
		TurnID:              "turn-dedupe",
		UserText:            "Настрой https://github.com/PleasePrompto/ductor на этом ПК",
		Commentary:          "Сначала проверю, есть ли уже `ductor` в рабочей папке, и сниму требования из репозитория, чтобы поставить его без догадок.",
		FinalText:           "Готово.",
		DuplicateUserEvent:  true,
		DuplicateAgentEvent: true,
	})

	thread := model.Thread{
		ID:          threadID,
		Title:       "Настроить ductor",
		CWD:         `C:\Users\you\Projects\Codex`,
		ProjectName: "Codex",
		UpdatedAt:   time.Date(2026, 4, 21, 16, 59, 9, 0, time.UTC).Unix(),
		Raw:         json.RawMessage(model.MustJSON(map[string]any{"thread": map[string]any{"path": primaryPath}})),
	}

	result, err := BuildThreadLogArchive(context.Background(), cfgPaths, thread, LogArchiveHint{})
	if err != nil {
		t.Fatalf("BuildThreadLogArchive() failed: %v", err)
	}

	humanLogBytes, err := os.ReadFile(result.HumanLogPath)
	if err != nil {
		t.Fatalf("ReadFile(human log) failed: %v", err)
	}
	humanLog := string(humanLogBytes)

	userNeedle := "[2026-04-21T16:58:55.977Z] USER Настрой https://github.com/PleasePrompto/ductor на этом ПК"
	if got := strings.Count(humanLog, userNeedle); got != 1 {
		t.Fatalf("USER line count = %d, want 1\n%s", got, humanLog)
	}
	commentaryNeedle := "[2026-04-21T16:58:57.977Z] ASSISTANT (commentary) Сначала проверю, есть ли уже `ductor` в рабочей папке, и сниму требования из репозитория, чтобы поставить его без догадок."
	if got := strings.Count(humanLog, commentaryNeedle); got != 1 {
		t.Fatalf("commentary line count = %d, want 1\n%s", got, humanLog)
	}
}

type fixtureTranscriptOptions struct {
	CWD                 string
	SessionStart        time.Time
	LastTime            time.Time
	TurnID              string
	UserText            string
	Commentary          string
	FinalText           string
	ToolName            string
	ToolArgs            string
	ToolOutput          string
	Reasoning           []string
	ApprovalQ           string
	InputQ              string
	DuplicateUserEvent  bool
	DuplicateAgentEvent bool
}

func writeTranscriptFixture(t *testing.T, path string, threadID string, options fixtureTranscriptOptions) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%s) failed: %v", path, err)
	}
	defer file.Close()

	lines := []map[string]any{
		{
			"timestamp": options.SessionStart.Format(time.RFC3339Nano),
			"type":      "session_meta",
			"payload": map[string]any{
				"id":         threadID,
				"timestamp":  options.SessionStart.Add(-2 * time.Second).Format(time.RFC3339Nano),
				"cwd":        options.CWD,
				"originator": "Codex Desktop",
				"source":     "vscode",
			},
		},
		{
			"timestamp": options.SessionStart.Add(1 * time.Second).Format(time.RFC3339Nano),
			"type":      "response_item",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": options.UserText},
				},
			},
		},
	}
	if options.DuplicateUserEvent {
		lines = append(lines, map[string]any{
			"timestamp": options.SessionStart.Add(1 * time.Second).Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": options.UserText,
			},
		})
	}
	if len(options.Reasoning) > 0 {
		summary := make([]map[string]any, 0, len(options.Reasoning))
		for _, entry := range options.Reasoning {
			summary = append(summary, map[string]any{"text": entry})
		}
		lines = append(lines, map[string]any{
			"timestamp": options.SessionStart.Add(2 * time.Second).Format(time.RFC3339Nano),
			"type":      "response_item",
			"payload": map[string]any{
				"type":    "reasoning",
				"summary": summary,
			},
		})
	}
	if options.Commentary != "" {
		lines = append(lines, map[string]any{
			"timestamp": options.SessionStart.Add(3 * time.Second).Format(time.RFC3339Nano),
			"type":      "response_item",
			"payload": map[string]any{
				"type":  "message",
				"role":  "assistant",
				"phase": "commentary",
				"content": []map[string]any{
					{"type": "output_text", "text": options.Commentary},
				},
			},
		})
		if options.DuplicateAgentEvent {
			lines = append(lines, map[string]any{
				"timestamp": options.SessionStart.Add(3 * time.Second).Format(time.RFC3339Nano),
				"type":      "event_msg",
				"payload": map[string]any{
					"type":    "agent_message",
					"phase":   "commentary",
					"message": options.Commentary,
				},
			})
		}
	}
	if options.ToolName != "" {
		lines = append(lines, map[string]any{
			"timestamp": options.SessionStart.Add(4 * time.Second).Format(time.RFC3339Nano),
			"type":      "response_item",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      options.ToolName,
				"call_id":   "call-1",
				"arguments": options.ToolArgs,
			},
		})
		lines = append(lines, map[string]any{
			"timestamp": options.SessionStart.Add(5 * time.Second).Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload": map[string]any{
				"type":              "exec_command_end",
				"call_id":           "call-1",
				"turn_id":           options.TurnID,
				"cwd":               options.CWD,
				"command":           []string{"pwsh", "-Command", "node -v"},
				"aggregated_output": options.ToolOutput,
				"exit_code":         0,
				"status":            "completed",
			},
		})
	}
	if options.ApprovalQ != "" {
		lines = append(lines, map[string]any{
			"timestamp": options.SessionStart.Add(6 * time.Second).Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload": map[string]any{
				"type":       "approval_request",
				"request_id": "req-approval-1",
				"thread_id":  threadID,
				"turn_id":    options.TurnID,
				"question":   options.ApprovalQ,
			},
		})
	}
	if options.InputQ != "" {
		lines = append(lines, map[string]any{
			"timestamp": options.SessionStart.Add(7 * time.Second).Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload": map[string]any{
				"type":       "user_input_request",
				"request_id": "req-input-1",
				"thread_id":  threadID,
				"turn_id":    options.TurnID,
				"question":   options.InputQ,
			},
		})
	}
	lines = append(lines, map[string]any{
		"timestamp": options.LastTime.Add(-1 * time.Second).Format(time.RFC3339Nano),
		"type":      "event_msg",
		"payload": map[string]any{
			"type":    "task_started",
			"turn_id": options.TurnID,
		},
	})
	lines = append(lines, map[string]any{
		"timestamp": options.LastTime.Format(time.RFC3339Nano),
		"type":      "response_item",
		"payload": map[string]any{
			"type":  "message",
			"role":  "assistant",
			"phase": "final_answer",
			"content": []map[string]any{
				{"type": "output_text", "text": options.FinalText},
			},
		},
	})

	for _, line := range lines {
		payload, err := json.Marshal(line)
		if err != nil {
			t.Fatalf("Marshal(line) failed: %v", err)
		}
		if _, err := file.Write(append(payload, '\n')); err != nil {
			t.Fatalf("Write() failed: %v", err)
		}
	}
}
