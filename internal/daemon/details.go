package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
	"github.com/mideco-tech/codex-tg/internal/tgformat"
)

const (
	detailsPageSize     = 4
	detailsToolMaxBytes = 2800
	staleDetailsText    = "Details panel is stale. Use /show <thread>."
)

func (s *Service) maybeRenderFinalCard(ctx context.Context, sender Sender, target model.ObserverTarget, panel *model.ThreadPanel, thread model.Thread, snapshot *appserver.ThreadReadSnapshot) error {
	if !isTerminalStatus(snapshot.LatestTurnStatus) || strings.TrimSpace(snapshot.LatestFinalText) == "" {
		return nil
	}
	if panel == nil || snapshot.LatestFinalFP == "" || snapshot.LatestFinalFP == panel.LastFinalNoticeFP {
		return nil
	}

	message, buttons, cardHash := s.renderFinalCard(ctx, panel.ID, thread, snapshot)
	messageIDs, err := sender.SendRenderedMessages(ctx, target.ChatID, target.TopicID, []model.RenderedMessage{message}, buttons, notifySendOptions())
	if err != nil {
		return err
	}
	finalMessageID := lastMessageID(messageIDs)
	if finalMessageID == 0 {
		return fmt.Errorf("telegram final card send returned no message id")
	}
	_ = s.store.PutMessageRoute(ctx, model.MessageRoute{
		ChatID:    target.ChatID,
		TopicID:   target.TopicID,
		MessageID: finalMessageID,
		ThreadID:  thread.ID,
		TurnID:    snapshot.LatestTurnID,
		EventID:   snapshot.LatestFinalFP,
		CreatedAt: model.NowString(),
	})
	oldSummaryMessageID := panel.SummaryMessageID
	panel.SummaryMessageID = finalMessageID
	panel.CurrentTurnID = snapshot.LatestTurnID
	panel.Status = snapshot.LatestTurnStatus
	panel.LastSummaryHash = cardHash
	panel.LastFinalCardHash = cardHash
	panel.LastFinalNoticeFP = snapshot.LatestFinalFP
	panel.DetailsViewJSON = model.MustJSON(model.DetailsViewState{})
	if err := s.store.UpdateThreadPanelFinalCard(ctx, panel.ID, panel.SummaryMessageID, panel.CurrentTurnID, panel.Status, panel.LastSummaryHash, panel.LastToolHash, panel.LastOutputHash, panel.LastFinalNoticeFP, panel.DetailsViewJSON, panel.LastFinalCardHash); err != nil {
		return err
	}
	if oldSummaryMessageID != 0 && oldSummaryMessageID != finalMessageID {
		_ = sender.DeleteMessage(ctx, target.ChatID, target.TopicID, oldSummaryMessageID)
	}
	if panel.RunNoticeMessageID != 0 {
		_ = sender.DeleteMessage(ctx, target.ChatID, target.TopicID, panel.RunNoticeMessageID)
	}
	if panel.ToolMessageID != 0 {
		_ = sender.DeleteMessage(ctx, target.ChatID, target.TopicID, panel.ToolMessageID)
	}
	if panel.OutputMessageID != 0 {
		_ = sender.DeleteMessage(ctx, target.ChatID, target.TopicID, panel.OutputMessageID)
	}
	return nil
}

func (s *Service) renderFinalCard(ctx context.Context, panelID int64, thread model.Thread, snapshot *appserver.ThreadReadSnapshot) (model.RenderedMessage, [][]model.ButtonSpec, string) {
	buttons := [][]model.ButtonSpec{
		{
			s.callbackButton(ctx, "Details", "details_open", thread.ID, snapshot.LatestTurnID, "", map[string]any{
				"panel_id": panelID,
				"page":     0,
			}),
			s.callbackButton(ctx, "Get full log", "get_full_log", thread.ID, snapshot.LatestTurnID, "", nil),
		},
		{
			s.callbackButton(ctx, "Show", "show_thread", thread.ID, snapshot.LatestTurnID, "", nil),
			s.callbackButton(ctx, "Bind here", "bind_here", thread.ID, snapshot.LatestTurnID, "", nil),
		},
		{
			s.callbackButton(ctx, "Get thread id", "get_thread_id", thread.ID, snapshot.LatestTurnID, "", nil),
		},
	}
	header := strings.Join([]string{
		s.visualHeader(ctx, "Final", thread, snapshot.LatestTurnID),
		fmt.Sprintf("Status: %s", readableStatus(snapshot.LatestTurnStatus, thread.Status)),
	}, "\n")
	body := strings.TrimSpace(snapshot.LatestFinalText)
	if line := runTimingFooter(snapshot, time.Now().UTC()); line != "" {
		if body != "" {
			body += "\n\n"
		}
		body += line
	}
	message := renderSingleMarkdownCard(header, body)
	return message, buttons, hashStrings(tgformat.HashRendered(message), flattenButtonSpecs(buttons))
}

func renderSingleMarkdownCard(header, markdown string) model.RenderedMessage {
	body := strings.TrimSpace(cleanTelegramNilLiteral(markdown))
	truncated := false
	for attempts := 0; attempts < 12; attempts++ {
		candidate := body
		if truncated {
			candidate = strings.TrimSpace(candidate) + "\n\n[Trimmed for Telegram. Use Get full log.]"
		}
		messages := tgformat.RenderMarkdownWithHeader(header, candidate)
		if len(messages) <= 1 {
			return firstRenderedMessage(messages)
		}
		runes := []rune(body)
		if len(runes) == 0 {
			break
		}
		next := len(runes) * 3 / 4
		if next <= 0 || next >= len(runes) {
			next = len(runes) - 1
		}
		body = string(runes[:next])
		truncated = true
	}
	text := trimOutputTail(strings.TrimSpace(header+"\n\n"+body+"\n\n[Trimmed for Telegram. Use Get full log.]"), tgformat.TelegramMessageLimit-32)
	return model.RenderedMessage{Text: text}
}

func (s *Service) handleDetailsCallback(ctx context.Context, chatID, topicID, callbackMessageID int64, route *model.CallbackRoute, payload map[string]any) (*DirectResponse, error) {
	panel, response, err := s.detailsPanelForCallback(ctx, chatID, topicID, callbackMessageID, route, payload, true)
	if err != nil {
		return nil, err
	}
	if response != nil {
		return response, nil
	}
	thread, snapshot, err := s.loadThreadPanelSnapshot(ctx, panel.ThreadID)
	if err != nil || thread == nil || snapshot == nil {
		return &DirectResponse{Text: "Could not load thread details. Try /repair or /show <thread>."}, nil
	}
	snapshot, ok := snapshotForPanelTurn(*thread, snapshot, panel)
	if !ok {
		return staleDetailsResponse(), nil
	}
	state := detailsStateFromPayload(payload)
	sectionCount := len(detailSections(snapshot))
	if route != nil {
		switch route.Action {
		case "details_open":
			state = model.DetailsViewState{Page: 0}
		case "details_prev":
			if state.ToolMode {
				state.CommentaryIndex = clampInt(state.CommentaryIndex-1, 1, maxInt(1, sectionCount))
			} else {
				state.Page = clampInt(state.Page-1, 0, maxInt(0, detailsPageCount(sectionCount)-1))
			}
		case "details_next":
			if state.ToolMode {
				state.CommentaryIndex = clampInt(state.CommentaryIndex+1, 1, maxInt(1, sectionCount))
			} else {
				state.Page = clampInt(state.Page+1, 0, maxInt(0, detailsPageCount(sectionCount)-1))
			}
		case "details_tool_toggle":
			state.ToolMode = !state.ToolMode
			if state.ToolMode && state.CommentaryIndex == 0 {
				state.CommentaryIndex = firstCommentaryWithToolsOnPage(snapshot, state.Page, sectionCount)
			}
		case "details_back":
			message, buttons, cardHash := s.renderFinalCard(ctx, panel.ID, *thread, snapshot)
			targetMessageID := callbackMessageID
			if targetMessageID == 0 {
				targetMessageID = panel.SummaryMessageID
			}
			if err := s.editPanelCard(ctx, chatID, topicID, targetMessageID, message, buttons, panel, snapshot, cardHash, model.DetailsViewState{}); err != nil {
				return nil, err
			}
			return &DirectResponse{CallbackText: "Final card."}, nil
		}
	}
	state.Page = clampInt(state.Page, 0, maxInt(0, detailsPageCount(sectionCount)-1))
	if state.ToolMode {
		state.CommentaryIndex = clampInt(state.CommentaryIndex, 1, maxInt(1, sectionCount))
	}
	message, buttons, cardHash := s.renderDetailsCard(ctx, panel.ID, *thread, snapshot, state)
	targetMessageID := callbackMessageID
	if targetMessageID == 0 {
		targetMessageID = panel.SummaryMessageID
	}
	if err := s.editPanelCard(ctx, chatID, topicID, targetMessageID, message, buttons, panel, snapshot, cardHash, state); err != nil {
		return nil, err
	}
	return &DirectResponse{CallbackText: "Details updated."}, nil
}

func (s *Service) editPanelCard(ctx context.Context, chatID, topicID, messageID int64, message model.RenderedMessage, buttons [][]model.ButtonSpec, panel *model.ThreadPanel, snapshot *appserver.ThreadReadSnapshot, cardHash string, state model.DetailsViewState) error {
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender == nil {
		return fmt.Errorf("telegram sender is not ready")
	}
	s.logTelegramRenderedMessagesContainsNil(panel.ThreadID, snapshot.LatestTurnID, "details", messageID, []model.RenderedMessage{message})
	if err := sender.EditRenderedMessage(ctx, chatID, topicID, messageID, message, buttons); err != nil {
		return err
	}
	_ = s.store.PutMessageRoute(ctx, model.MessageRoute{ChatID: chatID, TopicID: topicID, MessageID: messageID, ThreadID: panel.ThreadID, TurnID: snapshot.LatestTurnID, EventID: snapshot.LatestFinalFP, CreatedAt: model.NowString()})
	panel.DetailsViewJSON = model.MustJSON(state)
	panel.LastFinalCardHash = cardHash
	return s.store.UpdateThreadPanelDetails(ctx, panel.ID, panel.DetailsViewJSON, panel.LastFinalCardHash)
}

func (s *Service) renderDetailsCard(ctx context.Context, panelID int64, thread model.Thread, snapshot *appserver.ThreadReadSnapshot, state model.DetailsViewState) (model.RenderedMessage, [][]model.ButtonSpec, string) {
	sections := detailSections(snapshot)
	totalPages := detailsPageCount(len(sections))
	if totalPages == 0 {
		totalPages = 1
	}
	state.Page = clampInt(state.Page, 0, totalPages-1)
	segments := []tgformat.Segment{tgformat.Plain(strings.Join([]string{
		s.visualHeader(ctx, "Details", thread, snapshot.LatestTurnID),
		fmt.Sprintf("Status: %s", readableStatus(snapshot.LatestTurnStatus, thread.Status)),
	}, "\n"))}

	if state.ToolMode {
		index := clampInt(state.CommentaryIndex, 1, maxInt(1, len(sections)))
		if len(sections) == 0 {
			segments = append(segments, tgformat.Plain("\n\nNo detail entries for this turn."))
		} else {
			section := sections[index-1]
			segments = appendDetailSectionHeaderSegments(segments, section)
			segments = appendToolDetailSegments(segments, detailsItemsForSection(snapshot, section), detailsToolMaxBytes)
		}
	} else {
		if len(sections) == 0 {
			segments = append(segments, tgformat.Plain("\n\nNo detail entries for this turn."))
		} else {
			start := state.Page * detailsPageSize
			end := minInt(start+detailsPageSize, len(sections))
			for index := start; index < end; index++ {
				segments = appendDetailSectionSummarySegments(segments, sections[index], detailsItemsForSection(snapshot, sections[index]))
			}
			segments = append(segments, tgformat.Plain(fmt.Sprintf("\n\nPage %d/%d", state.Page+1, totalPages)))
		}
	}
	message := firstRenderedMessage(tgformat.RenderSegments(segments, tgformat.TelegramMessageLimit))
	buttons := s.detailsButtons(ctx, panelID, thread.ID, snapshot.LatestTurnID, state, len(sections))
	return message, buttons, hashStrings(tgformat.HashRendered(message), flattenButtonSpecs(buttons))
}

func appendDetailSectionHeaderSegments(segments []tgformat.Segment, section detailSection) []tgformat.Segment {
	segments = append(segments, tgformat.Plain("\n\n"+section.Title))
	if text := strings.TrimSpace(cleanTelegramNilLiteral(section.Text)); text != "" {
		segments = append(segments, tgformat.Plain("\n"), tgformat.Markdown(text))
	}
	return segments
}

func appendDetailSectionSummarySegments(segments []tgformat.Segment, section detailSection, items []model.DetailItem) []tgformat.Segment {
	segments = appendDetailSectionHeaderSegments(segments, section)
	if section.ToolOnly {
		segments = appendToolDetailSegments(segments, items, detailsToolMaxBytes)
	}
	return segments
}

func appendToolDetailSegments(segments []tgformat.Segment, items []model.DetailItem, quota int) []tgformat.Segment {
	if len(items) == 0 {
		return append(segments, tgformat.Plain("\n\nNo related tool/output entries for this commentary."))
	}
	perItem := quota / len(items)
	if perItem < 300 {
		perItem = 300
	}
	for _, item := range items {
		switch item.Kind {
		case model.DetailItemTool:
			label := strings.TrimSpace(cleanTelegramNilLiteral(item.Label))
			if label == "" {
				label = strings.TrimSpace(cleanTelegramNilLiteral(item.Text))
			}
			segments = append(segments, tgformat.Plain("\n\n[Tool]\n"))
			if label != "" {
				segments = append(segments, tgformat.Markdown("```\n"+trimHead(label, perItem)+"\n```"))
			}
			if status := strings.TrimSpace(item.Status); status != "" {
				segments = append(segments, tgformat.Plain("\nStatus: "+status))
			}
		case model.DetailItemOutput:
			output := strings.TrimSpace(cleanTelegramNilLiteral(item.Output))
			if output == "" {
				output = strings.TrimSpace(cleanTelegramNilLiteral(item.Text))
			}
			if output != "" {
				segments = append(segments, tgformat.Plain("\n\n[Output]\n"), tgformat.Markdown("```\n"+trimOutputTail(output, perItem)+"\n```"))
			}
		}
	}
	return segments
}

func (s *Service) detailsButtons(ctx context.Context, panelID int64, threadID, turnID string, state model.DetailsViewState, commentaryCount int) [][]model.ButtonSpec {
	prevPayload := map[string]any{"panel_id": panelID, "page": state.Page, "tool_mode": state.ToolMode, "commentary_index": state.CommentaryIndex}
	nextPayload := map[string]any{"panel_id": panelID, "page": state.Page, "tool_mode": state.ToolMode, "commentary_index": state.CommentaryIndex}
	rows := [][]model.ButtonSpec{{
		s.callbackButton(ctx, "<", "details_prev", threadID, turnID, "", prevPayload),
		s.callbackButton(ctx, "Back", "details_back", threadID, turnID, "", map[string]any{"panel_id": panelID}),
		s.callbackButton(ctx, ">", "details_next", threadID, turnID, "", nextPayload),
	}}
	togglePayload := map[string]any{"panel_id": panelID, "page": state.Page, "tool_mode": state.ToolMode, "commentary_index": state.CommentaryIndex}
	if state.ToolMode {
		rows = append(rows, []model.ButtonSpec{
			s.callbackButton(ctx, "Tool off", "details_tool_toggle", threadID, turnID, "", togglePayload),
			s.callbackButton(ctx, "Tools file", "details_tools_file", threadID, turnID, "", togglePayload),
		})
	} else {
		rows = append(rows, []model.ButtonSpec{s.callbackButton(ctx, "Tool on", "details_tool_toggle", threadID, turnID, "", togglePayload)})
	}
	return rows
}

func (s *Service) sendDetailsToolsFile(ctx context.Context, chatID, topicID, callbackMessageID int64, route *model.CallbackRoute, payload map[string]any) (*DirectResponse, error) {
	panel, response, err := s.detailsPanelForCallback(ctx, chatID, topicID, callbackMessageID, route, payload, true)
	if err != nil {
		return nil, err
	}
	if response != nil {
		return response, nil
	}
	thread, snapshot, err := s.loadThreadPanelSnapshot(ctx, panel.ThreadID)
	if err != nil || thread == nil || snapshot == nil {
		return &DirectResponse{Text: "Could not load thread details for export."}, nil
	}
	snapshot, ok := snapshotForPanelTurn(*thread, snapshot, panel)
	if !ok {
		return staleDetailsResponse(), nil
	}
	index := payloadInt(payload, "commentary_index")
	if index == 0 {
		index = 1
	}
	body := buildDetailsToolsText(*thread, snapshot, index)
	if strings.TrimSpace(body) == "" {
		return &DirectResponse{CallbackText: "No related tool/output entries."}, nil
	}
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender == nil {
		return &DirectResponse{Text: "Telegram sender is not ready yet."}, nil
	}
	fileName := fmt.Sprintf("%s-%s-details-%d.txt", sanitizeFileName(thread.ProjectName), sanitizeFileName(thread.ShortID()), index)
	caption := s.visualHeader(ctx, "Details tools", *thread, snapshot.LatestTurnID)
	if _, err := sender.SendDocumentData(ctx, chatID, topicID, fileName, []byte(body), caption, silentSendOptions()); err != nil {
		return &DirectResponse{Text: fmt.Sprintf("Could not send details tools file: %v", err)}, nil
	}
	_ = route
	return &DirectResponse{CallbackText: "Tools file sent."}, nil
}

func (s *Service) detailsPanelForCallback(ctx context.Context, chatID, topicID, callbackMessageID int64, route *model.CallbackRoute, payload map[string]any, requireMessageMatch bool) (*model.ThreadPanel, *DirectResponse, error) {
	panelID := payloadInt64(payload, "panel_id")
	if panelID == 0 {
		return nil, staleDetailsResponse(), nil
	}
	panel, err := s.store.GetThreadPanelByID(ctx, panelID)
	if err != nil {
		return nil, nil, err
	}
	if panel == nil {
		return nil, staleDetailsResponse(), nil
	}
	if panel.ChatID != chatID || panel.TopicID != topicID {
		return nil, staleDetailsResponse(), nil
	}
	if route != nil {
		if threadID := strings.TrimSpace(route.ThreadID); threadID != "" && strings.TrimSpace(panel.ThreadID) != threadID {
			return nil, staleDetailsResponse(), nil
		}
		if turnID := strings.TrimSpace(route.TurnID); turnID != "" && strings.TrimSpace(panel.CurrentTurnID) != turnID {
			return nil, staleDetailsResponse(), nil
		}
	}
	if requireMessageMatch && callbackMessageID != panel.SummaryMessageID {
		return nil, staleDetailsResponse(), nil
	}
	return panel, nil, nil
}

func staleDetailsResponse() *DirectResponse {
	return &DirectResponse{Text: staleDetailsText}
}

type detailSection struct {
	Title           string
	Text            string
	CommentaryIndex int
	ToolOnly        bool
}

func buildDetailsToolsText(thread model.Thread, snapshot *appserver.ThreadReadSnapshot, sectionIndex int) string {
	sections := detailSections(snapshot)
	if sectionIndex < 1 || sectionIndex > len(sections) {
		return ""
	}
	section := sections[sectionIndex-1]
	lines := []string{
		visualFileHeader(thread, snapshot.LatestTurnID, "Details tools"),
		section.Title,
	}
	if text := strings.TrimSpace(cleanTelegramNilLiteral(section.Text)); text != "" {
		lines = append(lines, text)
	}
	items := detailsItemsForSection(snapshot, section)
	for _, item := range items {
		switch item.Kind {
		case model.DetailItemTool:
			lines = append(lines, "", "[Tool]", strings.TrimSpace(item.Label))
			if status := strings.TrimSpace(item.Status); status != "" {
				lines = append(lines, "Status: "+status)
			}
		case model.DetailItemOutput:
			output := strings.TrimSpace(item.Output)
			if output == "" {
				output = strings.TrimSpace(item.Text)
			}
			lines = append(lines, "", "[Output]", output)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func detailSections(snapshot *appserver.ThreadReadSnapshot) []detailSection {
	if snapshot == nil {
		return nil
	}
	out := []detailSection{}
	if len(detailsItemsForCommentaryIndex(snapshot, 0)) > 0 {
		out = append(out, detailSection{Title: "Tool activity", ToolOnly: true})
	}
	for index, commentary := range detailCommentaries(snapshot) {
		commentaryIndex := commentary.CommentaryIndex
		if commentaryIndex <= 0 {
			commentaryIndex = index + 1
		}
		out = append(out, detailSection{
			Title:           fmt.Sprintf("[commentary %d]", index+1),
			Text:            commentary.Text,
			CommentaryIndex: commentaryIndex,
		})
	}
	return out
}

func detailCommentaries(snapshot *appserver.ThreadReadSnapshot) []model.DetailItem {
	if snapshot == nil {
		return nil
	}
	out := []model.DetailItem{}
	for _, item := range snapshot.DetailItems {
		if item.Kind == model.DetailItemCommentary {
			out = append(out, item)
		}
	}
	if len(out) > 0 {
		return out
	}
	entries := chronologicalAgentEntries(snapshot.LatestAgentMessageEntries)
	for index, entry := range entries {
		if strings.TrimSpace(entry.Text) == "" {
			continue
		}
		out = append(out, model.DetailItem{
			ID:              entry.ID,
			Kind:            model.DetailItemCommentary,
			Phase:           entry.Phase,
			Text:            entry.Text,
			FP:              entry.FP,
			CommentaryIndex: index + 1,
		})
	}
	return out
}

func snapshotForPanelTurn(thread model.Thread, snapshot *appserver.ThreadReadSnapshot, panel *model.ThreadPanel) (*appserver.ThreadReadSnapshot, bool) {
	if snapshot == nil || panel == nil {
		return snapshot, true
	}
	turnID := strings.TrimSpace(panel.CurrentTurnID)
	if turnID == "" || strings.TrimSpace(snapshot.LatestTurnID) == turnID {
		return snapshot, true
	}
	var payload map[string]any
	if err := json.Unmarshal(snapshot.Thread.Raw, &payload); err != nil || payload == nil {
		if err := json.Unmarshal(thread.Raw, &payload); err != nil || payload == nil {
			return nil, false
		}
	}
	turns := turnsFromThreadPayload(payload)
	if len(turns) == 0 {
		return nil, false
	}
	for _, rawTurn := range turns {
		turn, _ := rawTurn.(map[string]any)
		if turn == nil || strings.TrimSpace(stringValueFromMap(turn, "id")) != turnID {
			continue
		}
		panelPayload := panelThreadPayloadForTurn(payload, turn)
		panelSnapshot := appserver.SnapshotFromThreadRead(panelPayload)
		if panelSnapshot.Thread.ID == "" {
			panelSnapshot.Thread = thread
		}
		return &panelSnapshot, true
	}
	return nil, false
}

func panelThreadPayloadForTurn(payload map[string]any, turn map[string]any) map[string]any {
	if nested, ok := payload["thread"].(map[string]any); ok && nested != nil {
		out := shallowCopyMap(payload)
		threadPayload := shallowCopyMap(nested)
		threadPayload["turns"] = []any{turn}
		out["thread"] = threadPayload
		return out
	}
	out := shallowCopyMap(payload)
	out["turns"] = []any{turn}
	return out
}

func turnsFromThreadPayload(payload map[string]any) []any {
	if payload == nil {
		return nil
	}
	if turns, ok := payload["turns"].([]any); ok {
		return turns
	}
	if nested, ok := payload["thread"].(map[string]any); ok && nested != nil {
		if turns, ok := nested["turns"].([]any); ok {
			return turns
		}
	}
	return nil
}

func shallowCopyMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func stringValueFromMap(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return value
}

func detailsItemsForSection(snapshot *appserver.ThreadReadSnapshot, section detailSection) []model.DetailItem {
	if snapshot == nil {
		return nil
	}
	if section.ToolOnly {
		return detailsItemsForCommentaryIndex(snapshot, 0)
	}
	return detailsItemsForCommentaryIndex(snapshot, section.CommentaryIndex)
}

func detailsItemsForCommentary(snapshot *appserver.ThreadReadSnapshot, sectionIndex int) []model.DetailItem {
	sections := detailSections(snapshot)
	if sectionIndex < 1 || sectionIndex > len(sections) {
		return nil
	}
	return detailsItemsForSection(snapshot, sections[sectionIndex-1])
}

func detailsItemsForCommentaryIndex(snapshot *appserver.ThreadReadSnapshot, commentaryIndex int) []model.DetailItem {
	if snapshot == nil || commentaryIndex < 0 {
		return nil
	}
	out := []model.DetailItem{}
	for _, item := range snapshot.DetailItems {
		if item.CommentaryIndex == commentaryIndex && (item.Kind == model.DetailItemTool || item.Kind == model.DetailItemOutput) {
			out = append(out, item)
		}
	}
	return out
}

func firstCommentaryWithToolsOnPage(snapshot *appserver.ThreadReadSnapshot, page, commentaryCount int) int {
	if commentaryCount <= 0 {
		return 1
	}
	start := clampInt(page, 0, maxInt(0, detailsPageCount(commentaryCount)-1))*detailsPageSize + 1
	end := minInt(start+detailsPageSize-1, commentaryCount)
	for index := start; index <= end; index++ {
		if len(detailsItemsForCommentary(snapshot, index)) > 0 {
			return index
		}
	}
	return clampInt(start, 1, commentaryCount)
}

func detailsPageCount(count int) int {
	if count <= 0 {
		return 0
	}
	return (count + detailsPageSize - 1) / detailsPageSize
}

func detailsStateFromPayload(payload map[string]any) model.DetailsViewState {
	return model.DetailsViewState{
		Page:            payloadInt(payload, "page"),
		ToolMode:        payloadBool(payload, "tool_mode"),
		CommentaryIndex: payloadInt(payload, "commentary_index"),
	}
}

func payloadInt(payload map[string]any, key string) int {
	return int(payloadInt64(payload, key))
}

func payloadInt64(payload map[string]any, key string) int64 {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	case jsonNumber:
		parsed, _ := value.Int64()
		return parsed
	case string:
		var parsed int64
		_, _ = fmt.Sscanf(value, "%d", &parsed)
		return parsed
	default:
		return 0
	}
}

func payloadBool(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	switch value := payload[key].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(value, "true")
	default:
		return false
	}
}

type jsonNumber interface {
	Int64() (int64, error)
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func trimHead(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	runes := []rune(value)
	if limit >= len(runes) {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "\n..."
}
