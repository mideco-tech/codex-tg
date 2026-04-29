package daemon

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
	"github.com/mideco-tech/codex-tg/internal/tgformat"
)

const (
	threadSummaryLimit = 3
	outputMessageLimit = 3900
	steerTTL           = 10 * time.Minute
)

func (s *Service) currentBackgroundTarget(ctx context.Context) (*model.ObserverTarget, error) {
	target, configured, err := s.store.GetGlobalObserverTarget(ctx)
	if err != nil {
		return nil, err
	}
	if configured {
		return target, nil
	}
	if len(s.cfg.AllowedUserIDs) == 1 {
		return &model.ObserverTarget{
			ChatKey: model.ChatKey(s.cfg.AllowedUserIDs[0], 0),
			ChatID:  s.cfg.AllowedUserIDs[0],
			TopicID: 0,
			Enabled: true,
		}, nil
	}
	if len(s.cfg.AllowedUserIDs) == 0 && len(s.cfg.AllowedChatIDs) == 1 {
		return &model.ObserverTarget{
			ChatKey: model.ChatKey(s.cfg.AllowedChatIDs[0], 0),
			ChatID:  s.cfg.AllowedChatIDs[0],
			TopicID: 0,
			Enabled: true,
		}, nil
	}
	return nil, nil
}

func (s *Service) syncThreadPanel(ctx context.Context, threadID string) {
	seen := map[string]struct{}{}
	target, err := s.currentBackgroundTarget(ctx)
	if err == nil && target != nil && target.Enabled {
		seen[target.ChatKey] = struct{}{}
		s.syncThreadPanelToTarget(ctx, *target, threadID, false, model.PanelSourceGlobalObserver)
	}
	panels, err := s.store.ListCurrentPanelsForThread(ctx, threadID)
	if err != nil {
		return
	}
	for _, panel := range panels {
		if panel.SourceMode == model.PanelSourceGlobalObserver && (target == nil || !target.Enabled) {
			continue
		}
		chatKey := model.ChatKey(panel.ChatID, panel.TopicID)
		if _, ok := seen[chatKey]; ok {
			continue
		}
		seen[chatKey] = struct{}{}
		s.syncThreadPanelToTarget(ctx, model.ObserverTarget{
			ChatKey: chatKey,
			ChatID:  panel.ChatID,
			TopicID: panel.TopicID,
			Enabled: true,
		}, threadID, false, panel.SourceMode)
	}
}

func (s *Service) syncThreadPanelToTarget(ctx context.Context, target model.ObserverTarget, threadID string, forceNew bool, sourceMode string) {
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender == nil {
		return
	}
	thread, snapshot, err := s.loadThreadPanelSnapshot(ctx, threadID)
	if err != nil || thread == nil || snapshot == nil {
		return
	}
	pending, _ := s.store.GetLatestPendingApprovalForThread(ctx, threadID)

	s.panelMu.Lock()
	defer s.panelMu.Unlock()

	existingPanel, _ := s.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	panelMode := s.panelMode(ctx)
	effectiveForceNew := forceNew && panelMode != model.PanelModeStable
	if sourceMode == model.PanelSourceTelegramInput && samePanelTurn(existingPanel, snapshot.LatestTurnID) {
		effectiveForceNew = false
	}
	panel, err := s.ensureCurrentPanel(ctx, sender, target, *thread, snapshot, pending, effectiveForceNew, sourceMode, panelMode)
	if err != nil || panel == nil {
		return
	}
	if sourceMode == model.PanelSourceTelegramInput && panel.SourceMode != model.PanelSourceTelegramInput && samePanelTurn(panel, snapshot.LatestTurnID) {
		panel.SourceMode = model.PanelSourceTelegramInput
		_ = s.store.UpdateThreadPanelSourceMode(ctx, panel.ID, panel.SourceMode)
	}
	legacyTerminalReplay := existingPanel != nil && existingPanel.CurrentTurnID == strings.TrimSpace(snapshot.LatestTurnID) && isLegacyTerminalReplay(panel, snapshot)
	if isTerminalStatus(snapshot.LatestTurnStatus) && strings.TrimSpace(snapshot.LatestFinalFP) != "" && panel.LastFinalNoticeFP == snapshot.LatestFinalFP {
		return
	}
	if legacyTerminalReplay && snapshot.LatestFinalFP != "" {
		panel.LastFinalNoticeFP = snapshot.LatestFinalFP
		if err := s.store.UpdateThreadPanelState(ctx, panel.ID, panel.CurrentTurnID, panel.Status, panel.LastSummaryHash, panel.LastToolHash, panel.LastOutputHash, panel.LastFinalNoticeFP); err != nil {
			s.setError(ctx, err)
		}
		return
	}
	if shouldRenderFinalCardNow(panel, snapshot) {
		if err := s.maybeSendUserRequestNotice(ctx, sender, panel, *thread, snapshot); err != nil {
			s.setError(ctx, err)
			return
		}
		if err := s.maybeRenderFinalCard(ctx, sender, target, panel, *thread, snapshot); err != nil {
			s.setError(ctx, err)
		}
		return
	}
	if err := s.updateCurrentPanel(ctx, sender, panel, *thread, snapshot, pending); err != nil {
		panel, recreateErr := s.createCurrentPanel(ctx, sender, target, *thread, snapshot, pending, sourceMode)
		if recreateErr != nil || panel == nil {
			s.setError(ctx, err)
			return
		}
		legacyTerminalReplay = false
		if err := s.updateCurrentPanel(ctx, sender, panel, *thread, snapshot, pending); err != nil {
			s.setError(ctx, err)
			return
		}
	}
	if err := s.maybeRenderFinalCard(ctx, sender, target, panel, *thread, snapshot); err != nil {
		s.setError(ctx, err)
	}
}

func (s *Service) loadThreadPanelSnapshot(ctx context.Context, threadID string) (*model.Thread, *appserver.ThreadReadSnapshot, error) {
	thread, err := s.store.GetThread(ctx, threadID)
	if err != nil || thread == nil {
		return nil, nil, err
	}
	snapshotState, err := s.store.GetSnapshot(ctx, threadID)
	if err != nil || snapshotState == nil || len(snapshotState.CompactJSON) == 0 {
		return thread, nil, err
	}
	var snapshot appserver.ThreadReadSnapshot
	if err := json.Unmarshal(snapshotState.CompactJSON, &snapshot); err != nil {
		return thread, nil, err
	}
	if snapshot.Thread.ID == "" {
		snapshot.Thread = *thread
	}
	return thread, &snapshot, nil
}

func (s *Service) ensureCurrentPanel(ctx context.Context, sender Sender, target model.ObserverTarget, thread model.Thread, snapshot *appserver.ThreadReadSnapshot, pending *model.PendingApproval, forceNew bool, sourceMode, panelMode string) (*model.ThreadPanel, error) {
	panel, err := s.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		return nil, err
	}
	turnID := strings.TrimSpace(snapshot.LatestTurnID)
	status := strings.TrimSpace(snapshot.LatestTurnStatus)
	if panel == nil &&
		sourceMode == model.PanelSourceGlobalObserver &&
		isTerminalStatus(status) &&
		!snapshot.WaitingOnApproval &&
		!snapshot.WaitingOnReply &&
		!s.allowRecentTerminalObserverCreate(ctx, thread) {
		return nil, nil
	}
	if forceNew || panel == nil || panelNeedsRefresh(panel, turnID, status, panelMode) {
		panel, err = s.createCurrentPanel(ctx, sender, target, thread, snapshot, pending, sourceMode)
		if err != nil {
			return nil, err
		}
	}
	return panel, nil
}

func (s *Service) allowRecentTerminalObserverCreate(ctx context.Context, thread model.Thread) bool {
	updatedAt := time.Unix(thread.UpdatedAt, 0).UTC()
	if thread.UpdatedAt <= 0 || updatedAt.IsZero() {
		return false
	}
	if sinceUnix := s.globalObserverSinceUnix(ctx); sinceUnix > 0 && thread.UpdatedAt < sinceUnix {
		return false
	}
	return time.Since(updatedAt) <= s.catchupWindow()
}

func panelNeedsRefresh(panel *model.ThreadPanel, turnID, status, panelMode string) bool {
	if panel == nil {
		return true
	}
	if panel.SummaryMessageID == 0 || panel.ToolMessageID == 0 || panel.OutputMessageID == 0 {
		return true
	}
	if panelMode == model.PanelModeStable {
		return false
	}
	if strings.TrimSpace(turnID) == "" {
		return false
	}
	if strings.TrimSpace(panel.CurrentTurnID) == "" {
		return isTerminalStatus(panel.Status)
	}
	if panel.CurrentTurnID != turnID && isTerminalStatus(panel.Status) {
		return true
	}
	return false
}

func samePanelTurn(panel *model.ThreadPanel, turnID string) bool {
	if panel == nil {
		return false
	}
	turnID = strings.TrimSpace(turnID)
	return turnID != "" && strings.TrimSpace(panel.CurrentTurnID) == turnID
}

func isLegacyTerminalReplay(panel *model.ThreadPanel, snapshot *appserver.ThreadReadSnapshot) bool {
	if panel == nil || snapshot == nil {
		return false
	}
	if panel.RunNoticeMessageID != 0 || panel.UserMessageID != 0 {
		return false
	}
	if strings.TrimSpace(panel.SourceMode) != model.PanelSourceGlobalObserver {
		return false
	}
	if strings.TrimSpace(panel.LastFinalNoticeFP) != "" || strings.TrimSpace(snapshot.LatestFinalFP) == "" {
		return false
	}
	if strings.TrimSpace(panel.CurrentTurnID) == "" || strings.TrimSpace(snapshot.LatestTurnID) == "" {
		return false
	}
	if panel.CurrentTurnID != strings.TrimSpace(snapshot.LatestTurnID) {
		return false
	}
	return isTerminalStatus(panel.Status) && isTerminalStatus(snapshot.LatestTurnStatus)
}

func shouldRenderFinalCardNow(panel *model.ThreadPanel, snapshot *appserver.ThreadReadSnapshot) bool {
	if panel == nil || snapshot == nil {
		return false
	}
	if !isTerminalStatus(snapshot.LatestTurnStatus) || strings.TrimSpace(snapshot.LatestFinalText) == "" {
		return false
	}
	finalFP := strings.TrimSpace(snapshot.LatestFinalFP)
	return finalFP != "" && finalFP != strings.TrimSpace(panel.LastFinalNoticeFP)
}

func (s *Service) createCurrentPanel(ctx context.Context, sender Sender, target model.ObserverTarget, thread model.Thread, snapshot *appserver.ThreadReadSnapshot, pending *model.PendingApproval, sourceMode string) (*model.ThreadPanel, error) {
	runNoticeMessageID, runNoticeFP, err := s.sendRunNotice(ctx, sender, target, thread, snapshot, sourceMode)
	if err != nil {
		return nil, err
	}
	userMessageID, userNoticeFP, err := s.sendInitialUserRequestNotice(ctx, sender, target, thread, snapshot, sourceMode)
	if err != nil {
		return nil, err
	}
	planPromptMessageID, planPromptFP, err := s.sendPlanPromptNotice(ctx, sender, target, thread, effectivePlanPrompt(pending, snapshot))
	if err != nil {
		return nil, err
	}
	summaryMessage, summaryButtons, summaryHash := s.renderSummaryPanel(ctx, thread, snapshot, nil)
	toolText, toolHash := s.renderToolPanel(ctx, thread, snapshot)
	outputText, outputHash := s.renderOutputPanel(ctx, thread, snapshot)

	summaryIDs, err := sender.SendRenderedMessages(ctx, target.ChatID, target.TopicID, []model.RenderedMessage{summaryMessage}, summaryButtons)
	if err != nil {
		return nil, err
	}
	summaryID := lastMessageID(summaryIDs)
	toolID, err := sender.SendMessage(ctx, target.ChatID, target.TopicID, toolText, nil)
	if err != nil {
		return nil, err
	}
	outputID, err := sender.SendMessage(ctx, target.ChatID, target.TopicID, outputText, nil)
	if err != nil {
		return nil, err
	}

	panel, err := s.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:              target.ChatID,
		TopicID:             target.TopicID,
		ProjectName:         thread.ProjectName,
		ThreadID:            thread.ID,
		SourceMode:          sourceMode,
		SummaryMessageID:    summaryID,
		ToolMessageID:       toolID,
		OutputMessageID:     outputID,
		CurrentTurnID:       snapshot.LatestTurnID,
		Status:              snapshot.LatestTurnStatus,
		ArchiveEnabled:      true,
		LastSummaryHash:     summaryHash,
		LastToolHash:        toolHash,
		LastOutputHash:      outputHash,
		RunNoticeMessageID:  runNoticeMessageID,
		LastRunNoticeFP:     runNoticeFP,
		UserMessageID:       userMessageID,
		LastUserNoticeFP:    userNoticeFP,
		PlanPromptMessageID: planPromptMessageID,
		LastPlanPromptFP:    planPromptFP,
	})
	if err != nil {
		return nil, err
	}
	_ = s.store.PutMessageRoute(ctx, model.MessageRoute{ChatID: target.ChatID, TopicID: target.TopicID, MessageID: summaryID, ThreadID: thread.ID, TurnID: snapshot.LatestTurnID, CreatedAt: model.NowString()})
	_ = s.store.PutMessageRoute(ctx, model.MessageRoute{ChatID: target.ChatID, TopicID: target.TopicID, MessageID: toolID, ThreadID: thread.ID, TurnID: snapshot.LatestTurnID, CreatedAt: model.NowString()})
	_ = s.store.PutMessageRoute(ctx, model.MessageRoute{ChatID: target.ChatID, TopicID: target.TopicID, MessageID: outputID, ThreadID: thread.ID, TurnID: snapshot.LatestTurnID, CreatedAt: model.NowString()})
	return panel, nil
}

func (s *Service) maybeSendUserRequestNotice(ctx context.Context, sender Sender, panel *model.ThreadPanel, thread model.Thread, snapshot *appserver.ThreadReadSnapshot) error {
	if panel == nil || strings.TrimSpace(snapshot.LatestUserMessageFP) == "" {
		return nil
	}
	if snapshot.LatestUserMessageFP == panel.LastUserNoticeFP {
		return nil
	}
	if !shouldSendUserRequestNotice(panel.SourceMode, snapshot) || s.isTelegramOriginTurn(ctx, thread.ID, snapshot.LatestTurnID) {
		return nil
	}
	if panel.SourceMode == model.PanelSourceGlobalObserver && panel.UserMessageID == 0 {
		return nil
	}
	if panel.UserMessageID != 0 {
		message := firstRenderedMessage(s.renderUserRequestNoticeCard(ctx, thread, snapshot))
		if err := sender.EditRenderedMessage(ctx, panel.ChatID, panel.TopicID, panel.UserMessageID, message, nil); err != nil {
			s.setError(ctx, fmt.Errorf("edit user notice card: %w", err))
			return nil
		}
		_ = s.store.PutMessageRoute(ctx, model.MessageRoute{
			ChatID:    panel.ChatID,
			TopicID:   panel.TopicID,
			MessageID: panel.UserMessageID,
			ThreadID:  thread.ID,
			TurnID:    snapshot.LatestTurnID,
			ItemID:    snapshot.LatestUserMessageID,
			EventID:   snapshot.LatestUserMessageFP,
			CreatedAt: model.NowString(),
		})
		panel.LastUserNoticeFP = snapshot.LatestUserMessageFP
		return s.store.UpdateThreadPanelUserNotice(ctx, panel.ID, panel.UserMessageID, panel.LastUserNoticeFP)
	}
	target := model.ObserverTarget{
		ChatKey: model.ChatKey(panel.ChatID, panel.TopicID),
		ChatID:  panel.ChatID,
		TopicID: panel.TopicID,
		Enabled: true,
	}
	messageID, noticeFP, err := s.sendUserRequestNotice(ctx, sender, target, thread, snapshot, panel.SourceMode)
	if err != nil {
		return err
	}
	if noticeFP == "" {
		return nil
	}
	panel.UserMessageID = messageID
	panel.LastUserNoticeFP = noticeFP
	return s.store.UpdateThreadPanelUserNotice(ctx, panel.ID, panel.UserMessageID, panel.LastUserNoticeFP)
}

func (s *Service) sendInitialUserRequestNotice(ctx context.Context, sender Sender, target model.ObserverTarget, thread model.Thread, snapshot *appserver.ThreadReadSnapshot, sourceMode string) (int64, string, error) {
	if shouldSendUserRequestNotice(sourceMode, snapshot) && !s.isTelegramOriginTurn(ctx, thread.ID, snapshot.LatestTurnID) {
		return s.sendUserRequestNotice(ctx, sender, target, thread, snapshot, sourceMode)
	}
	if shouldSendUserPlaceholder(sourceMode, snapshot) && !s.isTelegramOriginTurn(ctx, thread.ID, snapshot.LatestTurnID) {
		return s.sendUserPlaceholderNotice(ctx, sender, target, thread, snapshot)
	}
	return 0, "", nil
}

func (s *Service) sendRunNotice(ctx context.Context, sender Sender, target model.ObserverTarget, thread model.Thread, snapshot *appserver.ThreadReadSnapshot, sourceMode string) (int64, string, error) {
	if !shouldSendRunNotice(sourceMode, snapshot) {
		return 0, "", nil
	}
	text, fp := s.renderRunNotice(ctx, thread, snapshot, sourceMode)
	messageID, err := sender.SendMessage(ctx, target.ChatID, target.TopicID, text, nil)
	if err != nil {
		return 0, "", err
	}
	_ = s.store.PutMessageRoute(ctx, model.MessageRoute{
		ChatID:    target.ChatID,
		TopicID:   target.TopicID,
		MessageID: messageID,
		ThreadID:  thread.ID,
		TurnID:    snapshot.LatestTurnID,
		EventID:   runNoticeEventID(target, thread.ID, snapshot.LatestTurnID),
		CreatedAt: model.NowString(),
	})
	return messageID, fp, nil
}

func (s *Service) renderRunNotice(ctx context.Context, thread model.Thread, snapshot *appserver.ThreadReadSnapshot, sourceMode string) (string, string) {
	source := "Explicit"
	switch strings.TrimSpace(sourceMode) {
	case model.PanelSourceGlobalObserver:
		source = "GUI/CLI observer"
	case model.PanelSourceTelegramInput:
		source = "Telegram"
	}
	text := strings.Join([]string{
		s.visualDividerText(ctx, thread, snapshot.LatestTurnID),
		fmt.Sprintf("Source: %s", source),
	}, "\n")
	return text, hashStrings(text)
}

func (s *Service) maybeSendPlanPromptNotice(ctx context.Context, sender Sender, panel *model.ThreadPanel, thread model.Thread, prompt *model.PlanPrompt) error {
	if panel == nil || prompt == nil || strings.TrimSpace(prompt.Fingerprint) == "" || prompt.Fingerprint == panel.LastPlanPromptFP {
		return nil
	}
	target := model.ObserverTarget{
		ChatKey: model.ChatKey(panel.ChatID, panel.TopicID),
		ChatID:  panel.ChatID,
		TopicID: panel.TopicID,
		Enabled: true,
	}
	messageID, promptFP, err := s.sendPlanPromptNotice(ctx, sender, target, thread, prompt)
	if err != nil {
		return err
	}
	if promptFP == "" {
		return nil
	}
	panel.PlanPromptMessageID = messageID
	panel.LastPlanPromptFP = promptFP
	return s.store.UpdateThreadPanelPlanPrompt(ctx, panel.ID, panel.PlanPromptMessageID, panel.LastPlanPromptFP)
}

func (s *Service) sendPlanPromptNotice(ctx context.Context, sender Sender, target model.ObserverTarget, thread model.Thread, prompt *model.PlanPrompt) (int64, string, error) {
	if prompt == nil || strings.TrimSpace(prompt.Question) == "" || strings.TrimSpace(prompt.Fingerprint) == "" {
		return 0, "", nil
	}
	message, buttons, _ := s.renderPlanPromptCard(ctx, thread, prompt)
	messageIDs, err := sender.SendRenderedMessages(ctx, target.ChatID, target.TopicID, []model.RenderedMessage{message}, buttons)
	if err != nil {
		return 0, "", err
	}
	messageID := lastMessageID(messageIDs)
	_ = s.store.PutMessageRoute(ctx, model.MessageRoute{
		ChatID:    target.ChatID,
		TopicID:   target.TopicID,
		MessageID: messageID,
		ThreadID:  thread.ID,
		TurnID:    prompt.TurnID,
		ItemID:    firstNonEmpty(prompt.ItemID, prompt.PromptID),
		EventID:   planPromptRouteEventID(prompt),
		CreatedAt: model.NowString(),
	})
	return messageID, prompt.Fingerprint, nil
}

func shouldSendRunNotice(sourceMode string, snapshot *appserver.ThreadReadSnapshot) bool {
	if snapshot == nil || strings.TrimSpace(snapshot.LatestTurnID) == "" {
		return false
	}
	switch strings.TrimSpace(sourceMode) {
	case model.PanelSourceTelegramInput:
		return true
	case model.PanelSourceGlobalObserver:
	default:
		return false
	}
	if strings.TrimSpace(snapshot.LatestUserMessageText) != "" {
		return true
	}
	if strings.TrimSpace(snapshot.LatestToolLabel) != "" || strings.TrimSpace(snapshot.LatestToolOutput) != "" {
		return true
	}
	return !isTerminalStatus(snapshot.LatestTurnStatus) || snapshot.WaitingOnApproval || snapshot.WaitingOnReply
}

func shouldSendUserPlaceholder(sourceMode string, snapshot *appserver.ThreadReadSnapshot) bool {
	if strings.TrimSpace(sourceMode) != model.PanelSourceGlobalObserver || snapshot == nil || strings.TrimSpace(snapshot.LatestTurnID) == "" {
		return false
	}
	if strings.TrimSpace(snapshot.LatestUserMessageText) != "" {
		return false
	}
	return strings.TrimSpace(snapshot.LatestToolLabel) != "" ||
		strings.TrimSpace(snapshot.LatestToolOutput) != "" ||
		!isTerminalStatus(snapshot.LatestTurnStatus) ||
		snapshot.WaitingOnApproval ||
		snapshot.WaitingOnReply
}

func planPromptRouteEventID(prompt *model.PlanPrompt) string {
	if prompt != nil && strings.TrimSpace(prompt.RequestID) != "" {
		return "plan_request:" + strings.TrimSpace(prompt.RequestID)
	}
	if prompt == nil {
		return ""
	}
	return prompt.Fingerprint
}

func (s *Service) sendUserRequestNotice(ctx context.Context, sender Sender, target model.ObserverTarget, thread model.Thread, snapshot *appserver.ThreadReadSnapshot, sourceMode string) (int64, string, error) {
	if !shouldSendUserRequestNotice(sourceMode, snapshot) || s.isTelegramOriginTurn(ctx, thread.ID, snapshot.LatestTurnID) {
		return 0, "", nil
	}
	messages := s.renderUserRequestNoticeCard(ctx, thread, snapshot)
	messageIDs, err := sender.SendRenderedMessages(ctx, target.ChatID, target.TopicID, messages, nil)
	if err != nil {
		return 0, "", err
	}
	canonicalMessageID := firstMessageID(messageIDs)
	for _, messageID := range messageIDs {
		_ = s.store.PutMessageRoute(ctx, model.MessageRoute{
			ChatID:    target.ChatID,
			TopicID:   target.TopicID,
			MessageID: messageID,
			ThreadID:  thread.ID,
			TurnID:    snapshot.LatestTurnID,
			ItemID:    snapshot.LatestUserMessageID,
			EventID:   snapshot.LatestUserMessageFP,
			CreatedAt: model.NowString(),
		})
	}
	return canonicalMessageID, snapshot.LatestUserMessageFP, nil
}

func (s *Service) sendUserPlaceholderNotice(ctx context.Context, sender Sender, target model.ObserverTarget, thread model.Thread, snapshot *appserver.ThreadReadSnapshot) (int64, string, error) {
	message := s.renderUserPlaceholderCard(ctx, thread, snapshot)
	messageIDs, err := sender.SendRenderedMessages(ctx, target.ChatID, target.TopicID, []model.RenderedMessage{message}, nil)
	if err != nil {
		return 0, "", err
	}
	messageID := firstMessageID(messageIDs)
	_ = s.store.PutMessageRoute(ctx, model.MessageRoute{
		ChatID:    target.ChatID,
		TopicID:   target.TopicID,
		MessageID: messageID,
		ThreadID:  thread.ID,
		TurnID:    snapshot.LatestTurnID,
		EventID:   userPlaceholderEventID(target, thread.ID, snapshot.LatestTurnID),
		CreatedAt: model.NowString(),
	})
	return messageID, "", nil
}

func (s *Service) renderUserRequestNoticeCard(ctx context.Context, thread model.Thread, snapshot *appserver.ThreadReadSnapshot) []model.RenderedMessage {
	return tgformat.RenderMarkdownWithHeader(s.visualHeader(ctx, "User", thread, snapshot.LatestTurnID), snapshot.LatestUserMessageText)
}

func (s *Service) renderUserPlaceholderCard(ctx context.Context, thread model.Thread, snapshot *appserver.ThreadReadSnapshot) model.RenderedMessage {
	return renderSingleMarkdownCard(s.visualHeader(ctx, "User", thread, snapshot.LatestTurnID), "User prompt was not available from app-server snapshot.")
}

func shouldSendUserRequestNotice(sourceMode string, snapshot *appserver.ThreadReadSnapshot) bool {
	if snapshot == nil || strings.TrimSpace(snapshot.LatestUserMessageText) == "" || strings.TrimSpace(snapshot.LatestUserMessageFP) == "" {
		return false
	}
	return strings.TrimSpace(sourceMode) != model.PanelSourceTelegramInput
}

func runNoticeEventID(target model.ObserverTarget, threadID, turnID string) string {
	return strings.Join([]string{
		"ui.run_notice",
		model.ChatKey(target.ChatID, target.TopicID),
		strings.TrimSpace(threadID),
		strings.TrimSpace(turnID),
	}, ".")
}

func userPlaceholderEventID(target model.ObserverTarget, threadID, turnID string) string {
	return strings.Join([]string{
		"ui.user_placeholder",
		model.ChatKey(target.ChatID, target.TopicID),
		strings.TrimSpace(threadID),
		strings.TrimSpace(turnID),
	}, ".")
}

func (s *Service) markTelegramOriginTurn(ctx context.Context, threadID, turnID string) error {
	key := telegramOriginTurnKey(threadID, turnID)
	if key == "" {
		return nil
	}
	return s.store.SetState(ctx, key, model.PanelSourceTelegramInput)
}

func (s *Service) isTelegramOriginTurn(ctx context.Context, threadID, turnID string) bool {
	key := telegramOriginTurnKey(threadID, turnID)
	if key == "" {
		return false
	}
	value, err := s.store.GetState(ctx, key)
	return err == nil && strings.TrimSpace(value) == model.PanelSourceTelegramInput
}

func telegramOriginTurnKey(threadID, turnID string) string {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return ""
	}
	return "turn_origin.telegram." + threadID + "." + turnID
}

func (s *Service) updateCurrentPanel(ctx context.Context, sender Sender, panel *model.ThreadPanel, thread model.Thread, snapshot *appserver.ThreadReadSnapshot, pending *model.PendingApproval) error {
	if err := s.maybeUpdateRunNotice(ctx, sender, panel, thread, snapshot); err != nil {
		return err
	}
	if err := s.maybeSendUserRequestNotice(ctx, sender, panel, thread, snapshot); err != nil {
		return err
	}
	if err := s.maybeSendPlanPromptNotice(ctx, sender, panel, thread, effectivePlanPrompt(pending, snapshot)); err != nil {
		return err
	}
	summaryMessage, summaryButtons, summaryHash := s.renderSummaryPanel(ctx, thread, snapshot, pending)
	if summaryHash != panel.LastSummaryHash {
		if err := sender.EditRenderedMessage(ctx, panel.ChatID, panel.TopicID, panel.SummaryMessageID, summaryMessage, summaryButtons); err != nil {
			return err
		}
		panel.LastSummaryHash = summaryHash
	}

	toolText, toolHash := s.renderToolPanel(ctx, thread, snapshot)
	if toolHash != panel.LastToolHash {
		if err := sender.EditMessage(ctx, panel.ChatID, panel.TopicID, panel.ToolMessageID, toolText, nil); err != nil {
			return err
		}
		panel.LastToolHash = toolHash
	}

	outputText, outputHash := s.renderOutputPanel(ctx, thread, snapshot)
	if outputHash != panel.LastOutputHash {
		if err := sender.EditMessage(ctx, panel.ChatID, panel.TopicID, panel.OutputMessageID, outputText, nil); err != nil {
			return err
		}
		panel.LastOutputHash = outputHash
	}

	panel.CurrentTurnID = snapshot.LatestTurnID
	panel.Status = snapshot.LatestTurnStatus
	return s.store.UpdateThreadPanelState(ctx, panel.ID, panel.CurrentTurnID, panel.Status, panel.LastSummaryHash, panel.LastToolHash, panel.LastOutputHash, panel.LastFinalNoticeFP)
}

func (s *Service) maybeUpdateRunNotice(ctx context.Context, sender Sender, panel *model.ThreadPanel, thread model.Thread, snapshot *appserver.ThreadReadSnapshot) error {
	if panel == nil || panel.RunNoticeMessageID == 0 || !shouldSendRunNotice(panel.SourceMode, snapshot) {
		return nil
	}
	if isTerminalStatus(snapshot.LatestTurnStatus) {
		return nil
	}
	text, fp := s.renderRunNotice(ctx, thread, snapshot, panel.SourceMode)
	if fp == panel.LastRunNoticeFP {
		return nil
	}
	if err := sender.EditMessage(ctx, panel.ChatID, panel.TopicID, panel.RunNoticeMessageID, text, nil); err != nil {
		s.setError(ctx, fmt.Errorf("edit run notice: %w", err))
		return nil
	}
	panel.LastRunNoticeFP = fp
	return s.store.UpdateThreadPanelRunNotice(ctx, panel.ID, panel.RunNoticeMessageID, panel.LastRunNoticeFP)
}

func (s *Service) renderSummaryPanel(ctx context.Context, thread model.Thread, snapshot *appserver.ThreadReadSnapshot, pending *model.PendingApproval) (model.RenderedMessage, [][]model.ButtonSpec, string) {
	buttons := [][]model.ButtonSpec{
		{
			s.callbackButton(ctx, "Stop", "stop_turn", thread.ID, snapshot.LatestTurnID, "", nil),
			s.callbackButton(ctx, "Steer", "arm_steer", thread.ID, snapshot.LatestTurnID, "", nil),
		},
		{
			s.callbackButton(ctx, "Show", "show_thread", thread.ID, snapshot.LatestTurnID, "", nil),
			s.callbackButton(ctx, "Bind here", "bind_here", thread.ID, snapshot.LatestTurnID, "", nil),
			s.callbackButton(ctx, "Show context", "show_context", thread.ID, snapshot.LatestTurnID, "", nil),
		},
	}
	if pending != nil {
		switch pending.PromptKind {
		case "approval":
			buttons = append(buttons,
				[]model.ButtonSpec{
					s.callbackButton(ctx, "Approve", "approve", pending.ThreadID, pending.TurnID, pending.RequestID, nil),
					s.callbackButton(ctx, "Approve Session", "approve_session", pending.ThreadID, pending.TurnID, pending.RequestID, nil),
				},
				[]model.ButtonSpec{
					s.callbackButton(ctx, "Deny", "deny", pending.ThreadID, pending.TurnID, pending.RequestID, nil),
					s.callbackButton(ctx, "Cancel", "cancel", pending.ThreadID, pending.TurnID, pending.RequestID, nil),
				},
			)
		case "user_input":
			if optionRows := s.pendingInputButtons(ctx, pending); len(optionRows) > 0 {
				buttons = append(buttons, optionRows...)
			}
		}
	}
	entries := append([]appserver.AgentMessageEntry(nil), snapshot.LatestAgentMessageEntries...)
	for {
		rendered := s.renderSummaryPanelMarkdown(ctx, thread, snapshot, entries, pending)
		if len(rendered) <= 1 {
			message := firstRenderedMessage(rendered)
			return message, buttons, hashStrings(tgformat.HashRendered(message), flattenButtonSpecs(buttons))
		}
		if len(entries) == 0 {
			message := firstRenderedMessage(rendered)
			return message, buttons, hashStrings(tgformat.HashRendered(message), flattenButtonSpecs(buttons))
		}
		entries = entries[:len(entries)-1]
	}
}

func (s *Service) renderSummaryPanelMarkdown(ctx context.Context, thread model.Thread, snapshot *appserver.ThreadReadSnapshot, entries []appserver.AgentMessageEntry, pending *model.PendingApproval) []model.RenderedMessage {
	segments := []tgformat.Segment{tgformat.Plain(strings.Join([]string{
		s.visualHeader(ctx, "commentary", thread, snapshot.LatestTurnID),
		fmt.Sprintf("Status: %s", readableStatus(snapshot.LatestTurnStatus, thread.Status)),
	}, "\n"))}
	if len(entries) == 0 {
		segments = append(segments, tgformat.Plain("\n\nNo agent messages yet."))
	} else {
		displayEntries := chronologicalAgentEntries(entries)
		for _, message := range displayEntries {
			segments = append(segments,
				tgformat.Plain(fmt.Sprintf("\n\n%s\n", summaryAgentLabel(message))),
				tgformat.Markdown(strings.TrimSpace(message.Text)),
			)
		}
	}
	if pending != nil {
		switch pending.PromptKind {
		case "approval":
			segments = append(segments, tgformat.Plain("\n\n[Approval]\n"), tgformat.Markdown(strings.TrimSpace(pending.Question)))
		case "user_input":
			segments = append(segments, tgformat.Plain("\n\n[Question]\n"), tgformat.Markdown(strings.TrimSpace(pending.Question)))
		}
	} else if snapshot != nil && snapshot.PlanPrompt != nil {
		segments = append(segments, tgformat.Plain("\n\n[Plan]\nWaiting for input. Reply to the [Plan] message or use /reply."))
	}
	return tgformat.RenderSegments(segments, tgformat.TelegramMessageLimit)
}

func (s *Service) renderPlanPromptCard(ctx context.Context, thread model.Thread, prompt *model.PlanPrompt) (model.RenderedMessage, [][]model.ButtonSpec, string) {
	header := strings.Join([]string{
		s.visualHeader(ctx, "Plan", thread, prompt.TurnID),
		fmt.Sprintf("Status: %s", firstNonEmpty(prompt.Status, "waiting for input")),
	}, "\n")
	body := strings.TrimSpace(prompt.Question) + "\n\nReply to this message or use /reply " + thread.ID + " <text>."
	message := renderSingleMarkdownCard(header, body)
	buttons := s.planPromptButtons(ctx, prompt)
	return message, buttons, hashStrings(tgformat.HashRendered(message), flattenButtonSpecs(buttons))
}

func (s *Service) planPromptButtons(ctx context.Context, prompt *model.PlanPrompt) [][]model.ButtonSpec {
	if prompt == nil || len(prompt.Options) == 0 {
		return nil
	}
	rows := make([][]model.ButtonSpec, 0, len(prompt.Options))
	for _, option := range prompt.Options {
		option = strings.TrimSpace(option)
		if option == "" {
			continue
		}
		rows = append(rows, []model.ButtonSpec{
			s.callbackButton(ctx, option, "answer_choice", prompt.ThreadID, prompt.TurnID, prompt.RequestID, map[string]any{"text": option}),
		})
	}
	return rows
}

func chronologicalAgentEntries(entries []appserver.AgentMessageEntry) []appserver.AgentMessageEntry {
	out := make([]appserver.AgentMessageEntry, 0, len(entries))
	for index := len(entries) - 1; index >= 0; index-- {
		out = append(out, entries[index])
	}
	return out
}

func (s *Service) renderToolPanel(ctx context.Context, thread model.Thread, snapshot *appserver.ThreadReadSnapshot) (string, string) {
	header := s.visualHeader(ctx, "Tool", thread, snapshot.LatestTurnID)
	label := strings.TrimSpace(snapshot.LatestToolLabel)
	if label == "" {
		text := strings.Join([]string{header, "No tool activity yet."}, "\n")
		return text, hashStrings(text)
	}

	escapedHeader := html.EscapeString(header)
	renderedTool := renderToolCommandBlock(label, outputMessageLimit-len(escapedHeader)-2)
	lines := []string{escapedHeader, renderedTool}
	if status := strings.TrimSpace(snapshot.LatestToolStatus); status != "" {
		lines = append(lines, html.EscapeString(fmt.Sprintf("Status: %s", status)))
	}
	text := strings.Join(lines, "\n")
	return text, hashStrings(text)
}

func (s *Service) renderOutputPanel(ctx context.Context, thread model.Thread, snapshot *appserver.ThreadReadSnapshot) (string, string) {
	header := s.visualHeader(ctx, "Output", thread, snapshot.LatestTurnID)
	output := strings.ReplaceAll(snapshot.LatestToolOutput, "\r\n", "\n")
	output = strings.TrimSpace(output)
	if output == "" {
		text := strings.Join([]string{header, "No tool output yet."}, "\n")
		return text, hashStrings(text)
	}

	escapedHeader := html.EscapeString(header)
	text := strings.Join([]string{
		escapedHeader,
		renderHTMLCodeBlockTail(trimOutputTail(output, outputMessageLimit), outputMessageLimit-len(escapedHeader)-1, ""),
	}, "\n")
	return text, hashStrings(text)
}

func renderToolCommandBlock(label string, maxLen int) string {
	tool := parseShellTool(label)
	if tool.ShellName == "" {
		return renderHTMLCodeBlockTail(label, maxLen, "")
	}
	header := fmt.Sprintf("[Shell:%s", html.EscapeString(tool.ShellName))
	if tool.DisplayName != "" {
		header += fmt.Sprintf(" (%s)", html.EscapeString(tool.DisplayName))
	} else {
		header += " (⚠️UNKNOWN SHELL⚠️)"
	}
	header += "]"
	codeBudget := maxLen - len(header) - 1
	return strings.Join([]string{
		header,
		renderHTMLCodeBlockTail(tool.Command, codeBudget, tool.Language),
	}, "\n")
}

type shellTool struct {
	ShellName   string
	DisplayName string
	Language    string
	Command     string
}

func parseShellTool(label string) shellTool {
	tokens := splitShellCommandLine(label)
	if len(tokens) < 2 {
		return shellTool{}
	}
	shellName := shellBaseName(tokens[0])
	flagIndex := -1
	for index := 1; index < len(tokens); index++ {
		if isShellCommandFlag(tokens[index]) {
			flagIndex = index
			break
		}
	}
	if flagIndex < 0 || flagIndex+1 >= len(tokens) {
		return shellTool{}
	}
	command := strings.TrimSpace(strings.Join(tokens[flagIndex+1:], " "))
	if command == "" {
		return shellTool{}
	}
	displayName, language, known := knownShell(shellName)
	if !known && !looksLikeExecutableShell(shellName) {
		return shellTool{}
	}
	return shellTool{
		ShellName:   shellName,
		DisplayName: displayName,
		Language:    language,
		Command:     command,
	}
}

func splitShellCommandLine(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	tokens := []string{}
	current := strings.Builder{}
	inQuotes := false
	escaped := false
	for _, r := range value {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && inQuotes:
			current.WriteRune(r)
		case r == '"':
			inQuotes = !inQuotes
		case !inQuotes && (r == ' ' || r == '\t' || r == '\n'):
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func shellBaseName(path string) string {
	path = strings.Trim(strings.TrimSpace(path), `"`)
	path = strings.ReplaceAll(path, "/", "\\")
	if index := strings.LastIndex(path, "\\"); index >= 0 {
		return strings.TrimSpace(path[index+1:])
	}
	return path
}

func isShellCommandFlag(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "-command", "-c", "/c":
		return true
	default:
		return false
	}
}

func knownShell(shellName string) (string, string, bool) {
	switch strings.ToLower(strings.TrimSpace(shellName)) {
	case "pwsh", "pwsh.exe", "powershell", "powershell.exe":
		return "PowerShell", "powershell", true
	case "bash", "bash.exe", "sh", "sh.exe", "zsh", "zsh.exe":
		return "Bash", "bash", true
	case "cmd", "cmd.exe":
		return "Command Prompt", "batch", true
	default:
		return "", "", false
	}
}

func looksLikeExecutableShell(shellName string) bool {
	shellName = strings.TrimSpace(shellName)
	return strings.Contains(shellName, ".") || strings.HasSuffix(strings.ToLower(shellName), "sh")
}

func renderHTMLCodeBlock(content, language string) string {
	language = strings.TrimSpace(strings.ToLower(language))
	if language != "" {
		return fmt.Sprintf(`<pre><code class="language-%s">%s</code></pre>`, html.EscapeString(language), html.EscapeString(content))
	}
	return fmt.Sprintf("<pre><code>%s</code></pre>", html.EscapeString(content))
}

func renderHTMLCodeBlockTail(content string, maxLen int, language string) string {
	content = strings.TrimSpace(content)
	if maxLen <= len("<pre><code></code></pre>") {
		return renderHTMLCodeBlock("", language)
	}
	runes := []rune(content)
	if len(runes) == 0 {
		return renderHTMLCodeBlock("", language)
	}
	if block := renderHTMLCodeBlock(content, language); len(block) <= maxLen {
		return block
	}
	bestStart := len(runes)
	low, high := 0, len(runes)
	for low <= high {
		mid := (low + high) / 2
		candidate := string(runes[mid:])
		if len(renderHTMLCodeBlock(candidate, language)) <= maxLen {
			bestStart = mid
			high = mid - 1
		} else {
			low = mid + 1
		}
	}
	tail := string(runes[bestStart:])
	if bestStart > 0 {
		if newline := strings.Index(tail, "\n"); newline >= 0 && newline+1 < len(tail) {
			tail = tail[newline+1:]
		}
	}
	return renderHTMLCodeBlock(strings.TrimSpace(tail), language)
}

func formatSummaryAgentMessage(entry appserver.AgentMessageEntry) string {
	text := strings.TrimSpace(entry.Text)
	if text == "" {
		return ""
	}
	phase := strings.TrimSpace(strings.ToLower(entry.Phase))
	switch phase {
	case "", "message", "final_answer":
		return text
	default:
		return fmt.Sprintf("(%s) %s", phase, text)
	}
}

func summaryAgentLabel(entry appserver.AgentMessageEntry) string {
	phase := strings.TrimSpace(strings.ToLower(entry.Phase))
	switch phase {
	case "":
		return "[agent]"
	case "final_answer":
		return "[final]"
	default:
		return fmt.Sprintf("[%s]", phase)
	}
}

func firstRenderedMessage(messages []model.RenderedMessage) model.RenderedMessage {
	if len(messages) == 0 {
		return model.RenderedMessage{Text: " "}
	}
	return messages[0]
}

func lastMessageID(ids []int64) int64 {
	if len(ids) == 0 {
		return 0
	}
	return ids[len(ids)-1]
}

func firstMessageID(ids []int64) int64 {
	if len(ids) == 0 {
		return 0
	}
	return ids[0]
}

func trimOutputTail(output string, limit int) string {
	if limit <= 0 || len(output) <= limit {
		return output
	}
	window := output[len(output)-limit:]
	if newline := strings.Index(window, "\n"); newline >= 0 && newline+1 < len(window) {
		window = window[newline+1:]
	}
	return strings.TrimSpace(window)
}

func readableStatus(turnStatus, threadStatus string) string {
	if strings.TrimSpace(turnStatus) != "" {
		return turnStatus
	}
	if strings.TrimSpace(threadStatus) != "" {
		return threadStatus
	}
	return "idle"
}

func (s *Service) pendingInputButtons(ctx context.Context, pending *model.PendingApproval) [][]model.ButtonSpec {
	options := pendingInputOptions(pending)
	if len(options) == 0 {
		return nil
	}
	rows := make([][]model.ButtonSpec, 0, len(options))
	for _, option := range options {
		rows = append(rows, []model.ButtonSpec{
			s.callbackButton(ctx, option, "answer_choice", pending.ThreadID, pending.TurnID, pending.RequestID, map[string]any{"text": option}),
		})
	}
	return rows
}

func pendingInputOptions(pending *model.PendingApproval) []string {
	if pending == nil || strings.TrimSpace(pending.PayloadJSON) == "" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return nil
	}
	return extractChoiceOptions(payload)
}

func effectivePlanPrompt(pending *model.PendingApproval, snapshot *appserver.ThreadReadSnapshot) *model.PlanPrompt {
	if pending != nil && pending.PromptKind == "user_input" {
		options := pendingInputOptions(pending)
		fp := hashStrings("planPrompt", model.PromptSourceServerRequest, pending.RequestID, pending.ThreadID, pending.TurnID, pending.Question, strings.Join(options, "\x1f"))
		return &model.PlanPrompt{
			PromptID:    "request:" + pending.RequestID,
			Source:      model.PromptSourceServerRequest,
			ThreadID:    pending.ThreadID,
			TurnID:      pending.TurnID,
			ItemID:      pending.ItemID,
			RequestID:   pending.RequestID,
			Question:    firstNonEmpty(pending.Question, "Input required."),
			Options:     options,
			Fingerprint: fp,
			Status:      "waiting for input",
		}
	}
	if snapshot != nil && snapshot.PlanPrompt != nil {
		return snapshot.PlanPrompt
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func extractChoiceOptions(payload map[string]any) []string {
	keys := []string{"choices", "options", "suggestions", "responses"}
	for _, key := range keys {
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
				if text := cleanPayloadString(typed); text != "" {
					out = append(out, text)
				}
			case map[string]any:
				if text := firstPayloadString(typed, "label", "text", "value"); text != "" {
					out = append(out, text)
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if questions, ok := payload["questions"].([]any); ok {
		out := make([]string, 0)
		seen := map[string]bool{}
		for _, rawQuestion := range questions {
			question, _ := rawQuestion.(map[string]any)
			if question == nil {
				continue
			}
			options, _ := question["options"].([]any)
			for _, rawOption := range options {
				option, _ := rawOption.(map[string]any)
				if option == nil {
					continue
				}
				label := firstPayloadString(option, "label", "text", "value")
				if label == "" || seen[label] {
					continue
				}
				seen[label] = true
				out = append(out, label)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func (s *Service) armSteer(ctx context.Context, chatID, topicID int64, threadID, turnID string, panelID int64) error {
	return s.store.ArmSteerState(ctx, model.SteerState{
		ChatKey:   model.ChatKey(chatID, topicID),
		ChatID:    chatID,
		TopicID:   topicID,
		ThreadID:  threadID,
		TurnID:    turnID,
		PanelID:   panelID,
		ExpiresAt: model.TimeString(time.Now().UTC().Add(steerTTL).Format(time.RFC3339Nano)),
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})
}

func (s *Service) resolveArmedSteer(ctx context.Context, chatID, topicID int64) (*model.SteerState, error) {
	state, err := s.store.GetSteerState(ctx, chatID, topicID)
	if err != nil || state == nil {
		return state, err
	}
	if expiresAt := parseTime(state.ExpiresAt); !expiresAt.IsZero() && time.Now().UTC().After(expiresAt) {
		_ = s.store.ClearSteerState(ctx, chatID, topicID)
		return nil, nil
	}
	return state, nil
}

func sanitizeFileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "thread"
	}
	replacer := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_", " ", "_")
	return replacer.Replace(value)
}

func hashStrings(parts ...string) string {
	hasher := sha1.New()
	for _, part := range parts {
		_, _ = hasher.Write([]byte(part))
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func flattenButtonSpecs(rows [][]model.ButtonSpec) string {
	out := make([]string, 0, len(rows)*2)
	for _, row := range rows {
		for _, button := range row {
			out = append(out, button.Text)
		}
	}
	return strings.Join(out, "\x1f")
}

func isTerminalStatus(status string) bool {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "completed", "interrupted", "failed", "cancelled", "canceled":
		return true
	default:
		return false
	}
}
