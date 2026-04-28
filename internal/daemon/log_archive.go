package daemon

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"codex-telegram-remote-go/internal/config"
	"codex-telegram-remote-go/internal/model"
)

type sessionLogEnvelope struct {
	Timestamp string         `json:"timestamp"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
}

type logArchiveResult struct {
	FileName string
	FilePath string
	Caption  string
}

type LogArchiveHint struct {
	PreferredTurnID string
	ThreadUpdatedAt time.Time
}

type BuildLogArchiveResult struct {
	FileName        string
	FilePath        string
	HumanLogPath    string
	SourceJSONLPath string
}

func (s *Service) sendFullLogArchive(ctx context.Context, chatID, topicID int64, threadID string) (*DirectResponse, error) {
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender == nil {
		return &DirectResponse{Text: "Telegram sender is not ready yet."}, nil
	}
	thread, err := s.store.GetThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return &DirectResponse{Text: fmt.Sprintf("Unknown thread: %s", threadID)}, nil
	}
	archive, err := BuildThreadLogArchive(ctx, s.cfg.Paths, *thread, LogArchiveHint{})
	if err != nil {
		return &DirectResponse{Text: fmt.Sprintf("Could not build full log: %v", err)}, nil
	}
	caption := s.visualHeader(ctx, "Full log", *thread, "")
	if _, err := sender.SendDocument(ctx, chatID, topicID, archive.FileName, archive.FilePath, caption); err != nil {
		return &DirectResponse{Text: fmt.Sprintf("Could not send full log: %v", err)}, nil
	}
	s.cleanupArchiveFiles(archive)
	return &DirectResponse{CallbackText: "Полный лог отправлен."}, nil
}

func BuildThreadLogArchive(ctx context.Context, paths config.Paths, thread model.Thread, hint LogArchiveHint) (*BuildLogArchiveResult, error) {
	sessionPath, err := findSessionLogPath(thread, hint)
	if err != nil {
		return nil, err
	}
	humanLog, err := buildHumanLog(sessionPath)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(paths.DataDir, "log-archives")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	baseName := fmt.Sprintf("%s-%s-log", sanitizeFileName(thread.ProjectName), sanitizeFileName(thread.ShortID()))
	humanLogPath := filepath.Join(dir, baseName+"-human-log.txt")
	if err := os.WriteFile(humanLogPath, []byte(humanLog), 0o644); err != nil {
		return nil, err
	}
	fileName := baseName + ".zip"
	filePath := filepath.Join(dir, fileName)
	if err := writeLogArchive(filePath, sessionPath, humanLog); err != nil {
		return nil, err
	}
	return &BuildLogArchiveResult{
		FileName:        fileName,
		FilePath:        filePath,
		HumanLogPath:    humanLogPath,
		SourceJSONLPath: sessionPath,
	}, nil
}

func (s *Service) cleanupArchiveFiles(result *BuildLogArchiveResult) {
	if result == nil {
		return
	}
	for _, path := range []string{result.FilePath, result.HumanLogPath} {
		if strings.TrimSpace(path) == "" {
			continue
		}
		_ = os.Remove(path)
	}
}

func (s *Service) cleanupTempArtifacts(ctx context.Context) {
	cleanupDir(filepath.Join(s.cfg.Paths.DataDir, "log-archives"), 24*time.Hour)
	cleanupDir(filepath.Join(s.cfg.Paths.DataDir, "tool-output"), 0)
	cleanupDir(filepath.Join(s.cfg.Paths.DataDir, "details-exports"), 24*time.Hour)
	_ = ctx
}

func cleanupDir(dir string, ttl time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if ttl > 0 && now.Sub(info.ModTime().UTC()) < ttl {
			continue
		}
		_ = os.RemoveAll(path)
	}
}

func findSessionLogPath(thread model.Thread, hint LogArchiveHint) (string, error) {
	if direct := strings.TrimSpace(threadPathFromRaw(thread.Raw)); direct != "" {
		if _, err := os.Stat(direct); err == nil {
			return direct, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(home, ".codex", "sessions")
	type candidate struct {
		path    string
		modTime time.Time
		score   int
	}
	var matches []candidate
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		if !strings.Contains(name, strings.ToLower(thread.ID)) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		score := 0
		if strings.TrimSpace(hint.PreferredTurnID) != "" {
			if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), hint.PreferredTurnID) {
				score += 100
			}
		}
		if !hint.ThreadUpdatedAt.IsZero() {
			delta := info.ModTime().Sub(hint.ThreadUpdatedAt)
			if delta < 0 {
				delta = -delta
			}
			switch {
			case delta <= 5*time.Minute:
				score += 20
			case delta <= 30*time.Minute:
				score += 10
			}
		}
		matches = append(matches, candidate{path: path, modTime: info.ModTime(), score: score})
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("session file for thread %s was not found under %s", thread.ID, root)
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].modTime.After(matches[j].modTime)
	})
	return matches[0].path, nil
}

func threadPathFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if threadNode, ok := payload["thread"].(map[string]any); ok {
		if path := strings.TrimSpace(fmt.Sprintf("%v", threadNode["path"])); path != "" && path != "<nil>" {
			return path
		}
	}
	if path := strings.TrimSpace(fmt.Sprintf("%v", payload["path"])); path != "" && path != "<nil>" {
		return path
	}
	return ""
}

func buildHumanLog(sessionPath string) (string, error) {
	file, err := os.Open(sessionPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 1024*1024)
	scanner.Buffer(buffer, 8*1024*1024)

	lines := []string{
		"CODEX SESSION LOG",
		fmt.Sprintf("SOURCE: %s", sessionPath),
		"",
	}
	lineNo := 0
	lastRendered := ""
	for scanner.Scan() {
		lineNo++
		raw := scanner.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		var entry sessionLogEnvelope
		if err := json.Unmarshal(raw, &entry); err != nil {
			lines = append(lines, fmt.Sprintf("[%04d] [decode-error] %v", lineNo, err))
			continue
		}
		rendered := renderSessionLogEntry(entry)
		if strings.TrimSpace(rendered) != "" {
			if rendered == lastRendered {
				continue
			}
			lines = append(lines, rendered)
			lastRendered = rendered
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

func renderSessionLogEntry(entry sessionLogEnvelope) string {
	timestamp := strings.TrimSpace(entry.Timestamp)
	switch entry.Type {
	case "session_meta":
		id := valueFromMap(entry.Payload, "id")
		cwd := valueFromMap(entry.Payload, "cwd")
		originator := valueFromMap(entry.Payload, "originator")
		return fmt.Sprintf("[%s] SESSION id=%s cwd=%s originator=%s", timestamp, id, cwd, originator)
	case "turn_context":
		turnID := valueFromMap(entry.Payload, "turn_id")
		cwd := valueFromMap(entry.Payload, "cwd")
		modelName := valueFromMap(entry.Payload, "model")
		return fmt.Sprintf("[%s] TURN CONTEXT turn=%s cwd=%s model=%s", timestamp, turnID, cwd, modelName)
	case "event_msg":
		return renderEventMsg(timestamp, entry.Payload)
	case "response_item":
		return renderResponseItem(timestamp, entry.Payload)
	default:
		return ""
	}
}

func renderEventMsg(timestamp string, payload map[string]any) string {
	switch valueFromMap(payload, "type") {
	case "task_started":
		return fmt.Sprintf("[%s] TASK STARTED turn=%s", timestamp, valueFromMap(payload, "turn_id"))
	case "thread_name_updated":
		return fmt.Sprintf("[%s] THREAD NAME %s", timestamp, valueFromMap(payload, "thread_name"))
	case "user_message":
		return fmt.Sprintf("[%s] USER %s", timestamp, strings.TrimSpace(valueFromMap(payload, "message")))
	case "agent_message":
		phase := valueFromMap(payload, "phase")
		if phase == "" {
			phase = "message"
		}
		return fmt.Sprintf("[%s] ASSISTANT (%s) %s", timestamp, phase, strings.TrimSpace(valueFromMap(payload, "message")))
	case "exec_command_end":
		command := renderCommand(valueFromMapAny(payload, "command"))
		status := valueFromMap(payload, "status")
		output := strings.TrimSpace(valueFromMap(payload, "aggregated_output"))
		lines := []string{fmt.Sprintf("[%s] TOOL OUTPUT (%s) %s", timestamp, status, command)}
		if output != "" {
			lines = append(lines, indentBlock(output))
		}
		return strings.Join(lines, "\n")
	case "approval_request":
		return fmt.Sprintf("[%s] APPROVAL REQUEST %s", timestamp, strings.TrimSpace(valueFromMap(payload, "question")))
	case "user_input_request":
		return fmt.Sprintf("[%s] INPUT REQUEST %s", timestamp, strings.TrimSpace(valueFromMap(payload, "question")))
	case "task_complete":
		return fmt.Sprintf("[%s] TASK COMPLETE %s", timestamp, strings.TrimSpace(valueFromMap(payload, "last_agent_message")))
	case "turn_aborted":
		return fmt.Sprintf("[%s] TURN ABORTED reason=%s", timestamp, valueFromMap(payload, "reason"))
	default:
		return ""
	}
}

func renderResponseItem(timestamp string, payload map[string]any) string {
	switch valueFromMap(payload, "type") {
	case "function_call":
		name := valueFromMap(payload, "name")
		args := strings.TrimSpace(valueFromMap(payload, "arguments"))
		if args == "" {
			return fmt.Sprintf("[%s] TOOL CALL %s", timestamp, name)
		}
		return fmt.Sprintf("[%s] TOOL CALL %s %s", timestamp, name, args)
	case "function_call_output":
		output := strings.TrimSpace(valueFromMap(payload, "output"))
		if output == "" {
			return ""
		}
		return fmt.Sprintf("[%s] TOOL OUTPUT\n%s", timestamp, indentBlock(output))
	case "reasoning":
		if summary := renderReasoningSummary(payload); summary != "" {
			return fmt.Sprintf("[%s] REASONING SUMMARY %s", timestamp, summary)
		}
	case "message":
		role := strings.ToLower(valueFromMap(payload, "role"))
		phase := valueFromMap(payload, "phase")
		text := collectMessageContent(payload)
		switch role {
		case "user":
			if text != "" {
				return fmt.Sprintf("[%s] USER %s", timestamp, text)
			}
		case "assistant":
			if phase == "" {
				phase = "message"
			}
			if text != "" {
				return fmt.Sprintf("[%s] ASSISTANT (%s) %s", timestamp, phase, text)
			}
		}
	}
	return ""
}

func renderReasoningSummary(payload map[string]any) string {
	raw, ok := payload["summary"].([]any)
	if !ok || len(raw) == 0 {
		return ""
	}
	parts := make([]string, 0, len(raw))
	for _, item := range raw {
		switch typed := item.(type) {
		case string:
			if text := strings.TrimSpace(typed); text != "" {
				parts = append(parts, text)
			}
		case map[string]any:
			if text := strings.TrimSpace(valueFromMap(typed, "text")); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, " | ")
}

func collectMessageContent(payload map[string]any) string {
	content, ok := payload["content"].([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(content))
	for _, item := range content {
		typed, ok := item.(map[string]any)
		if !ok {
			continue
		}
		text := strings.TrimSpace(valueFromMap(typed, "text"))
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func renderCommand(value any) string {
	switch typed := value.(type) {
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprintf("%v", item))
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}

func indentBlock(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for index, line := range lines {
		lines[index] = "    " + line
	}
	return strings.Join(lines, "\n")
}

func valueFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", values[key]))
}

func valueFromMapAny(values map[string]any, key string) any {
	if values == nil {
		return nil
	}
	return values[key]
}

func writeLogArchive(destination, sessionPath, humanLog string) error {
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()

	archive := zip.NewWriter(file)
	defer archive.Close()

	if err := writeZipFile(archive, "human-log.txt", strings.NewReader(humanLog)); err != nil {
		return err
	}
	rawFile, err := os.Open(sessionPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer rawFile.Close()
	return writeZipFile(archive, filepath.Base(sessionPath), rawFile)
}

func writeZipFile(archive *zip.Writer, name string, source io.Reader) error {
	writer, err := archive.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, source)
	return err
}
