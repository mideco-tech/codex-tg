package appserver

import (
	"fmt"
	"strings"

	"github.com/mideco-tech/codex-tg/internal/model"
)

type LiveEventKind string

const (
	LiveEventTurnStarted        LiveEventKind = "turn_started"
	LiveEventTurnCompleted      LiveEventKind = "turn_completed"
	LiveEventThreadStatus       LiveEventKind = "thread_status"
	LiveEventToolStarted        LiveEventKind = "tool_started"
	LiveEventToolUpdated        LiveEventKind = "tool_updated"
	LiveEventToolCompleted      LiveEventKind = "tool_completed"
	LiveEventAgentMessage       LiveEventKind = "agent_message"
	LiveEventApprovalRequest    LiveEventKind = "approval_request"
	LiveEventInputRequest       LiveEventKind = "input_request"
	LiveEventLegacyTaskStarted  LiveEventKind = "legacy_task_started"
	LiveEventLegacyTaskComplete LiveEventKind = "legacy_task_complete"
)

type NormalizedLiveEvent struct {
	Kind        LiveEventKind
	Method      string
	ThreadID    string
	ThreadTitle string
	ProjectName string
	TurnID      string
	TurnStatus  string
	ItemID      string
	ItemKind    string
	Label       string
	Status      string
	Output      string
	Text        string
	Phase       string
	RequestID   string
	Raw         Event
}

func NormalizeAppServerLiveEvent(event Event, thread model.Thread) (NormalizedLiveEvent, bool) {
	normalized := NormalizedLiveEvent{
		Method: strings.TrimSpace(event.Method),
		Raw:    event,
	}
	switch event.Channel {
	case "notification":
		return normalizeNotificationEvent(event, thread, normalized)
	case "server_request":
		return normalizeServerRequestEvent(event, thread, normalized)
	default:
		return NormalizedLiveEvent{}, false
	}
}

func normalizeNotificationEvent(event Event, thread model.Thread, normalized NormalizedLiveEvent) (NormalizedLiveEvent, bool) {
	method := strings.TrimSpace(strings.ToLower(event.Method))
	params := event.Params
	if strings.HasPrefix(method, "codex/event") {
		return normalizeLegacyCodexEvent(event, thread, normalized)
	}
	normalized.ThreadID = firstString(nestedThreadID(params), thread.ID)
	normalized.ThreadTitle = firstString(stringValue(params["threadTitle"], ""), thread.Title)
	normalized.ProjectName = firstString(stringValue(params["projectName"], ""), thread.ProjectName)
	normalized.TurnID = firstString(stringValue(params["turnId"], ""), nestedTurnID(params), thread.ActiveTurnID)
	normalized.TurnStatus = statusText(firstPayload(params, "status", "turn.status"))

	switch method {
	case "turn/started":
		normalized.Kind = LiveEventTurnStarted
		normalized.TurnStatus = firstString(normalized.TurnStatus, "inProgress")
		return normalized, normalized.ThreadID != "" || normalized.TurnID != ""
	case "turn/completed":
		normalized.Kind = LiveEventTurnCompleted
		normalized.TurnStatus = firstString(normalized.TurnStatus, statusText(firstPayload(params, "turn.status")), "completed")
		return normalized, normalized.ThreadID != "" || normalized.TurnID != ""
	case "thread/status/changed":
		normalized.Kind = LiveEventThreadStatus
		normalized.TurnStatus = firstString(normalized.TurnStatus, statusText(firstPayload(params, "thread.status", "status.type")), "inProgress")
		return normalized, normalized.ThreadID != "" || normalized.TurnStatus != ""
	case "item/started", "item/updated", "item/completed":
		return normalizeItemEvent(method, params, thread, normalized)
	default:
		return NormalizedLiveEvent{}, false
	}
}

func normalizeItemEvent(method string, params map[string]any, thread model.Thread, normalized NormalizedLiveEvent) (NormalizedLiveEvent, bool) {
	item := asMap(params["item"])
	itemType := strings.TrimSpace(stringValue(item["type"], ""))
	normalized.ItemKind = itemType
	normalized.ItemID = stringValue(item["id"], itemType)

	switch itemType {
	case "commandExecution", "fileChange", "dynamicToolCall", "mcpToolCall", "webSearch":
		normalized.Kind = toolLiveEventKind(method)
		normalized.Label = strings.TrimSpace(toolLabel(item))
		normalized.Output = toolOutput(item)
		normalized.Status = strings.TrimSpace(toolStatus(item, ""))
		if normalized.Status == "" {
			normalized.Status = strings.TrimSpace(stringValue(params["status"], ""))
		}
		if normalized.Status == "" {
			if normalized.Kind == LiveEventToolCompleted {
				normalized.Status = "completed"
			} else {
				normalized.Status = "running"
			}
		}
		return normalized, normalized.ThreadID != "" && (normalized.Label != "" || strings.TrimSpace(normalized.Output) != "")
	case "agentMessage":
		normalized.Kind = LiveEventAgentMessage
		normalized.Text = strings.TrimSpace(stringValue(item["text"], ""))
		normalized.Phase = normalizeAgentMessagePhase(item)
		return normalized, normalized.ThreadID != "" && normalized.Text != ""
	default:
		return NormalizedLiveEvent{}, false
	}
}

func toolLiveEventKind(method string) LiveEventKind {
	switch method {
	case "item/completed":
		return LiveEventToolCompleted
	case "item/updated":
		return LiveEventToolUpdated
	default:
		return LiveEventToolStarted
	}
}

func normalizeServerRequestEvent(event Event, thread model.Thread, normalized NormalizedLiveEvent) (NormalizedLiveEvent, bool) {
	method := strings.TrimSpace(strings.ToLower(event.Method))
	params := event.Params
	normalized.ThreadID = firstString(nestedThreadID(params), thread.ID)
	normalized.ThreadTitle = thread.Title
	normalized.ProjectName = thread.ProjectName
	normalized.TurnID = firstString(stringValue(params["turnId"], ""), nestedTurnID(params), thread.ActiveTurnID)
	normalized.ItemID = stringValue(params["itemId"], "")
	normalized.RequestID = rpcString(event.ID)
	switch {
	case strings.Contains(method, "requestapproval"):
		normalized.Kind = LiveEventApprovalRequest
	case strings.Contains(method, "input") || strings.Contains(method, "elicitation"):
		normalized.Kind = LiveEventInputRequest
	default:
		return NormalizedLiveEvent{}, false
	}
	return normalized, normalized.ThreadID != "" || normalized.RequestID != ""
}

func normalizeLegacyCodexEvent(event Event, thread model.Thread, normalized NormalizedLiveEvent) (NormalizedLiveEvent, bool) {
	msg := asMap(event.Params["msg"])
	if len(msg) == 0 {
		msg = event.Params
	}
	msgType := strings.TrimSpace(stringValue(msg["type"], ""))
	normalized.ThreadID = firstString(nestedThreadID(msg), nestedThreadID(event.Params), thread.ID)
	normalized.ThreadTitle = thread.Title
	normalized.ProjectName = thread.ProjectName
	normalized.TurnID = firstString(stringValue(msg["turn_id"], ""), stringValue(msg["turnId"], ""), thread.ActiveTurnID)
	switch msgType {
	case "task_started":
		normalized.Kind = LiveEventLegacyTaskStarted
		normalized.TurnStatus = "inProgress"
	case "task_complete":
		normalized.Kind = LiveEventLegacyTaskComplete
		normalized.TurnStatus = "completed"
	case "turn_aborted":
		normalized.Kind = LiveEventTurnCompleted
		normalized.TurnStatus = "interrupted"
	case "exec_command_begin":
		normalized.Kind = LiveEventToolStarted
		normalized.ItemKind = "commandExecution"
		normalized.ItemID = firstString(stringValue(msg["call_id"], ""), stringValue(msg["callId"], ""), "commandExecution")
		normalized.Label = legacyCommandLabel(msg["command"])
		normalized.Status = firstString(stringValue(msg["status"], ""), "running")
	case "exec_command_end":
		normalized.Kind = LiveEventToolCompleted
		normalized.ItemKind = "commandExecution"
		normalized.ItemID = firstString(stringValue(msg["call_id"], ""), stringValue(msg["callId"], ""), "commandExecution")
		normalized.Label = legacyCommandLabel(msg["command"])
		normalized.Output = firstString(stringValue(msg["output"], ""), stringValue(msg["error"], ""))
		normalized.Status = firstString(stringValue(msg["status"], ""), "completed")
	case "agent_message":
		normalized.Kind = LiveEventAgentMessage
		normalized.Text = strings.TrimSpace(stringValue(msg["message"], ""))
	default:
		return NormalizedLiveEvent{}, false
	}
	return normalized, normalized.ThreadID != "" || normalized.TurnID != "" || normalized.Label != "" || normalized.Text != ""
}

func (e NormalizedLiveEvent) ToolSnapshot(thread model.Thread) (ThreadReadSnapshot, bool) {
	switch e.Kind {
	case LiveEventToolStarted, LiveEventToolUpdated, LiveEventToolCompleted:
	default:
		return ThreadReadSnapshot{}, false
	}
	threadID := firstString(e.ThreadID, thread.ID)
	if threadID == "" {
		return ThreadReadSnapshot{}, false
	}
	label := strings.TrimSpace(e.Label)
	output := e.Output
	if label == "" && strings.TrimSpace(output) == "" {
		return ThreadReadSnapshot{}, false
	}
	status := strings.TrimSpace(e.Status)
	if status == "" {
		if e.Kind == LiveEventToolCompleted {
			status = "completed"
		} else {
			status = "running"
		}
	}
	turnID := strings.TrimSpace(e.TurnID)
	turnStatus := "inProgress"
	if terminalLatestToolStatus(status) {
		turnStatus = firstString(strings.TrimSpace(e.TurnStatus), "inProgress")
	}
	progressText := label
	if progressText == "" {
		progressText = strings.TrimSpace(output)
	}
	itemKind := firstString(e.ItemKind, "tool")
	toolID := firstString(e.ItemID, itemKind)
	snapshot := ThreadReadSnapshot{
		Thread: model.Thread{
			ID:           threadID,
			Title:        firstString(thread.Title, e.ThreadTitle, threadID),
			ProjectName:  firstString(thread.ProjectName, e.ProjectName),
			Status:       "inProgress",
			ActiveTurnID: turnID,
		},
		LatestTurnID:       turnID,
		LatestTurnStatus:   turnStatus,
		LatestProgressText: progressText,
		LatestToolID:       toolID,
		LatestToolKind:     itemKind,
		LatestToolLabel:    label,
		LatestToolStatus:   status,
		LatestToolOutput:   output,
	}
	snapshot.LatestProgressFP = fingerprint("live-progress", itemKind, toolID, progressText, status, output)
	snapshot.LatestToolFP = fingerprint("live-tool", itemKind, toolID, label, status, output)
	snapshot.DetailItems = liveToolDetailItems(snapshot)
	return snapshot, true
}

func nestedTurnID(payload map[string]any) string {
	turn := asMap(payload["turn"])
	return stringValue(turn["id"], "")
}

func firstPayload(payload map[string]any, keys ...string) any {
	for _, key := range keys {
		value := nestedPayloadValue(payload, key)
		if value != nil {
			return value
		}
	}
	return nil
}

func nestedPayloadValue(payload map[string]any, key string) any {
	parts := strings.Split(key, ".")
	var current any = payload
	for _, part := range parts {
		currentMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = currentMap[part]
	}
	return current
}

func legacyCommandLabel(value any) string {
	switch typed := value.(type) {
	case []any:
		parts := make([]string, 0, len(typed))
		for _, part := range typed {
			if text := strings.TrimSpace(fmt.Sprint(part)); text != "" && text != "<nil>" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	default:
		return strings.TrimSpace(stringValue(value, ""))
	}
}
