package daemon

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

const sessionTailOverlayMaxBytes int64 = 4 * 1024 * 1024

type sessionTailToolOverlay struct {
	CallID    string
	Name      string
	Command   string
	Status    string
	Output    string
	TurnID    string
	Timestamp string
	FP        string
}

type sessionTailFinalOverlay struct {
	TurnID    string
	Text      string
	Timestamp string
	FP        string
}

func applySessionTailToolOverlay(thread model.Thread, snapshot *appserver.ThreadReadSnapshot) bool {
	if snapshot == nil || strings.TrimSpace(thread.ID) == "" {
		return false
	}
	overlay, ok := latestActiveSessionTailTool(thread, snapshot.LatestTurnID)
	if !ok {
		return false
	}
	overlayTurnID := strings.TrimSpace(overlay.TurnID)
	snapshotTurnID := strings.TrimSpace(snapshot.LatestTurnID)
	if snapshotTurnID != "" && overlayTurnID != "" && snapshotTurnID != overlayTurnID && !isTerminalStatus(snapshot.LatestTurnStatus) {
		return false
	}
	if strings.TrimSpace(snapshot.LatestFinalFP) != "" && !snapshot.WaitingOnApproval && !snapshot.WaitingOnReply {
		return false
	}
	label := strings.TrimSpace(overlay.Command)
	if label == "" {
		label = strings.TrimSpace(overlay.Name)
	}
	if label == "" {
		return false
	}
	status := strings.TrimSpace(overlay.Status)
	if status == "" {
		status = "running"
	}
	turnID := strings.TrimSpace(overlay.TurnID)
	if turnID == "" {
		turnID = strings.TrimSpace(snapshot.LatestTurnID)
	}
	snapshot.LatestToolID = overlay.CallID
	snapshot.LatestToolKind = "sessionTailToolCall"
	snapshot.LatestToolLabel = label
	snapshot.LatestToolStatus = status
	snapshot.LatestToolOutput = overlay.Output
	snapshot.LatestToolFP = overlay.FP
	snapshot.LatestToolSource = "session_tail"
	snapshot.LatestProgressFP = overlay.FP
	snapshot.LatestProgressText = label
	snapshot.SessionTailActiveTool = strings.EqualFold(status, "running")
	if turnID != "" {
		snapshot.LatestTurnID = turnID
		snapshot.Thread.ActiveTurnID = turnID
	}
	if snapshot.SessionTailActiveTool {
		snapshot.LatestTurnStatus = "inProgress"
		snapshot.Thread.Status = "inProgress"
	}
	snapshot.DetailItems = upsertSessionTailToolDetails(snapshot.DetailItems, overlay)
	return true
}

func applySessionTailFinalOverlay(thread model.Thread, snapshot *appserver.ThreadReadSnapshot) bool {
	if snapshot == nil || strings.TrimSpace(thread.ID) == "" {
		return false
	}
	overlay, ok := latestSessionTailFinal(thread, snapshot.LatestTurnID)
	if !ok {
		return false
	}
	if strings.TrimSpace(snapshot.LatestFinalText) == "" {
		snapshot.LatestFinalText = overlay.Text
	}
	if strings.TrimSpace(snapshot.LatestFinalFP) == "" {
		snapshot.LatestFinalFP = overlay.FP
	}
	snapshot.LatestTurnStatus = "completed"
	snapshot.Thread.Status = "completed"
	snapshot.Thread.ActiveTurnID = ""
	snapshot.SessionTailActiveTool = false
	snapshot.DetailItems = upsertSessionTailFinalDetails(snapshot.DetailItems, overlay)
	return true
}

func latestActiveSessionTailTool(thread model.Thread, fallbackTurnID string) (sessionTailToolOverlay, bool) {
	sessionPath, err := sessionPathForOverlay(thread)
	if err != nil {
		return sessionTailToolOverlay{}, false
	}
	lines, err := readTailLines(sessionPath, sessionTailOverlayMaxBytes)
	if err != nil || len(lines) == 0 {
		return sessionTailToolOverlay{}, false
	}
	return latestActiveToolFromSessionLines(lines, fallbackTurnID)
}

func latestSessionTailFinal(thread model.Thread, fallbackTurnID string) (sessionTailFinalOverlay, bool) {
	sessionPath, err := sessionPathForOverlay(thread)
	if err != nil {
		return sessionTailFinalOverlay{}, false
	}
	lines, err := readTailLines(sessionPath, sessionTailOverlayMaxBytes)
	if err != nil || len(lines) == 0 {
		return sessionTailFinalOverlay{}, false
	}
	return latestFinalFromSessionLines(lines, fallbackTurnID)
}

func threadHasActiveSessionTailTool(thread model.Thread, fallbackTurnID string) bool {
	_, ok := latestActiveSessionTailTool(thread, fallbackTurnID)
	return ok
}

func sessionPathForOverlay(thread model.Thread) (string, error) {
	if direct := strings.TrimSpace(threadPathFromRaw(thread.Raw)); direct != "" {
		if _, err := os.Stat(direct); err == nil {
			return direct, nil
		}
	}
	return findSessionLogPath(thread, LogArchiveHint{})
}

func readTailLines(path string, maxBytes int64) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	offset := int64(0)
	if size > maxBytes {
		offset = size - maxBytes
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		if newline := bytes.IndexByte(data, '\n'); newline >= 0 {
			data = data[newline+1:]
		}
	}
	parts := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			lines = append(lines, part)
		}
	}
	return lines, nil
}

func latestFinalFromSessionLines(lines []string, fallbackTurnID string) (sessionTailFinalOverlay, bool) {
	fallbackTurnID = strings.TrimSpace(fallbackTurnID)
	currentTurnID := fallbackTurnID
	var latest sessionTailFinalOverlay
	for _, line := range lines {
		var entry sessionLogEnvelope
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type == "turn_context" {
			if turnID := strings.TrimSpace(valueFromMap(entry.Payload, "turn_id")); turnID != "" {
				currentTurnID = turnID
			}
			continue
		}
		if entry.Type != "event_msg" || valueFromMap(entry.Payload, "type") != "task_complete" {
			continue
		}
		turnID := strings.TrimSpace(valueFromMap(entry.Payload, "turn_id"))
		if turnID == "" {
			turnID = currentTurnID
		}
		if fallbackTurnID != "" && turnID != fallbackTurnID {
			continue
		}
		text := strings.TrimSpace(valueFromMap(entry.Payload, "last_agent_message"))
		if text == "" {
			text = strings.TrimSpace(valueFromMap(entry.Payload, "message"))
		}
		if text == "" {
			continue
		}
		latest = sessionTailFinalOverlay{
			TurnID:    turnID,
			Text:      text,
			Timestamp: strings.TrimSpace(entry.Timestamp),
			FP:        hashStrings("session_tail_final", turnID, text),
		}
	}
	return latest, strings.TrimSpace(latest.Text) != ""
}

func latestActiveToolFromSessionLines(lines []string, fallbackTurnID string) (sessionTailToolOverlay, bool) {
	type callState struct {
		overlay sessionTailToolOverlay
		closed  bool
		order   int
	}
	calls := map[string]*callState{}
	currentTurnID := strings.TrimSpace(fallbackTurnID)
	order := 0
	for _, line := range lines {
		var entry sessionLogEnvelope
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type == "turn_context" {
			if turnID := strings.TrimSpace(valueFromMap(entry.Payload, "turn_id")); turnID != "" {
				currentTurnID = turnID
			}
			continue
		}
		switch entry.Type {
		case "response_item":
			itemType := valueFromMap(entry.Payload, "type")
			switch itemType {
			case "function_call", "custom_tool_call":
				overlay, ok := overlayFromResponseItem(entry, currentTurnID)
				if !ok {
					continue
				}
				order++
				calls[overlay.CallID] = &callState{overlay: overlay, order: order}
			case "function_call_output", "custom_tool_call_output":
				callID := strings.TrimSpace(valueFromMap(entry.Payload, "call_id"))
				if callID == "" {
					callID = strings.TrimSpace(valueFromMap(entry.Payload, "id"))
				}
				if state := calls[callID]; state != nil {
					state.closed = true
					state.overlay.Status = "completed"
					state.overlay.Output = strings.TrimSpace(valueFromMap(entry.Payload, "output"))
				}
			}
		case "event_msg":
			eventType := valueFromMap(entry.Payload, "type")
			if eventType == "task_complete" {
				turnID := strings.TrimSpace(valueFromMap(entry.Payload, "turn_id"))
				if turnID == "" {
					turnID = currentTurnID
				}
				for _, state := range calls {
					if state != nil && strings.TrimSpace(state.overlay.TurnID) == turnID {
						state.closed = true
						state.overlay.Status = "completed"
					}
				}
				continue
			}
			if eventType != "exec_command_end" {
				continue
			}
			callID := strings.TrimSpace(valueFromMap(entry.Payload, "call_id"))
			if callID == "" {
				callID = strings.TrimSpace(valueFromMap(entry.Payload, "id"))
			}
			if state := calls[callID]; state != nil {
				state.closed = true
				status := strings.TrimSpace(valueFromMap(entry.Payload, "status"))
				if status == "" {
					status = "completed"
				}
				state.overlay.Status = status
				state.overlay.Output = strings.TrimSpace(valueFromMap(entry.Payload, "aggregated_output"))
			}
		}
	}
	var latest *callState
	for _, state := range calls {
		if state == nil || state.closed {
			continue
		}
		if latest == nil || state.order > latest.order {
			latest = state
		}
	}
	if latest == nil {
		return sessionTailToolOverlay{}, false
	}
	latest.overlay.Status = "running"
	latest.overlay.FP = hashStrings("session_tail", latest.overlay.CallID, latest.overlay.Command, latest.overlay.Status, latest.overlay.Output)
	return latest.overlay, true
}

func overlayFromResponseItem(entry sessionLogEnvelope, turnID string) (sessionTailToolOverlay, bool) {
	callID := strings.TrimSpace(valueFromMap(entry.Payload, "call_id"))
	if callID == "" {
		callID = strings.TrimSpace(valueFromMap(entry.Payload, "id"))
	}
	if callID == "" {
		return sessionTailToolOverlay{}, false
	}
	name := firstPayloadString(entry.Payload, "name", "tool")
	command := commandFromResponseItem(entry.Payload)
	if strings.TrimSpace(command) == "" && strings.TrimSpace(name) == "" {
		return sessionTailToolOverlay{}, false
	}
	return sessionTailToolOverlay{
		CallID:    callID,
		Name:      name,
		Command:   command,
		Status:    "running",
		TurnID:    strings.TrimSpace(turnID),
		Timestamp: strings.TrimSpace(entry.Timestamp),
		FP:        hashStrings("session_tail", callID, command, "running"),
	}, true
}

func commandFromResponseItem(payload map[string]any) string {
	name := firstPayloadString(payload, "name", "tool")
	if command := commandFromArgumentsString(valueFromMap(payload, "arguments")); command != "" {
		return command
	}
	if strings.EqualFold(name, "shell_command") {
		args := strings.TrimSpace(valueFromMap(payload, "arguments"))
		if args != "" {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(args), &parsed); err == nil {
				if command := strings.TrimSpace(valueFromMap(parsed, "command")); command != "" {
					return command
				}
			}
		}
	}
	for _, key := range []string{"command", "cmd", "input"} {
		if command := strings.TrimSpace(valueFromMap(payload, key)); command != "" {
			return command
		}
	}
	return ""
}

func commandFromArgumentsString(arguments string) string {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" || arguments == "<nil>" || arguments == "{}" || arguments == "[]" || arguments == "map[]" {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(arguments), &parsed); err == nil {
		if len(parsed) == 0 {
			return ""
		}
		if command := firstPayloadString(parsed, "command", "cmd", "input", "query", "path", "text"); command != "" {
			return command
		}
		return ""
	}
	return arguments
}

func upsertSessionTailToolDetails(items []model.DetailItem, overlay sessionTailToolOverlay) []model.DetailItem {
	if strings.TrimSpace(overlay.CallID) == "" {
		return items
	}
	out := make([]model.DetailItem, 0, len(items)+2)
	for _, item := range items {
		if item.ID == overlay.CallID || item.ID == overlay.CallID+":output" {
			continue
		}
		out = append(out, item)
	}
	commentaryIndex := 0
	for _, item := range out {
		if item.CommentaryIndex > commentaryIndex {
			commentaryIndex = item.CommentaryIndex
		}
	}
	out = append(out, model.DetailItem{
		ID:              overlay.CallID,
		Kind:            model.DetailItemTool,
		Label:           strings.TrimSpace(firstNonEmpty(overlay.Command, overlay.Name)),
		Status:          strings.TrimSpace(overlay.Status),
		FP:              overlay.FP,
		CommentaryIndex: commentaryIndex,
	})
	if output := strings.TrimSpace(overlay.Output); output != "" {
		out = append(out, model.DetailItem{
			ID:              overlay.CallID + ":output",
			Kind:            model.DetailItemOutput,
			Output:          output,
			FP:              hashStrings("session_tail_output", overlay.CallID, output),
			CommentaryIndex: commentaryIndex,
		})
	}
	return out
}

func upsertSessionTailFinalDetails(items []model.DetailItem, overlay sessionTailFinalOverlay) []model.DetailItem {
	if strings.TrimSpace(overlay.FP) == "" || strings.TrimSpace(overlay.Text) == "" {
		return items
	}
	id := strings.TrimSpace(overlay.TurnID)
	if id == "" {
		id = "session-tail-final"
	}
	id += ":final"
	out := make([]model.DetailItem, 0, len(items)+1)
	for _, item := range items {
		if item.ID == id || item.FP == overlay.FP || item.Kind == model.DetailItemFinal {
			continue
		}
		out = append(out, item)
	}
	out = append(out, model.DetailItem{
		ID:    id,
		Kind:  model.DetailItemFinal,
		Text:  overlay.Text,
		FP:    overlay.FP,
		Phase: "final_answer",
	})
	return out
}
