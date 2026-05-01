package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

type lifecycleFields map[string]any

const (
	diagnosticLogWindow = time.Minute
	// Hard cap for lifecycle diagnostics if app-server enters a tight error loop.
	diagnosticLogMaxPerWindow       = 120
	diagnosticLogMaxLineBytes       = 4096
	diagnosticRepeatWindow          = 10 * time.Minute
	diagnosticObserverRepeatWindow  = time.Minute
	diagnosticAppServerRepeatWindow = time.Minute
)

var (
	tokenLikeDiagnosticPattern = regexp.MustCompile(`(?i)\b(bot|token|api[_-]?hash|password|secret)[=:/ ]+[A-Za-z0-9:_\-.]{8,}`)
	localFileDiagnosticPattern = regexp.MustCompile(`(?:/|[A-Za-z]:\\)[^\s"'<>]*(?:\.sock|\.session|\.sqlite|\.env)\b`)
)

func discardDiagnosticLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

func (s *Service) SetLogger(logger *log.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if logger == nil {
		s.logger = discardDiagnosticLogger()
		return
	}
	s.logger = logger
}

func (s *Service) logLifecycle(event string, fields lifecycleFields) {
	event = strings.TrimSpace(event)
	if event == "" {
		return
	}
	if !s.allowLifecycleLog(event) {
		return
	}
	s.mu.RLock()
	logger := s.logger
	s.mu.RUnlock()
	if logger == nil {
		return
	}
	payload := map[string]any{
		"event": event,
		"at":    time.Now().UTC().Format(time.RFC3339Nano),
	}
	for key, value := range fields {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		payload[key] = sanitizeDiagnosticValue(value)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Printf("daemon_event %s", event)
		return
	}
	if len(data) > diagnosticLogMaxLineBytes {
		data, _ = json.Marshal(map[string]any{
			"event":     event,
			"at":        payload["at"],
			"truncated": true,
			"byte_len":  len(data),
		})
	}
	logger.Printf("daemon_event %s", data)
}

func (s *Service) allowLifecycleLog(event string) bool {
	now := time.Now().UTC()
	s.diagnosticMu.Lock()
	defer s.diagnosticMu.Unlock()
	if s.diagnosticBy == nil || s.diagnosticWin.IsZero() || now.Sub(s.diagnosticWin) >= diagnosticLogWindow {
		s.diagnosticWin = now
		s.diagnosticN = 0
		s.diagnosticBy = map[string]int{}
	}
	if s.diagnosticN >= diagnosticLogMaxPerWindow {
		return false
	}
	limit := diagnosticEventLimit(event)
	if s.diagnosticBy[event] >= limit {
		return false
	}
	s.diagnosticN++
	s.diagnosticBy[event]++
	return true
}

func (s *Service) allowDiagnosticRepeat(key string, interval time.Duration) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	now := time.Now().UTC()
	s.diagnosticMu.Lock()
	defer s.diagnosticMu.Unlock()
	if s.diagnosticLast == nil {
		s.diagnosticLast = map[string]time.Time{}
	}
	if last := s.diagnosticLast[key]; !last.IsZero() && now.Sub(last) < interval {
		return false
	}
	s.diagnosticLast[key] = now
	if len(s.diagnosticLast) > 1024 {
		for existing, last := range s.diagnosticLast {
			if now.Sub(last) >= interval {
				delete(s.diagnosticLast, existing)
			}
		}
	}
	return true
}

func diagnosticEventLimit(event string) int {
	lower := strings.ToLower(event)
	switch {
	case strings.Contains(lower, "error"), strings.Contains(lower, "failed"), strings.Contains(lower, "closed"), strings.Contains(lower, "repair"):
		return 60
	default:
		return 30
	}
}

func (s *Service) logThreadReadSkipped(threadID, reason string) {
	threadID = strings.TrimSpace(threadID)
	reason = strings.TrimSpace(reason)
	if threadID == "" {
		return
	}
	if reason == "" {
		reason = "unknown"
	}
	if !s.allowDiagnosticRepeat("thread_read_skipped:"+reason+":"+threadID, diagnosticRepeatWindow) {
		return
	}
	s.logLifecycle("thread_read_skipped", lifecycleFields{
		"operation": "poll_tracked",
		"thread_id": threadID,
		"reason":    reason,
		"debounce":  diagnosticRepeatWindow.String(),
	})
}

func (s *Service) logTelegramInbound(kind string, chatID, topicID int64, replyToMessageID int64, decision model.RouteDecision, text, collaborationMode string) {
	s.logLifecycle("telegram_inbound", lifecycleFields{
		"kind":               kind,
		"chat_key":           model.ChatKey(chatID, topicID),
		"reply_message_id":   replyToMessageID,
		"route_source":       string(decision.Source),
		"thread_id":          decision.ThreadID,
		"turn_id":            decision.TurnID,
		"request_id":         decision.RequestID,
		"text_len":           len([]rune(text)),
		"text_sha256":        shortTextHash(text),
		"collaboration_mode": collaborationMode,
	})
}

func (s *Service) logTelegramRenderContainsNil(threadID, turnID, panelKind string, messageID int64, text string) {
	if !strings.Contains(text, "<nil>") {
		return
	}
	textHash := shortTextHash(text)
	key := strings.Join([]string{
		"telegram_render_contains_nil",
		strings.TrimSpace(threadID),
		strings.TrimSpace(turnID),
		strings.TrimSpace(panelKind),
		textHash,
	}, ":")
	if !s.allowDiagnosticRepeat(key, diagnosticRepeatWindow) {
		return
	}
	s.logLifecycle("telegram_render_contains_nil", lifecycleFields{
		"thread_id":    threadID,
		"turn_id":      turnID,
		"panel_kind":   panelKind,
		"message_id":   messageID,
		"text_len":     len([]rune(text)),
		"text_sha256":  textHash,
		"contains_nil": true,
	})
}

func (s *Service) logTelegramRenderedMessagesContainsNil(threadID, turnID, panelKind string, messageID int64, messages []model.RenderedMessage) {
	for index, message := range messages {
		kind := strings.TrimSpace(panelKind)
		if len(messages) > 1 {
			kind = fmt.Sprintf("%s:%d", kind, index+1)
		}
		s.logTelegramRenderContainsNil(threadID, turnID, kind, messageID, message.Text)
	}
}

func (s *Service) logAppServerCall(method string, started time.Time, err error, session Session, fields lifecycleFields) {
	if fields == nil {
		fields = lifecycleFields{}
	}
	operation := strings.TrimSpace(fmt.Sprint(fields["operation"]))
	threadID := strings.TrimSpace(fmt.Sprint(fields["thread_id"]))
	includeTurns := strings.TrimSpace(fmt.Sprint(fields["include_turns"]))
	if err == nil && method == "ThreadRead" && operation == "poll_tracked" {
		return
	}
	if err != nil && method == "ThreadRead" && operation == "poll_tracked" && isThreadNotLoadedError(err) {
		return
	}
	if method == "ThreadRead" {
		outcome := "success"
		errKey := ""
		if err != nil {
			outcome = "error"
			errKey = sanitizeDiagnosticString(err.Error())
		}
		key := strings.Join([]string{"appserver_call", method, operation, threadID, includeTurns, outcome, errKey}, ":")
		if !s.allowDiagnosticRepeat(key, diagnosticAppServerRepeatWindow) {
			return
		}
	}
	fields["method"] = method
	fields["duration_ms"] = maxInt64(0, time.Since(started).Milliseconds())
	if err != nil {
		fields["outcome"] = "error"
		fields["error"] = err
		if tail := sanitizedStderrTail(session); len(tail) > 0 {
			fields["stderr_tail"] = tail
		}
	} else {
		fields["outcome"] = "success"
	}
	s.logLifecycle("appserver_call", fields)
}

func (s *Service) logObserverSyncResult(operation string, snapshot appserver.ThreadReadSnapshot) {
	if strings.TrimSpace(snapshot.LatestTurnID) == "" &&
		strings.TrimSpace(snapshot.LatestTurnStatus) == "" &&
		len(snapshot.DetailItems) == 0 {
		return
	}
	if operation == "poll_tracked" && !isTerminalTurnStatus(snapshot.LatestTurnStatus) && !snapshot.WaitingOnApproval && !snapshot.WaitingOnReply {
		return
	}
	key := strings.Join([]string{
		"observer_sync_result",
		operation,
		strings.TrimSpace(snapshot.Thread.ID),
		strings.TrimSpace(snapshot.LatestTurnID),
		strings.TrimSpace(snapshot.LatestTurnStatus),
		fmt.Sprint(snapshot.WaitingOnApproval),
		fmt.Sprint(snapshot.WaitingOnReply),
	}, ":")
	if !s.allowDiagnosticRepeat(key, diagnosticObserverRepeatWindow) {
		return
	}
	fields := snapshotDiagnosticFields(snapshot)
	fields["operation"] = operation
	s.logLifecycle("observer_sync_result", fields)
}

func (s *Service) maybeLogTelegramOriginTerminal(ctx context.Context, snapshot appserver.ThreadReadSnapshot) {
	threadID := strings.TrimSpace(snapshot.Thread.ID)
	turnID := strings.TrimSpace(snapshot.LatestTurnID)
	status := strings.TrimSpace(snapshot.LatestTurnStatus)
	if threadID == "" || turnID == "" || !isTerminalTurnStatus(status) || !s.isTelegramOriginTurn(ctx, threadID, turnID) {
		return
	}
	if decision, err := s.decideTelegramOriginEmptyInterruptedTerminal(ctx, &snapshot, time.Now().UTC()); err == nil && decision.Action == terminalGateDefer {
		return
	}
	key := telegramOriginTerminalLoggedKey(threadID, turnID)
	if key == "" {
		return
	}
	if value, err := s.store.GetState(ctx, key); err == nil && strings.TrimSpace(value) != "" {
		return
	}
	fields := snapshotDiagnosticFields(snapshot)
	fields["chat_source"] = model.PanelSourceTelegramInput
	s.logLifecycle("telegram_origin_turn_terminal", fields)
	_ = s.store.SetState(ctx, key, string(model.NowString()))
	_ = s.clearTelegramOriginEmptyInterruptedDefer(ctx, threadID, turnID)
}

func snapshotDiagnosticFields(snapshot appserver.ThreadReadSnapshot) lifecycleFields {
	counts := map[string]int{}
	for _, item := range snapshot.DetailItems {
		kind := strings.TrimSpace(item.Kind)
		if kind == "" {
			kind = "unknown"
		}
		counts[kind]++
	}
	return lifecycleFields{
		"thread_id":           snapshot.Thread.ID,
		"latest_turn_id":      snapshot.LatestTurnID,
		"latest_turn_status":  snapshot.LatestTurnStatus,
		"detail_item_count":   len(snapshot.DetailItems),
		"detail_kind_counts":  counts,
		"agent_message_count": len(snapshot.LatestAgentMessageEntries),
		"has_commentary":      strings.TrimSpace(snapshot.LatestProgressText) != "" || counts[model.DetailItemCommentary] > 0,
		"has_final":           strings.TrimSpace(snapshot.LatestFinalText) != "" || counts[model.DetailItemFinal] > 0,
		"has_tool":            strings.TrimSpace(snapshot.LatestToolID) != "" || counts[model.DetailItemTool] > 0,
		"has_output":          strings.TrimSpace(snapshot.LatestToolOutput) != "" || counts[model.DetailItemOutput] > 0,
		"waiting_approval":    snapshot.WaitingOnApproval,
		"waiting_reply":       snapshot.WaitingOnReply,
	}
}

func isTerminalTurnStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "interrupted", "canceled", "cancelled":
		return true
	default:
		return false
	}
}

func telegramOriginTerminalLoggedKey(threadID, turnID string) string {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return ""
	}
	return "turn_origin.telegram_terminal_logged." + threadID + "." + turnID
}

func shortTextHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}

func sanitizedStderrTail(session Session) []string {
	if session == nil {
		return nil
	}
	lines := session.StderrTail()
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(sanitizeDiagnosticString(line))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func sanitizeDiagnosticValue(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case error:
		return sanitizeDiagnosticString(typed.Error())
	case string:
		return sanitizeDiagnosticString(typed)
	case time.Time:
		if typed.IsZero() {
			return ""
		}
		return typed.UTC().Format(time.RFC3339Nano)
	case fmt.Stringer:
		return sanitizeDiagnosticString(typed.String())
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			item = strings.TrimSpace(sanitizeDiagnosticString(item))
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			out[key] = sanitizeDiagnosticValue(typed[key])
		}
		return out
	case map[string]int:
		out := make(map[string]int, len(typed))
		for key, item := range typed {
			out[sanitizeDiagnosticString(key)] = item
		}
		return out
	default:
		return typed
	}
}

func sanitizeDiagnosticString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		value = strings.ReplaceAll(value, home, "~")
	}
	value = tokenLikeDiagnosticPattern.ReplaceAllString(value, "$1=<redacted>")
	value = localFileDiagnosticPattern.ReplaceAllString(value, "<local-file>")
	if len(value) > 2000 {
		value = value[:2000] + "...<truncated>"
	}
	return value
}

func parseRepairRequest(value string) (time.Time, string) {
	parts := strings.SplitN(strings.TrimSpace(value), "|", 2)
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return time.Time{}, "manual"
	}
	at, _ := time.Parse(time.RFC3339Nano, parts[0])
	if len(parts) == 1 || strings.TrimSpace(parts[1]) == "" {
		return at, "manual"
	}
	return at, strings.TrimSpace(parts[1])
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
