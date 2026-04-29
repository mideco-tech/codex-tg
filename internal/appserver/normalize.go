package appserver

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mideco-tech/codex-tg/internal/model"
)

type ThreadReadSnapshot struct {
	Thread                    model.Thread
	LatestTurnID              string
	LatestTurnStatus          string
	WaitingOnApproval         bool
	WaitingOnReply            bool
	LatestAgentMessages       []string
	LatestAgentMessageEntries []AgentMessageEntry
	LatestProgressFP          string
	LatestProgressText        string
	LatestUserMessageID       string
	LatestUserMessageText     string
	LatestUserMessageFP       string
	LatestFinalFP             string
	LatestFinalText           string
	LatestToolID              string
	LatestToolKind            string
	LatestToolLabel           string
	LatestToolStatus          string
	LatestToolOutput          string
	LatestToolFP              string
	LatestToolSource          string
	SessionTailActiveTool     bool
	PlanPrompt                *model.PlanPrompt
	DetailItems               []model.DetailItem
}

type AgentMessageEntry struct {
	ID    string `json:"id,omitempty"`
	Phase string `json:"phase,omitempty"`
	Text  string `json:"text,omitempty"`
	FP    string `json:"fp,omitempty"`
}

func ThreadsFromList(result map[string]any) []model.Thread {
	items := iterThreadItems(result)
	out := make([]model.Thread, 0, len(items))
	for _, item := range items {
		out = append(out, ThreadFromPayload(item))
	}
	return out
}

func ThreadFromPayload(payload map[string]any) model.Thread {
	threadPayload := payload
	if nested, ok := payload["thread"].(map[string]any); ok && nested != nil {
		threadPayload = nested
	}
	id := stringValue(threadPayload["id"], stringValue(threadPayload["threadId"], ""))
	cwd := stringValue(threadPayload["cwd"], "")
	project, directory := model.ProjectNameFromCWD(cwd)
	title := stringValue(threadPayload["name"], stringValue(threadPayload["title"], id))
	preview := stringValue(threadPayload["preview"], "")
	status := statusText(threadPayload["status"])
	preferredModel := stringValue(threadPayload["model"], stringValue(payload["model"], ""))
	raw, _ := json.Marshal(threadPayload)
	updatedAt := int64Value(threadPayload["updatedAt"])
	activeTurnID := stringValue(threadPayload["activeTurnId"], "")
	if activeTurnID == "" {
		if turns, ok := threadPayload["turns"].([]any); ok && len(turns) > 0 {
			if lastTurn, ok := turns[len(turns)-1].(map[string]any); ok {
				turnStatus := statusText(lastTurn["status"])
				if strings.EqualFold(turnStatus, "inProgress") || statusHasFlag(turnStatus, "active") {
					activeTurnID = stringValue(lastTurn["id"], "")
				}
				if preview == "" {
					preview = previewFromTurn(lastTurn)
				}
			}
		}
	}
	return model.Thread{
		ID:             id,
		Title:          title,
		CWD:            cwd,
		ProjectName:    project,
		DirectoryName:  directory,
		UpdatedAt:      updatedAt,
		Status:         status,
		LastPreview:    preview,
		ActiveTurnID:   activeTurnID,
		PreferredModel: preferredModel,
		Archived:       false,
		Raw:            raw,
	}
}

func SnapshotFromThreadRead(result map[string]any) ThreadReadSnapshot {
	thread := ThreadFromPayload(result)
	payload := result
	if nested, ok := result["thread"].(map[string]any); ok && nested != nil {
		payload = nested
	}
	snapshot := ThreadReadSnapshot{Thread: thread}
	if statusHasFlag(thread.Status, "waitingOnApproval") {
		snapshot.WaitingOnApproval = true
	}
	if statusHasFlag(thread.Status, "waitingOnUserInput") || statusHasFlag(thread.Status, "waitingOnInput") {
		snapshot.WaitingOnReply = true
	}
	turns, _ := payload["turns"].([]any)
	if len(turns) == 0 {
		return snapshot
	}
	lastTurn, _ := turns[len(turns)-1].(map[string]any)
	snapshot.LatestTurnID = stringValue(lastTurn["id"], "")
	snapshot.LatestTurnStatus = statusText(lastTurn["status"])
	if statusHasFlag(snapshot.LatestTurnStatus, "waitingOnApproval") {
		snapshot.WaitingOnApproval = true
	}
	if statusHasFlag(snapshot.LatestTurnStatus, "waitingOnUserInput") || statusHasFlag(snapshot.LatestTurnStatus, "waitingOnInput") {
		snapshot.WaitingOnReply = true
	}
	items, _ := lastTurn["items"].([]any)
	snapshot.LatestAgentMessages = collectAgentMessagesFromItems(items, 3)
	snapshot.LatestAgentMessageEntries = collectAgentMessageEntriesFromItems(items, 3)
	snapshot.DetailItems = collectDetailItemsFromItems(items, snapshot.LatestTurnStatus)
	snapshot.PlanPrompt = syntheticPlanPrompt(snapshot.Thread, snapshot.LatestTurnID, items, snapshot.WaitingOnReply)
	userID, userText, userFP := latestUserMessage(items)
	snapshot.LatestUserMessageID = userID
	snapshot.LatestUserMessageText = userText
	snapshot.LatestUserMessageFP = userFP
	finalText, finalFP := latestFinalAgentMessage(items)
	snapshot.LatestFinalText = finalText
	snapshot.LatestFinalFP = finalFP
	for i := len(items) - 1; i >= 0; i-- {
		item, _ := items[i].(map[string]any)
		itemType := strings.TrimSpace(stringValue(item["type"], ""))
		switch itemType {
		case "plan":
			text := strings.TrimSpace(renderItemText(item))
			if text != "" && snapshot.LatestProgressFP == "" {
				snapshot.LatestProgressFP = fingerprint(itemType, stringValue(item["id"], ""), text)
				snapshot.LatestProgressText = text
			}
		case "commandExecution", "fileChange", "dynamicToolCall", "mcpToolCall", "webSearch":
			text := strings.TrimSpace(renderItemText(item))
			if text != "" && snapshot.LatestProgressFP == "" {
				snapshot.LatestProgressFP = fingerprint(itemType, stringValue(item["id"], ""), text)
				snapshot.LatestProgressText = text
			}
			if snapshot.LatestToolFP == "" {
				snapshot.LatestToolID = stringValue(item["id"], "")
				snapshot.LatestToolKind = itemType
				snapshot.LatestToolLabel = toolLabel(item)
				snapshot.LatestToolStatus = toolStatus(item, snapshot.LatestTurnStatus)
				snapshot.LatestToolOutput = toolOutput(item)
				snapshot.LatestToolFP = fingerprint(itemType, snapshot.LatestToolID, snapshot.LatestToolLabel, snapshot.LatestToolStatus, snapshot.LatestToolOutput)
			}
		}
	}
	return snapshot
}

func DiffSnapshot(previous *model.ThreadSnapshotState, current ThreadReadSnapshot) []model.ObserverEvent {
	events := []model.ObserverEvent{}
	turnID := current.LatestTurnID
	thread := current.Thread
	if previous == nil {
		return events
	}
	if turnID != "" && previous.LastSeenTurnID != turnID {
		events = append(events, model.ObserverEvent{
			EventID:     fmt.Sprintf("poll:%s:%s:started", thread.ID, turnID),
			Kind:        "turn_started",
			ThreadID:    thread.ID,
			ProjectName: thread.ProjectName,
			ThreadTitle: thread.Title,
			Text:        "Turn started",
			Status:      current.LatestTurnStatus,
			TurnID:      turnID,
		})
	}
	if current.LatestProgressFP != "" && current.LatestProgressFP != previous.LastProgressFP {
		events = append(events, model.ObserverEvent{
			EventID:     fmt.Sprintf("poll:%s:%s:progress:%s", thread.ID, turnID, current.LatestProgressFP),
			Kind:        "tool_activity",
			ThreadID:    thread.ID,
			ProjectName: thread.ProjectName,
			ThreadTitle: thread.Title,
			Text:        current.LatestProgressText,
			Status:      current.LatestTurnStatus,
			TurnID:      turnID,
		})
	}
	if current.WaitingOnApproval && previous.LastApprovalFP != turnID {
		events = append(events, model.ObserverEvent{
			EventID:       fmt.Sprintf("poll:%s:%s:approval", thread.ID, turnID),
			Kind:          "thread_updated",
			ThreadID:      thread.ID,
			ProjectName:   thread.ProjectName,
			ThreadTitle:   thread.Title,
			Text:          "Thread is waiting for approval.",
			Status:        thread.Status,
			TurnID:        turnID,
			NeedsApproval: true,
		})
	}
	if current.WaitingOnReply && previous.LastSeenThreadStatus != thread.Status {
		replyFP := current.LatestTurnID
		if current.PlanPrompt != nil && current.PlanPrompt.Fingerprint != "" {
			replyFP = current.PlanPrompt.Fingerprint
		}
		events = append(events, model.ObserverEvent{
			EventID:     fmt.Sprintf("poll:%s:%s:reply:%s", thread.ID, turnID, replyFP),
			Kind:        "thread_updated",
			ThreadID:    thread.ID,
			ProjectName: thread.ProjectName,
			ThreadTitle: thread.Title,
			Text:        waitingText(current.PlanPrompt),
			Status:      thread.Status,
			TurnID:      turnID,
			NeedsReply:  true,
		})
	}
	if current.LatestFinalFP != "" && current.LatestFinalFP != previous.LastFinalFP {
		events = append(events, model.ObserverEvent{
			EventID:     fmt.Sprintf("poll:%s:%s:final:%s", thread.ID, turnID, current.LatestFinalFP),
			Kind:        "final_answer",
			ThreadID:    thread.ID,
			ProjectName: thread.ProjectName,
			ThreadTitle: thread.Title,
			Text:        current.LatestFinalText,
			Status:      current.LatestTurnStatus,
			TurnID:      turnID,
		})
	}
	if turnID != "" && current.LatestTurnStatus != "" {
		completionFP := fingerprint(turnID, current.LatestTurnStatus)
		switch current.LatestTurnStatus {
		case "completed", "interrupted":
			if completionFP != previous.LastCompletionFP {
				events = append(events, model.ObserverEvent{
					EventID:     fmt.Sprintf("poll:%s:%s:%s", thread.ID, turnID, current.LatestTurnStatus),
					Kind:        "turn_completed",
					ThreadID:    thread.ID,
					ProjectName: thread.ProjectName,
					ThreadTitle: thread.Title,
					Text:        "Turn " + current.LatestTurnStatus,
					Status:      current.LatestTurnStatus,
					TurnID:      turnID,
				})
			}
		case "failed":
			if completionFP != previous.LastCompletionFP {
				events = append(events, model.ObserverEvent{
					EventID:     fmt.Sprintf("poll:%s:%s:failed", thread.ID, turnID),
					Kind:        "turn_failed",
					ThreadID:    thread.ID,
					ProjectName: thread.ProjectName,
					ThreadTitle: thread.Title,
					Text:        "Turn failed",
					Status:      current.LatestTurnStatus,
					TurnID:      turnID,
				})
			}
		}
	}
	return events
}

func CompactSnapshot(previous *model.ThreadSnapshotState, current ThreadReadSnapshot, polledAt time.Time) model.ThreadSnapshotState {
	out := model.ThreadSnapshotState{
		ThreadUpdatedAt:      current.Thread.UpdatedAt,
		LastSeenThreadStatus: current.Thread.Status,
		LastSeenTurnID:       current.LatestTurnID,
		LastSeenTurnStatus:   current.LatestTurnStatus,
		LastProgressFP:       current.LatestProgressFP,
		LastFinalFP:          current.LatestFinalFP,
		LastPollAt:           model.TimeString(polledAt.UTC().Format(time.RFC3339Nano)),
	}
	if previous != nil {
		out.LastRichLiveEventAt = previous.LastRichLiveEventAt
		out.LastProgressSentAt = previous.LastProgressSentAt
		out.LastCompletionFP = previous.LastCompletionFP
		out.LastFinalNoticeFP = previous.LastFinalNoticeFP
		out.LastToolDocumentFP = previous.LastToolDocumentFP
	}
	if current.LatestTurnID != "" && current.LatestTurnStatus != "" {
		switch current.LatestTurnStatus {
		case "completed", "interrupted", "failed":
			out.LastCompletionFP = fingerprint(current.LatestTurnID, current.LatestTurnStatus)
		}
	}
	if current.WaitingOnApproval {
		out.LastApprovalFP = current.LatestTurnID
	}
	if current.WaitingOnReply && current.PlanPrompt != nil {
		out.LastReplyFP = current.PlanPrompt.Fingerprint
	}
	raw, _ := json.Marshal(current)
	out.CompactJSON = raw
	return out
}

func NormalizeLiveNotification(event Event, thread model.Thread) []model.ObserverEvent {
	method := strings.TrimSpace(strings.ToLower(event.Method))
	params := event.Params
	threadID := nestedThreadID(params)
	if threadID == "" {
		threadID = thread.ID
	}
	if threadID == "" {
		return nil
	}
	projectName := thread.ProjectName
	threadTitle := thread.Title
	if projectName == "" {
		projectName = stringValue(params["projectName"], "")
	}
	if threadTitle == "" {
		threadTitle = stringValue(params["threadTitle"], threadID)
	}
	switch method {
	case "turn/started":
		turn := asMap(params["turn"])
		turnID := stringValue(turn["id"], stringValue(params["turnId"], ""))
		return []model.ObserverEvent{{
			EventID:     fmt.Sprintf("live:%s:%s:started", threadID, turnID),
			Kind:        "turn_started",
			ThreadID:    threadID,
			ProjectName: projectName,
			ThreadTitle: threadTitle,
			Text:        "Turn started",
			Status:      stringValue(turn["status"], ""),
			TurnID:      turnID,
		}}
	case "turn/completed":
		turn := asMap(params["turn"])
		turnID := stringValue(turn["id"], stringValue(params["turnId"], ""))
		status := stringValue(turn["status"], stringValue(params["status"], "completed"))
		kind := "turn_completed"
		text := "Turn " + status
		if status == "failed" {
			kind = "turn_failed"
			text = "Turn failed"
		}
		return []model.ObserverEvent{{
			EventID:     fmt.Sprintf("live:%s:%s:%s", threadID, turnID, status),
			Kind:        kind,
			ThreadID:    threadID,
			ProjectName: projectName,
			ThreadTitle: threadTitle,
			Text:        text,
			Status:      status,
			TurnID:      turnID,
		}}
	case "turn/plan/updated":
		turnID := stringValue(params["turnId"], "")
		text := strings.TrimSpace(stringValue(params["text"], stringValue(params["plan"], "Plan updated")))
		return []model.ObserverEvent{{
			EventID:     fmt.Sprintf("live:%s:%s:plan:%s", threadID, turnID, fingerprint(text)),
			Kind:        "thread_updated",
			ThreadID:    threadID,
			ProjectName: projectName,
			ThreadTitle: threadTitle,
			Text:        text,
			Status:      "",
			TurnID:      turnID,
		}}
	case "item/completed":
		item := asMap(params["item"])
		itemType := strings.TrimSpace(stringValue(item["type"], ""))
		turnID := stringValue(params["turnId"], "")
		text := strings.TrimSpace(renderItemText(item))
		if itemType == "agentMessage" && text != "" {
			phase := strings.ToLower(normalizeAgentMessagePhase(item))
			if phase != "" && phase != "final_answer" {
				return nil
			}
			return []model.ObserverEvent{{
				EventID:     fmt.Sprintf("live:%s:%s:%s", threadID, turnID, stringValue(item["id"], "agent")),
				Kind:        "final_answer",
				ThreadID:    threadID,
				ProjectName: projectName,
				ThreadTitle: threadTitle,
				Text:        text,
				TurnID:      turnID,
				ItemID:      stringValue(item["id"], ""),
			}}
		}
		if itemType == "plan" && text != "" {
			return []model.ObserverEvent{{
				EventID:     fmt.Sprintf("live:%s:%s:%s", threadID, turnID, stringValue(item["id"], "plan")),
				Kind:        "thread_updated",
				ThreadID:    threadID,
				ProjectName: projectName,
				ThreadTitle: threadTitle,
				Text:        text,
				TurnID:      turnID,
				ItemID:      stringValue(item["id"], ""),
			}}
		}
		if text != "" {
			return []model.ObserverEvent{{
				EventID:     fmt.Sprintf("live:%s:%s:%s", threadID, turnID, stringValue(item["id"], itemType)),
				Kind:        "tool_activity",
				ThreadID:    threadID,
				ProjectName: projectName,
				ThreadTitle: threadTitle,
				Text:        text,
				TurnID:      turnID,
				ItemID:      stringValue(item["id"], ""),
			}}
		}
	case "thread/status/changed":
		status := statusText(params["status"])
		if statusHasFlag(status, "waitingOnApproval") {
			return []model.ObserverEvent{{
				EventID:       fmt.Sprintf("live:%s:status:%s", threadID, status),
				Kind:          "thread_updated",
				ThreadID:      threadID,
				ProjectName:   projectName,
				ThreadTitle:   threadTitle,
				Text:          "Thread is waiting for approval.",
				Status:        status,
				NeedsApproval: true,
			}}
		}
		if statusHasFlag(status, "waitingOnUserInput") || statusHasFlag(status, "waitingOnInput") {
			return []model.ObserverEvent{{
				EventID:     fmt.Sprintf("live:%s:status:%s", threadID, status),
				Kind:        "thread_updated",
				ThreadID:    threadID,
				ProjectName: projectName,
				ThreadTitle: threadTitle,
				Text:        "Thread is waiting for input.",
				Status:      status,
				NeedsReply:  true,
			}}
		}
	}
	return nil
}

func PendingApprovalFromServerRequest(event Event) (*model.PendingApproval, bool) {
	if event.Channel != "server_request" {
		return nil, false
	}
	method := strings.ToLower(strings.TrimSpace(event.Method))
	requestID := fmt.Sprintf("%v", event.ID)
	if requestID == "" {
		return nil, false
	}
	threadID := nestedThreadID(event.Params)
	turnID := stringValue(event.Params["turnId"], "")
	itemID := stringValue(event.Params["itemId"], "")
	switch {
	case strings.Contains(method, "requestapproval"):
		return &model.PendingApproval{
			RequestID:   requestID,
			ThreadID:    threadID,
			TurnID:      turnID,
			ItemID:      itemID,
			PromptKind:  "approval",
			Question:    stringValue(event.Params["question"], "Approval required."),
			Status:      "pending",
			PayloadJSON: model.MustJSON(event.Params),
			UpdatedAt:   model.NowString(),
		}, true
	case strings.Contains(method, "requestuserinput"):
		return &model.PendingApproval{
			RequestID:   requestID,
			ThreadID:    threadID,
			TurnID:      turnID,
			ItemID:      itemID,
			PromptKind:  "user_input",
			Question:    requestUserInputQuestion(event.Params),
			Status:      "pending",
			PayloadJSON: model.MustJSON(event.Params),
			UpdatedAt:   model.NowString(),
		}, true
	}
	return nil, false
}

func iterThreadItems(payload map[string]any) []map[string]any {
	if items, ok := payload["data"].([]any); ok {
		return anyMaps(items)
	}
	if items, ok := payload["items"].([]any); ok {
		return anyMaps(items)
	}
	if thread, ok := payload["thread"].(map[string]any); ok {
		if items, ok := thread["items"].([]any); ok {
			return anyMaps(items)
		}
	}
	if threads, ok := payload["threads"].([]any); ok {
		return anyMaps(threads)
	}
	if results, ok := payload["results"].([]any); ok {
		return anyMaps(results)
	}
	return nil
}

func anyMaps(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if typed, ok := item.(map[string]any); ok {
			out = append(out, typed)
		}
	}
	return out
}

func nestedThreadID(params map[string]any) string {
	if value := stringValue(params["threadId"], ""); value != "" {
		return value
	}
	if thread, ok := params["thread"].(map[string]any); ok {
		if value := stringValue(thread["id"], ""); value != "" {
			return value
		}
		if value := stringValue(thread["threadId"], ""); value != "" {
			return value
		}
	}
	return ""
}

func statusText(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	if typed, ok := value.(map[string]any); ok {
		statusType := stringValue(typed["type"], "")
		flags := stringSliceValue(typed["activeFlags"])
		if statusType == "active" && len(flags) > 0 {
			return fmt.Sprintf("active[%s]", strings.Join(flags, ","))
		}
		return statusType
	}
	return ""
}

func statusHasFlag(status, flag string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	flag = strings.ToLower(strings.TrimSpace(flag))
	if status == "" || flag == "" {
		return false
	}
	return status == flag || strings.Contains(status, flag)
}

func previewFromTurn(turn map[string]any) string {
	items, _ := turn["items"].([]any)
	for i := len(items) - 1; i >= 0; i-- {
		item, _ := items[i].(map[string]any)
		if text := renderItemText(item); text != "" {
			return text
		}
	}
	return ""
}

func renderItemText(item map[string]any) string {
	itemType := stringValue(item["type"], "")
	switch itemType {
	case "agentMessage":
		return strings.TrimSpace(stringValue(item["text"], ""))
	case "userMessage":
		return strings.TrimSpace(userMessageText(item))
	case "plan":
		return strings.TrimSpace(planText(item))
	case "commandExecution":
		command := strings.TrimSpace(stringValue(item["command"], ""))
		status := strings.TrimSpace(stringValue(item["status"], ""))
		output := strings.TrimSpace(stringValue(item["aggregatedOutput"], ""))
		if output != "" {
			line := output
			if idx := strings.Index(line, "\n"); idx >= 0 {
				line = line[:idx]
			}
			if status != "" && command != "" {
				return fmt.Sprintf("%s: %s\nOutput: %s", status, command, strings.TrimSpace(line))
			}
			return strings.TrimSpace(line)
		}
		if status != "" && command != "" {
			return fmt.Sprintf("%s: %s", status, command)
		}
		return command
	case "fileChange":
		path := strings.TrimSpace(stringValue(item["path"], ""))
		if path != "" {
			return "File changed: " + path
		}
	}
	return strings.TrimSpace(stringValue(item["text"], ""))
}

func collectAgentMessages(turns []any, limit int) []string {
	entries := collectAgentMessageEntries(turns, limit)
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Text == "" {
			continue
		}
		out = append(out, entry.Text)
	}
	return out
}

func collectAgentMessagesFromItems(items []any, limit int) []string {
	entries := collectAgentMessageEntriesFromItems(items, limit)
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Text == "" {
			continue
		}
		out = append(out, entry.Text)
	}
	return out
}

func collectAgentMessageEntries(turns []any, limit int) []AgentMessageEntry {
	if limit <= 0 {
		return nil
	}
	out := make([]AgentMessageEntry, 0, limit)
	for turnIndex := len(turns) - 1; turnIndex >= 0 && len(out) < limit; turnIndex-- {
		turn, _ := turns[turnIndex].(map[string]any)
		out = append(out, collectAgentMessageEntriesFromItems(turn["items"], limit-len(out))...)
	}
	return out
}

func collectAgentMessageEntriesFromItems(raw any, limit int) []AgentMessageEntry {
	if limit <= 0 {
		return nil
	}
	items, _ := raw.([]any)
	out := make([]AgentMessageEntry, 0, min(limit, len(items)))
	for itemIndex := len(items) - 1; itemIndex >= 0 && len(out) < limit; itemIndex-- {
		item, _ := items[itemIndex].(map[string]any)
		itemType := strings.TrimSpace(stringValue(item["type"], ""))
		if itemType != "agentMessage" && itemType != "plan" {
			continue
		}
		text := strings.TrimSpace(userMessageText(item))
		if itemType == "plan" {
			text = strings.TrimSpace(planText(item))
		}
		if text == "" {
			continue
		}
		phase := normalizeAgentMessagePhase(item)
		if itemType == "plan" && phase == "" {
			phase = "plan"
		}
		entry := AgentMessageEntry{
			ID:    stringValue(item["id"], ""),
			Phase: phase,
			Text:  text,
		}
		entry.FP = fingerprint("agentMessage", entry.ID, entry.Phase, entry.Text)
		out = append(out, entry)
	}
	return out
}

func latestFinalAgentMessage(items []any) (string, string) {
	entries := collectAgentMessageEntriesFromItems(items, len(items))
	if len(entries) == 0 {
		return "", ""
	}
	hasPhase := false
	for _, entry := range entries {
		phase := strings.ToLower(strings.TrimSpace(entry.Phase))
		if phase != "" {
			hasPhase = true
		}
		if phase == "final_answer" {
			return entry.Text, entry.FP
		}
	}
	if hasPhase {
		return "", ""
	}
	for _, entry := range entries {
		if entry.Text != "" {
			return entry.Text, fingerprint("agentMessage", entry.ID, entry.Text)
		}
	}
	return "", ""
}

func latestUserMessage(items []any) (string, string, string) {
	for itemIndex := len(items) - 1; itemIndex >= 0; itemIndex-- {
		item, _ := items[itemIndex].(map[string]any)
		if strings.TrimSpace(stringValue(item["type"], "")) != "userMessage" {
			continue
		}
		text := strings.TrimSpace(userMessageText(item))
		if text == "" {
			continue
		}
		id := stringValue(item["id"], "")
		return id, text, fingerprint("userMessage", id, text)
	}
	return "", "", ""
}

func collectDetailItemsFromItems(items []any, turnStatus string) []model.DetailItem {
	out := make([]model.DetailItem, 0, len(items))
	commentaryIndex := 0
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		itemType := strings.TrimSpace(stringValue(item["type"], ""))
		itemID := stringValue(item["id"], "")
		switch itemType {
		case "userMessage":
			text := strings.TrimSpace(userMessageText(item))
			if text == "" {
				continue
			}
			out = append(out, model.DetailItem{
				ID:   itemID,
				Kind: model.DetailItemUser,
				Text: text,
				FP:   fingerprint("detail", itemType, itemID, text),
			})
		case "agentMessage":
			text := strings.TrimSpace(stringValue(item["text"], ""))
			if text == "" {
				continue
			}
			phase := normalizeAgentMessagePhase(item)
			kind := model.DetailItemCommentary
			if strings.EqualFold(phase, "final_answer") {
				kind = model.DetailItemFinal
			} else {
				commentaryIndex++
			}
			index := commentaryIndex
			if kind == model.DetailItemFinal {
				index = commentaryIndex
			}
			out = append(out, model.DetailItem{
				ID:              itemID,
				Kind:            kind,
				Phase:           phase,
				Text:            text,
				FP:              fingerprint("detail", itemType, itemID, phase, text),
				CommentaryIndex: index,
			})
		case "plan":
			text := strings.TrimSpace(planText(item))
			if text == "" {
				continue
			}
			commentaryIndex++
			out = append(out, model.DetailItem{
				ID:              itemID,
				Kind:            model.DetailItemPlan,
				Phase:           "plan",
				Text:            text,
				FP:              fingerprint("detail", itemType, itemID, text),
				CommentaryIndex: commentaryIndex,
			})
		case "commandExecution", "fileChange", "dynamicToolCall", "mcpToolCall", "webSearch":
			label := strings.TrimSpace(toolLabel(item))
			status := strings.TrimSpace(toolStatus(item, turnStatus))
			output := toolOutput(item)
			if label == "" && strings.TrimSpace(output) == "" {
				continue
			}
			toolFP := fingerprint("detail", itemType, itemID, label, status)
			out = append(out, model.DetailItem{
				ID:              itemID,
				Kind:            model.DetailItemTool,
				Label:           label,
				Status:          status,
				FP:              toolFP,
				CommentaryIndex: commentaryIndex,
			})
			if strings.TrimSpace(output) != "" {
				out = append(out, model.DetailItem{
					ID:              itemID + ":output",
					Kind:            model.DetailItemOutput,
					Output:          output,
					FP:              fingerprint("detail-output", itemType, itemID, output),
					CommentaryIndex: commentaryIndex,
				})
			}
		}
	}
	return out
}

func normalizeAgentMessagePhase(item map[string]any) string {
	return strings.TrimSpace(stringValue(item["phase"], ""))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func toolLabel(item map[string]any) string {
	itemType := strings.TrimSpace(stringValue(item["type"], ""))
	switch itemType {
	case "commandExecution":
		if command := strings.TrimSpace(stringValue(item["command"], "")); command != "" {
			return command
		}
	case "fileChange":
		if path := strings.TrimSpace(stringValue(item["path"], "")); path != "" {
			return "File changed: " + path
		}
	}
	if text := strings.TrimSpace(renderItemText(item)); text != "" {
		return text
	}
	return itemType
}

func toolStatus(item map[string]any, turnStatus string) string {
	if status := strings.TrimSpace(stringValue(item["status"], "")); status != "" {
		return status
	}
	return strings.TrimSpace(turnStatus)
}

func toolOutput(item map[string]any) string {
	if output := stringValue(item["aggregatedOutput"], ""); strings.TrimSpace(output) != "" {
		return output
	}
	if text := renderItemText(item); strings.TrimSpace(text) != "" {
		return text
	}
	return ""
}

func syntheticPlanPrompt(thread model.Thread, turnID string, items []any, waiting bool) *model.PlanPrompt {
	if !waiting {
		return nil
	}
	itemID, question := latestPlanQuestion(items)
	if question == "" {
		question = strings.TrimSpace(thread.LastPreview)
	}
	if question == "" {
		question = "Input required."
	}
	options := latestChoiceOptions(items)
	fp := fingerprint("planPrompt", model.PromptSourceSyntheticPoll, thread.ID, turnID, itemID, question, strings.Join(options, "\x1f"))
	return &model.PlanPrompt{
		PromptID:    "synthetic:" + thread.ID + ":" + turnID + ":" + fp[:12],
		Source:      model.PromptSourceSyntheticPoll,
		ThreadID:    thread.ID,
		TurnID:      turnID,
		ItemID:      itemID,
		Question:    question,
		Options:     options,
		Fingerprint: fp,
		Status:      "waiting for input",
	}
}

func waitingText(prompt *model.PlanPrompt) string {
	if prompt != nil && strings.TrimSpace(prompt.Question) != "" {
		return prompt.Question
	}
	return "Thread is waiting for input."
}

func latestPlanQuestion(items []any) (string, string) {
	for itemIndex := len(items) - 1; itemIndex >= 0; itemIndex-- {
		item, _ := items[itemIndex].(map[string]any)
		if item == nil {
			continue
		}
		itemType := strings.TrimSpace(stringValue(item["type"], ""))
		switch itemType {
		case "agentMessage":
			phase := strings.ToLower(strings.TrimSpace(normalizeAgentMessagePhase(item)))
			if phase == "final_answer" {
				continue
			}
			if text := strings.TrimSpace(userMessageText(item)); text != "" {
				return stringValue(item["id"], ""), text
			}
		case "plan":
			if text := strings.TrimSpace(planText(item)); text != "" {
				return stringValue(item["id"], ""), text
			}
		}
	}
	for itemIndex := len(items) - 1; itemIndex >= 0; itemIndex-- {
		item, _ := items[itemIndex].(map[string]any)
		if item == nil || strings.TrimSpace(stringValue(item["type"], "")) != "userMessage" {
			continue
		}
		text := strings.TrimSpace(userMessageText(item))
		if strings.Contains(text, "?") {
			return stringValue(item["id"], ""), text
		}
	}
	return "", ""
}

func latestChoiceOptions(items []any) []string {
	for itemIndex := len(items) - 1; itemIndex >= 0; itemIndex-- {
		item, _ := items[itemIndex].(map[string]any)
		if item == nil {
			continue
		}
		if options := extractChoiceOptions(item); len(options) > 0 {
			return options
		}
	}
	return nil
}

func extractChoiceOptions(payload map[string]any) []string {
	for _, key := range []string{"choices", "options", "suggestions", "responses"} {
		raw, ok := payload[key]
		if !ok {
			continue
		}
		items, ok := raw.([]any)
		if !ok {
			continue
		}
		out := make([]string, 0, len(items))
		for _, item := range items {
			switch typed := item.(type) {
			case string:
				if text := strings.TrimSpace(typed); text != "" {
					out = append(out, text)
				}
			case map[string]any:
				for _, field := range []string{"label", "text", "value"} {
					if text := strings.TrimSpace(fmt.Sprintf("%v", typed[field])); text != "" && text != "<nil>" {
						out = append(out, text)
						break
					}
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func requestUserInputQuestion(params map[string]any) string {
	if text := strings.TrimSpace(stringValue(params["question"], "")); text != "" {
		return text
	}
	questions, ok := params["questions"].([]any)
	if !ok {
		return "Input required."
	}
	parts := make([]string, 0, len(questions))
	for _, raw := range questions {
		question, _ := raw.(map[string]any)
		if question == nil {
			continue
		}
		header := strings.TrimSpace(stringValue(question["header"], ""))
		text := strings.TrimSpace(stringValue(question["question"], ""))
		switch {
		case header != "" && text != "":
			parts = append(parts, header+": "+text)
		case text != "":
			parts = append(parts, text)
		case header != "":
			parts = append(parts, header)
		}
	}
	if len(parts) == 0 {
		return "Input required."
	}
	return strings.Join(parts, "\n")
}

func planText(item map[string]any) string {
	if text := strings.TrimSpace(stringValue(item["text"], "")); text != "" {
		return text
	}
	if text := strings.TrimSpace(stringValue(item["plan"], "")); text != "" {
		return text
	}
	return strings.TrimSpace(userMessageText(item))
}

func userMessageText(item map[string]any) string {
	if text := strings.TrimSpace(stringValue(item["text"], "")); text != "" {
		return text
	}
	content, _ := item["content"].([]any)
	parts := make([]string, 0, len(content))
	for _, raw := range content {
		part, _ := raw.(map[string]any)
		if part == nil {
			continue
		}
		text := strings.TrimSpace(stringValue(part["text"], ""))
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func stringSliceValue(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(fmt.Sprintf("%v", item))
		if text != "" && text != "<nil>" {
			out = append(out, text)
		}
	}
	return out
}

func fingerprint(parts ...string) string {
	hasher := sha1.New()
	for _, part := range parts {
		_, _ = hasher.Write([]byte(part))
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func stringValue(value any, fallback string) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return fallback
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	default:
		return 0
	}
}
