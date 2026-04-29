package daemon

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

type recordedMessage struct {
	chatID    int64
	topicID   int64
	messageID int64
	text      string
	entities  []model.MessageEntity
	buttons   [][]model.ButtonSpec
}

type recordedDocument struct {
	chatID   int64
	topicID  int64
	fileName string
	filePath string
	data     []byte
	caption  string
}

type recordingSender struct {
	messages  []recordedMessage
	documents []recordedDocument
	edits     []recordedMessage
	deletes   []recordedMessage
}

func (s *recordingSender) SendMessage(ctx context.Context, chatID, topicID int64, text string, buttons [][]model.ButtonSpec) (int64, error) {
	messageID := int64(len(s.messages) + 1)
	s.messages = append(s.messages, recordedMessage{chatID: chatID, topicID: topicID, messageID: messageID, text: text, buttons: buttons})
	return messageID, nil
}

func (s *recordingSender) SendRenderedMessages(ctx context.Context, chatID, topicID int64, messages []model.RenderedMessage, buttons [][]model.ButtonSpec) ([]int64, error) {
	ids := make([]int64, 0, len(messages))
	for _, message := range messages {
		messageID := int64(len(s.messages) + 1)
		s.messages = append(s.messages, recordedMessage{chatID: chatID, topicID: topicID, messageID: messageID, text: message.Text, entities: message.Entities, buttons: buttons})
		ids = append(ids, messageID)
	}
	return ids, nil
}

func (s *recordingSender) EditMessage(ctx context.Context, chatID, topicID, messageID int64, text string, buttons [][]model.ButtonSpec) error {
	s.edits = append(s.edits, recordedMessage{chatID: chatID, topicID: topicID, messageID: messageID, text: text, buttons: buttons})
	return nil
}

func (s *recordingSender) EditRenderedMessage(ctx context.Context, chatID, topicID, messageID int64, rendered model.RenderedMessage, buttons [][]model.ButtonSpec) error {
	s.edits = append(s.edits, recordedMessage{chatID: chatID, topicID: topicID, messageID: messageID, text: rendered.Text, entities: rendered.Entities, buttons: buttons})
	return nil
}

func (s *recordingSender) DeleteMessage(ctx context.Context, chatID, topicID, messageID int64) error {
	s.deletes = append(s.deletes, recordedMessage{chatID: chatID, topicID: topicID, messageID: messageID})
	return nil
}

func (s *recordingSender) SendDocumentData(ctx context.Context, chatID, topicID int64, fileName string, data []byte, caption string) (int64, error) {
	s.documents = append(s.documents, recordedDocument{
		chatID:   chatID,
		topicID:  topicID,
		fileName: fileName,
		data:     append([]byte(nil), data...),
		caption:  caption,
	})
	return int64(len(s.documents)), nil
}

func hasRecordedEntity(entities []model.MessageEntity, entityType, language string) bool {
	for _, entity := range entities {
		if entity.Type != entityType {
			continue
		}
		if language == "" || entity.Language == language {
			return true
		}
	}
	return false
}

func hasHeaderKind(text, kind string) bool {
	firstLine := strings.SplitN(text, "\n", 2)[0]
	return strings.Contains(firstLine, "["+kind+"]")
}

func hasThreadChip(text, threadID string) bool {
	return strings.Contains(strings.SplitN(text, "\n", 2)[0], "[T:"+visualShortID(threadID)+"]")
}

func TestSyncThreadPanelDoesNotDuplicateFinalAnswerOnRepeatedSync(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-1",
		Title:       "Observer smoke",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-1",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-fp-1",
		LatestFinalText:  "All work complete.",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "stable")
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "stable")

	finalCount := 0
	for _, message := range sender.edits {
		if hasHeaderKind(message.text, "Final") {
			finalCount++
		}
	}
	if finalCount != 1 {
		t.Fatalf("final edit count = %d, want 1; edits=%#v", finalCount, sender.edits)
	}
	if len(sender.messages) != 3 {
		t.Fatalf("message count = %d, want 3 live trio messages on first sync only; messages=%#v", len(sender.messages), sender.messages)
	}
	if len(sender.documents) != 0 {
		t.Fatalf("documents = %#v, want no tool documents for a completed turn without tool output", sender.documents)
	}
	if len(sender.deletes) != 2 {
		t.Fatalf("deletes = %#v, want best-effort delete for tool/output messages", sender.deletes)
	}

	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil {
		t.Fatal("GetCurrentThreadPanel returned nil")
	}
	if panel.SourceMode != "stable" {
		t.Fatalf("panel SourceMode = %q, want stable", panel.SourceMode)
	}

	if panel.LastFinalNoticeFP != "final-fp-1" {
		t.Fatalf("panel LastFinalNoticeFP = %q, want final-fp-1", panel.LastFinalNoticeFP)
	}
}

func TestSyncThreadPanelFormatsFinalAnswerMarkdownWithEntities(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-md-final",
		Title:       "Markdown final",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-md-final",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-md-fp",
		LatestFinalText:  "Run `rg`:\n\n```bash\nrg -n 'Authorization' stellar_ws.txt\n```",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "explicit")

	final := recordedMessage{}
	for _, message := range sender.edits {
		if hasHeaderKind(message.text, "Final") {
			final = message
			break
		}
	}
	if final.text == "" {
		t.Fatalf("final message not found in %#v", sender.messages)
	}
	if strings.Contains(final.text, "```") {
		t.Fatalf("final message still contains raw markdown fence: %q", final.text)
	}
	if !hasRecordedEntity(final.entities, "code", "") {
		t.Fatalf("final entities = %#v, want inline code entity", final.entities)
	}
	if !hasRecordedEntity(final.entities, "pre", "bash") {
		t.Fatalf("final entities = %#v, want bash pre entity", final.entities)
	}
}

func TestFinalTransitionDeletesRunNoticeToolAndOutputButKeepsUser(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-final-delete-run-notice",
		Title:       "Final cleanup",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	initial := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-final-cleanup",
		LatestTurnStatus:      "inProgress",
		LatestUserMessageID:   "user-final-cleanup",
		LatestUserMessageText: "Run cleanup smoke.",
		LatestUserMessageFP:   "user-final-cleanup-fp",
		LatestToolID:          "tool-final-cleanup",
		LatestToolLabel:       "Write-Output cleanup",
		LatestToolStatus:      "running",
		LatestToolFP:          "tool-final-cleanup-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, initial); err != nil {
		t.Fatalf("UpsertSnapshot(initial) failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.messages) != 5 {
		t.Fatalf("messages = %#v, want New run + [User] + trio", sender.messages)
	}
	if strings.Contains(sender.messages[0].text, "Status:") {
		t.Fatalf("run notice text = %q, want no status", sender.messages[0].text)
	}
	if strings.Contains(sender.messages[1].text, "Status:") {
		t.Fatalf("user notice text = %q, want no status", sender.messages[1].text)
	}
	runNoticeID := sender.messages[0].messageID
	userID := sender.messages[1].messageID
	toolID := sender.messages[3].messageID
	outputID := sender.messages[4].messageID

	completed := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-final-cleanup",
		LatestTurnStatus:      "completed",
		LatestUserMessageID:   "user-final-cleanup",
		LatestUserMessageText: "Run cleanup smoke.",
		LatestUserMessageFP:   "user-final-cleanup-fp",
		LatestFinalFP:         "final-cleanup-fp",
		LatestFinalText:       "Cleanup done.",
		LatestAgentMessageEntries: []appserver.AgentMessageEntry{{
			ID:    "agent-final-cleanup",
			Phase: "commentary",
			Text:  "This completed commentary belongs in Details only.",
			FP:    "agent-final-cleanup-fp",
		}},
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, completed); err != nil {
		t.Fatalf("UpsertSnapshot(completed) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.deletes) != 3 {
		t.Fatalf("deletes = %#v, want New run + tool + output deletes", sender.deletes)
	}
	wantDeletes := []int64{runNoticeID, toolID, outputID}
	for index, want := range wantDeletes {
		if sender.deletes[index].messageID != want {
			t.Fatalf("delete[%d] = %d, want %d; deletes=%#v", index, sender.deletes[index].messageID, want, sender.deletes)
		}
	}
	for _, deleteMessage := range sender.deletes {
		if deleteMessage.messageID == userID {
			t.Fatalf("[User] message %d was deleted unexpectedly: %#v", userID, sender.deletes)
		}
	}
	if len(sender.edits) == 0 || !hasHeaderKind(sender.edits[len(sender.edits)-1].text, "Final") {
		t.Fatalf("edits = %#v, want summary edited into Final", sender.edits)
	}
	finalText := sender.edits[len(sender.edits)-1].text
	if strings.Contains(finalText, "[commentary]") || strings.Contains(finalText, "This completed commentary belongs in Details only.") {
		t.Fatalf("final text = %q, want final answer without commentary transcript", finalText)
	}
}

func TestSummaryPanelFormatsCommentaryMarkdownWithoutFinalLabel(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-md-summary",
		Title:       "Markdown summary",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-md-summary",
		LatestTurnStatus: "inProgress",
		LatestAgentMessageEntries: []appserver.AgentMessageEntry{{
			ID:    "agent-1",
			Phase: "commentary",
			Text:  "Checking `node`:\n\n```powershell\nnode -v\n```",
			FP:    "agent-1-fp",
		}},
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "explicit")

	if len(sender.messages) == 0 {
		t.Fatal("no summary message was sent")
	}
	summary := sender.messages[0]
	if hasHeaderKind(summary.text, "Final") {
		t.Fatalf("summary text = %q, must not label commentary as final", summary.text)
	}
	if strings.Contains(summary.text, "```") {
		t.Fatalf("summary text still contains raw markdown fence: %q", summary.text)
	}
	if !hasRecordedEntity(summary.entities, "code", "") {
		t.Fatalf("summary entities = %#v, want inline code entity", summary.entities)
	}
	if !hasRecordedEntity(summary.entities, "pre", "powershell") {
		t.Fatalf("summary entities = %#v, want powershell pre entity", summary.entities)
	}
}

func TestSummaryPanelDisplaysAgentMessagesChronologically(t *testing.T) {
	t.Parallel()

	thread := model.Thread{
		ID:          "thread-order",
		Title:       "Summary order",
		ProjectName: "Codex",
	}
	snapshot := &appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-order",
		LatestTurnStatus: "inProgress",
		LatestAgentMessageEntries: []appserver.AgentMessageEntry{
			{ID: "agent-3", Phase: "commentary", Text: "THIRD newest"},
			{ID: "agent-2", Phase: "commentary", Text: "SECOND middle"},
			{ID: "agent-1", Phase: "commentary", Text: "FIRST oldest"},
		},
	}

	service := newTestService(t)
	messages := service.renderSummaryPanelMarkdown(context.Background(), thread, snapshot, snapshot.LatestAgentMessageEntries, nil)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	text := messages[0].Text
	first := strings.Index(text, "FIRST oldest")
	second := strings.Index(text, "SECOND middle")
	third := strings.Index(text, "THIRD newest")
	if first < 0 || second < 0 || third < 0 {
		t.Fatalf("summary text missing expected entries: %q", text)
	}
	if !(first < second && second < third) {
		t.Fatalf("summary text order is not chronological: %q", text)
	}
	if strings.Contains(text, "1. [commentary]") || strings.Contains(text, "2. [commentary]") || strings.Contains(text, "3. [commentary]") {
		t.Fatalf("summary text must not number commentary entries: %q", text)
	}
}

func TestSyncThreadPanelDoesNotUseDocumentDeliveryForToolOutput(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-tool",
		Title:       "Tool output smoke",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	snapshot := appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-tool",
		LatestTurnStatus: "completed",
		LatestToolID:     "tool-1",
		LatestToolKind:   "commandExecution",
		LatestToolLabel:  "pwsh -Command node -v",
		LatestToolStatus: "completed",
		LatestToolOutput: "v22.22.2\n",
		LatestToolFP:     "tool-fp-1",
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	compact := appserver.CompactSnapshot(nil, snapshot, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, compact); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}, thread.ID, false, "explicit")

	if len(sender.documents) != 0 {
		t.Fatalf("documents = %#v, want no SendDocument path for tool output", sender.documents)
	}
	if len(sender.messages) != 3 {
		t.Fatalf("message count = %d, want 3 trio messages only", len(sender.messages))
	}
	if got := sender.messages[1].text; strings.HasPrefix(got, "<pre><code>[Tool]") || !strings.Contains(got, "[Shell:pwsh (PowerShell)]\n<pre><code class=\"language-powershell\">node -v</code></pre>") {
		t.Fatalf("tool message = %q, want shell line and command-only HTML code block", got)
	}
	if got := sender.messages[2].text; strings.HasPrefix(got, "<pre><code>[Output]") || !strings.Contains(got, "<pre><code>v22.22.2</code></pre>") {
		t.Fatalf("output message = %q, want plain header and output-only HTML code block", got)
	}
}

func TestGlobalObserverSyncSendsUserRequestNoticeOnceBeforeTrio(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-user-notice",
		Title:       "GUI prompt",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-user-notice",
		LatestTurnStatus:      "inProgress",
		LatestUserMessageID:   "user-item-1",
		LatestUserMessageText: "Check `node -v` from GUI.",
		LatestUserMessageFP:   "user-fp-1",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 5 {
		t.Fatalf("message count = %d, want 5 (New run + [User] + trio once); messages=%#v", len(sender.messages), sender.messages)
	}
	if !strings.Contains(sender.messages[0].text, "New run:") {
		t.Fatalf("first message = %q, want New run before [User]", sender.messages[0].text)
	}
	if strings.Contains(sender.messages[0].text, "Status:") {
		t.Fatalf("run notice text = %q, want no status", sender.messages[0].text)
	}
	if !hasHeaderKind(sender.messages[1].text, "User") {
		t.Fatalf("second message = %q, want [User] before trio", sender.messages[1].text)
	}
	if strings.Contains(sender.messages[1].text, "Status:") {
		t.Fatalf("user notice text = %q, want no status", sender.messages[1].text)
	}
	if strings.Contains(sender.messages[1].text, "```") {
		t.Fatalf("user notice contains raw markdown: %q", sender.messages[1].text)
	}
	if !hasRecordedEntity(sender.messages[1].entities, "code", "") {
		t.Fatalf("user notice entities = %#v, want inline code entity", sender.messages[1].entities)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.RunNoticeMessageID != sender.messages[0].messageID || panel.UserMessageID != sender.messages[1].messageID || panel.LastUserNoticeFP != "user-fp-1" {
		t.Fatalf("panel notice state = %#v, want run id %d and user id %d / user-fp-1", panel, sender.messages[0].messageID, sender.messages[1].messageID)
	}
	route, err := service.store.ResolveMessageRoute(ctx, target.ChatID, target.TopicID, sender.messages[1].messageID)
	if err != nil {
		t.Fatalf("ResolveMessageRoute failed: %v", err)
	}
	if route == nil || route.ThreadID != thread.ID || route.TurnID != "turn-user-notice" || route.ItemID != "user-item-1" {
		t.Fatalf("user notice route = %#v, want thread/turn/item route", route)
	}
}

func TestGlobalObserverSyncCreatesRunNoticeAndUserPlaceholderBeforeTrioWithoutUserPrompt(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-orientation-no-user",
		Title:       "GUI tool first",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-tool-first",
		LatestTurnStatus: "inProgress",
		LatestToolID:     "tool-first",
		LatestToolLabel:  "Start-Sleep -Seconds 900",
		LatestToolStatus: "running",
		LatestToolFP:     "tool-first-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 5 {
		t.Fatalf("messages = %#v, want New run + [User placeholder] + trio", sender.messages)
	}
	if !strings.Contains(sender.messages[0].text, "New run:") {
		t.Fatalf("first message = %q, want New run", sender.messages[0].text)
	}
	if strings.Contains(sender.messages[0].text, "Status:") {
		t.Fatalf("run notice text = %q, want no status", sender.messages[0].text)
	}
	if !hasHeaderKind(sender.messages[1].text, "User") || !strings.Contains(sender.messages[1].text, "User prompt was not available") {
		t.Fatalf("second message = %q, want [User] placeholder", sender.messages[1].text)
	}
	if strings.Contains(sender.messages[1].text, "Status:") {
		t.Fatalf("user placeholder text = %q, want no status", sender.messages[1].text)
	}
	if !hasHeaderKind(sender.messages[2].text, "commentary") || !hasHeaderKind(sender.messages[3].text, "Tool") || !hasHeaderKind(sender.messages[4].text, "Output") {
		t.Fatalf("messages = %#v, want trio after New run and [User]", sender.messages)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.RunNoticeMessageID != sender.messages[0].messageID || panel.UserMessageID != sender.messages[1].messageID || panel.LastUserNoticeFP != "" {
		t.Fatalf("panel = %#v, want run notice id and user placeholder id without user fp", panel)
	}
}

func TestLateUserPromptEditsExistingUserPlaceholder(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-late-user-orientation",
		Title:       "Late user",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	initial := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-late-user",
		LatestTurnStatus: "inProgress",
		LatestToolID:     "tool-late-user",
		LatestToolLabel:  "Start-Sleep -Seconds 900",
		LatestToolStatus: "running",
		LatestToolFP:     "tool-late-user-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, initial); err != nil {
		t.Fatalf("UpsertSnapshot(initial) failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.messages) != 5 || !strings.Contains(sender.messages[0].text, "New run:") || !hasHeaderKind(sender.messages[1].text, "User") {
		t.Fatalf("initial messages = %#v, want New run + [User placeholder] + trio", sender.messages)
	}
	userMessageID := sender.messages[1].messageID

	late := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-late-user",
		LatestTurnStatus:      "inProgress",
		LatestUserMessageID:   "user-late",
		LatestUserMessageText: "Use `mtkachenko2` config.",
		LatestUserMessageFP:   "user-late-fp",
		LatestToolID:          "tool-late-user",
		LatestToolLabel:       "Start-Sleep -Seconds 900",
		LatestToolStatus:      "running",
		LatestToolFP:          "tool-late-user-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, late); err != nil {
		t.Fatalf("UpsertSnapshot(late) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 5 {
		t.Fatalf("messages = %#v, want no late appended [User] message", sender.messages)
	}
	foundEdit := false
	for _, edit := range sender.edits {
		if edit.messageID == userMessageID && hasHeaderKind(edit.text, "User") && strings.Contains(edit.text, "mtkachenko2") {
			if strings.Contains(edit.text, "Status:") {
				t.Fatalf("user placeholder edit text = %q, want no status", edit.text)
			}
			foundEdit = true
		}
	}
	if !foundEdit {
		t.Fatalf("edits = %#v, want user placeholder edited into [User]", sender.edits)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.UserMessageID != userMessageID || panel.LastUserNoticeFP != "user-late-fp" {
		t.Fatalf("panel = %#v, want same user placeholder id and user fp", panel)
	}
	route, err := service.store.ResolveMessageRoute(ctx, target.ChatID, target.TopicID, userMessageID)
	if err != nil {
		t.Fatalf("ResolveMessageRoute failed: %v", err)
	}
	if route == nil || route.ThreadID != thread.ID || route.TurnID != "turn-late-user" || route.ItemID != "user-late" {
		t.Fatalf("user route = %#v, want edited [User] route", route)
	}

	userEditCount := 0
	for _, edit := range sender.edits {
		if edit.messageID == userMessageID {
			userEditCount++
		}
	}
	statusOnly := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-late-user",
		LatestTurnStatus:      "interrupted",
		LatestUserMessageID:   "user-late",
		LatestUserMessageText: "Use `mtkachenko2` config.",
		LatestUserMessageFP:   "user-late-fp",
		LatestToolID:          "tool-late-user",
		LatestToolLabel:       "Start-Sleep -Seconds 900",
		LatestToolStatus:      "running",
		LatestToolFP:          "tool-late-user-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, statusOnly); err != nil {
		t.Fatalf("UpsertSnapshot(statusOnly) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	afterStatusUserEditCount := 0
	for _, edit := range sender.edits {
		if edit.messageID == userMessageID {
			afterStatusUserEditCount++
		}
	}
	if afterStatusUserEditCount != userEditCount {
		t.Fatalf("user edit count after status-only sync = %d, want %d; edits=%#v", afterStatusUserEditCount, userEditCount, sender.edits)
	}
}

func TestTelegramInputSyncDoesNotDuplicateUserRequestNotice(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-telegram-input",
		Title:       "Telegram prompt",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-telegram-input",
		LatestTurnStatus:      "inProgress",
		LatestUserMessageID:   "user-telegram",
		LatestUserMessageText: "This was already sent in Telegram.",
		LatestUserMessageFP:   "user-telegram-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, true, model.PanelSourceTelegramInput)

	if len(sender.messages) != 4 {
		t.Fatalf("message count = %d, want New run + trio without [User] duplicate; messages=%#v", len(sender.messages), sender.messages)
	}
	if !strings.Contains(sender.messages[0].text, "New run:") || !strings.Contains(sender.messages[0].text, "Source: Telegram") {
		t.Fatalf("first message = %q, want Telegram New run notice", sender.messages[0].text)
	}
	if strings.Contains(sender.messages[0].text, "Status:") {
		t.Fatalf("run notice text = %q, want no status", sender.messages[0].text)
	}
	for _, message := range sender.messages {
		if hasHeaderKind(message.text, "User") {
			t.Fatalf("unexpected user notice for Telegram-originated input: %#v", sender.messages)
		}
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.SourceMode != model.PanelSourceTelegramInput || panel.RunNoticeMessageID != sender.messages[0].messageID || panel.LastUserNoticeFP != "" {
		t.Fatalf("panel = %#v, want telegram_input with empty user notice fp", panel)
	}
}

func TestMarkedTelegramOriginTurnDoesNotDuplicateUserRequestNoticeOnObserverResync(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-marked-telegram-input",
		Title:       "Marked Telegram prompt",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.markTelegramOriginTurn(ctx, thread.ID, "turn-marked-telegram"); err != nil {
		t.Fatalf("markTelegramOriginTurn failed: %v", err)
	}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		TopicID:          0,
		ProjectName:      thread.ProjectName,
		ThreadID:         thread.ID,
		SourceMode:       model.PanelSourceGlobalObserver,
		SummaryMessageID: 101,
		ToolMessageID:    102,
		OutputMessageID:  103,
		CurrentTurnID:    "turn-old",
		Status:           "completed",
		ArchiveEnabled:   true,
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-marked-telegram",
		LatestTurnStatus:      "inProgress",
		LatestUserMessageID:   "user-marked-telegram",
		LatestUserMessageText: "This was sent from Telegram and later re-polled.",
		LatestUserMessageFP:   "user-marked-telegram-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	for _, message := range sender.messages {
		if hasHeaderKind(message.text, "User") {
			t.Fatalf("unexpected user notice for marked Telegram-origin turn: %#v", sender.messages)
		}
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.LastUserNoticeFP != "" {
		t.Fatalf("panel = %#v, want no user notice fp for marked Telegram-origin turn", panel)
	}
}

func TestTelegramInputSyncAdoptsObserverPanelForSameTurn(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-adopt-observer-panel",
		Title:       "Telegram race",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		TopicID:          0,
		ProjectName:      thread.ProjectName,
		ThreadID:         thread.ID,
		SourceMode:       model.PanelSourceGlobalObserver,
		SummaryMessageID: 101,
		ToolMessageID:    102,
		OutputMessageID:  103,
		CurrentTurnID:    "turn-telegram-race",
		Status:           "inProgress",
		ArchiveEnabled:   true,
		LastSummaryHash:  "old-summary",
		LastToolHash:     "old-tool",
		LastOutputHash:   "old-output",
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	if err := service.markTelegramOriginTurn(ctx, thread.ID, "turn-telegram-race"); err != nil {
		t.Fatalf("markTelegramOriginTurn failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-telegram-race",
		LatestTurnStatus:      "completed",
		LatestUserMessageID:   "user-telegram-race",
		LatestUserMessageText: "Проверка, ответь: Test",
		LatestUserMessageFP:   "user-telegram-race-fp",
		LatestFinalText:       "Test",
		LatestFinalFP:         "final-telegram-race-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, true, model.PanelSourceTelegramInput)

	if len(sender.messages) != 0 {
		t.Fatalf("messages = %#v, want no replacement trio/final messages", sender.messages)
	}
	if len(sender.edits) == 0 || !hasHeaderKind(sender.edits[len(sender.edits)-1].text, "Final") {
		t.Fatalf("edits = %#v, want existing summary edited into Final Card", sender.edits)
	}
	if len(sender.deletes) != 2 || sender.deletes[0].messageID != 102 || sender.deletes[1].messageID != 103 {
		t.Fatalf("deletes = %#v, want old tool/output messages deleted", sender.deletes)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.SummaryMessageID != 101 || panel.SourceMode != model.PanelSourceTelegramInput || panel.LastFinalNoticeFP != "final-telegram-race-fp" {
		t.Fatalf("panel = %#v, want adopted original panel with final fp", panel)
	}
}

func TestStablePanelSendsNewUserNoticeForNewForeignTurnWithoutNewTrio(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()
	if err := service.store.SetState(ctx, "ui.panel_mode", model.PanelModeStable); err != nil {
		t.Fatalf("SetState(ui.panel_mode) failed: %v", err)
	}

	thread := model.Thread{
		ID:          "thread-stable-user-notice",
		Title:       "Stable GUI prompt",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           123456789,
		TopicID:          0,
		ProjectName:      thread.ProjectName,
		ThreadID:         thread.ID,
		SourceMode:       model.PanelSourceGlobalObserver,
		SummaryMessageID: 101,
		ToolMessageID:    102,
		OutputMessageID:  103,
		CurrentTurnID:    "turn-old",
		Status:           "completed",
		ArchiveEnabled:   true,
		LastUserNoticeFP: "user-old-fp",
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-new",
		LatestTurnStatus:      "inProgress",
		LatestUserMessageID:   "user-new",
		LatestUserMessageText: "New GUI prompt.",
		LatestUserMessageFP:   "user-new-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 0 {
		t.Fatalf("messages = %#v, want no late [User] append below legacy stable trio", sender.messages)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.UserMessageID != 0 || panel.LastUserNoticeFP != "user-old-fp" {
		t.Fatalf("panel user notice state = %#v, want legacy state unchanged without late append", panel)
	}
}

func TestSyncThreadPanelToTargetSkipsInitialTerminalGlobalObserverReplay(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-terminal-global",
		Title:       "Completed elsewhere",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Add(-10 * time.Minute).Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-terminal",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-fp-terminal",
		LatestFinalText:  "Already done.",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "global_observer")

	if len(sender.messages) != 0 {
		t.Fatalf("message count = %d, want 0 for terminal global observer replay; messages=%#v", len(sender.messages), sender.messages)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel != nil {
		t.Fatalf("expected no panel for initial terminal global observer replay, got %#v", panel)
	}
}

func TestSyncThreadPanelToTargetCreatesPanelForRecentTerminalGlobalObserverChange(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-recent-terminal-global",
		Title:       "Completed just now",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-recent-terminal",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-fp-recent-terminal",
		LatestFinalText:  "Fresh completion.",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "global_observer")

	if len(sender.messages) != 3 {
		t.Fatalf("message count = %d, want 3 live trio messages for recent terminal observer change; messages=%#v", len(sender.messages), sender.messages)
	}
	if len(sender.edits) != 1 || !hasHeaderKind(sender.edits[0].text, "Final") {
		t.Fatalf("edits = %#v, want one final-card edit", sender.edits)
	}
	if len(sender.deletes) != 2 {
		t.Fatalf("deletes = %#v, want tool/output delete after final", sender.deletes)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil {
		t.Fatal("expected panel for recent terminal observer change")
	}
}

func TestTerminalObserverPanelWithRunNoticeCollapsesWhenFinalAppears(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-terminal-run-notice-final",
		Title:       "Terminal with prompt",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	initial := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-terminal-run-notice",
		LatestTurnStatus:      "completed",
		LatestUserMessageID:   "user-terminal-run-notice",
		LatestUserMessageText: "Finish from GUI.",
		LatestUserMessageFP:   "user-terminal-run-notice-fp",
		LatestToolID:          "tool-terminal-run-notice",
		LatestToolLabel:       "go test ./...",
		LatestToolStatus:      "completed",
		LatestToolOutput:      "ok\n",
		LatestToolFP:          "tool-terminal-run-notice-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, initial); err != nil {
		t.Fatalf("UpsertSnapshot(initial) failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	if len(sender.messages) != 5 {
		t.Fatalf("initial messages = %#v, want New run + [User] + trio", sender.messages)
	}
	runNoticeID := sender.messages[0].messageID
	toolID := sender.messages[3].messageID
	outputID := sender.messages[4].messageID

	finalSnapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-terminal-run-notice",
		LatestTurnStatus:      "completed",
		LatestUserMessageID:   "user-terminal-run-notice",
		LatestUserMessageText: "Finish from GUI.",
		LatestUserMessageFP:   "user-terminal-run-notice-fp",
		LatestFinalFP:         "final-terminal-run-notice-fp",
		LatestFinalText:       "Done from final answer.",
		LatestAgentMessageEntries: []appserver.AgentMessageEntry{{
			ID:    "agent-terminal-run-notice",
			Phase: "commentary",
			Text:  "Completed commentary should stay in Details.",
			FP:    "agent-terminal-run-notice-fp",
		}},
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, finalSnapshot); err != nil {
		t.Fatalf("UpsertSnapshot(final) failed: %v", err)
	}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.deletes) != 3 {
		t.Fatalf("deletes = %#v, want New run + tool + output", sender.deletes)
	}
	wantDeletes := []int64{runNoticeID, toolID, outputID}
	for index, want := range wantDeletes {
		if sender.deletes[index].messageID != want {
			t.Fatalf("delete[%d] = %d, want %d; deletes=%#v", index, sender.deletes[index].messageID, want, sender.deletes)
		}
	}
	if len(sender.edits) == 0 {
		t.Fatalf("edits = %#v, want final edit", sender.edits)
	}
	finalEdit := sender.edits[len(sender.edits)-1]
	if !hasHeaderKind(finalEdit.text, "Final") {
		t.Fatalf("last edit = %q, want Final", finalEdit.text)
	}
	if strings.Contains(finalEdit.text, "[commentary]") || strings.Contains(finalEdit.text, "Completed commentary should stay in Details.") {
		t.Fatalf("final edit = %q, want final answer only", finalEdit.text)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.LastFinalNoticeFP != "final-terminal-run-notice-fp" {
		t.Fatalf("panel = %#v, want final fingerprint recorded", panel)
	}
}

func TestTerminalSyncDoesNotRewriteRunNotice(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-terminal-run-notice-no-edit",
		Title:       "Terminal no run edit",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:             target.ChatID,
		TopicID:            target.TopicID,
		ProjectName:        thread.ProjectName,
		ThreadID:           thread.ID,
		SourceMode:         model.PanelSourceGlobalObserver,
		SummaryMessageID:   102,
		ToolMessageID:      103,
		OutputMessageID:    104,
		RunNoticeMessageID: 101,
		LastRunNoticeFP:    "legacy-run-notice-with-status",
		CurrentTurnID:      "turn-terminal-no-run-edit",
		Status:             "inProgress",
		ArchiveEnabled:     true,
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:                thread,
		LatestTurnID:          "turn-terminal-no-run-edit",
		LatestTurnStatus:      "completed",
		LatestUserMessageID:   "user-terminal-no-run-edit",
		LatestUserMessageText: "Finish without final yet.",
		LatestUserMessageFP:   "user-terminal-no-run-edit-fp",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	for _, edit := range sender.edits {
		if edit.messageID == 101 {
			t.Fatalf("edits = %#v, want no terminal run-notice rewrite", sender.edits)
		}
	}
}

func TestRenderToolPanelUnwrapsQuotedPowerShellCommand(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	text, _ := service.renderToolPanel(context.Background(), model.Thread{
		ID:          "thread-tool-pwsh",
		Title:       "Find Swagger",
		ProjectName: "Codex",
	}, &appserver.ThreadReadSnapshot{
		LatestTurnID:     "turn-tool-pwsh",
		LatestToolLabel:  `"C:\Program Files\PowerShell\7\pwsh.exe" -Command "rg -n 'session/33655d2a' 'C:\Users\you\Downloads\stellar_ws.txt'"`,
		LatestToolStatus: "completed",
	})

	if !strings.Contains(text, "[Shell:pwsh.exe (PowerShell)]") {
		t.Fatalf("rendered tool = %q, want shell metadata line", text)
	}
	if strings.Contains(text, "C:\\Program Files\\PowerShell") {
		t.Fatalf("rendered tool = %q, want wrapper shell path omitted", text)
	}
	if !strings.Contains(text, `<pre><code class="language-powershell">rg -n &#39;session/33655d2a&#39; &#39;C:\Users\you\Downloads\stellar_ws.txt&#39;</code></pre>`) {
		t.Fatalf("rendered tool = %q, want inner command in powershell code block", text)
	}
}

func TestRenderToolPanelMarksUnknownShell(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	text, _ := service.renderToolPanel(context.Background(), model.Thread{
		ID:          "thread-tool-unknown",
		Title:       "Unknown shell",
		ProjectName: "Codex",
	}, &appserver.ThreadReadSnapshot{
		LatestTurnID:    "turn-tool-unknown",
		LatestToolLabel: `hui.exe -Command "echo 1"`,
	})

	if !strings.Contains(text, "[Shell:hui.exe (⚠️UNKNOWN SHELL⚠️)]") {
		t.Fatalf("rendered tool = %q, want unknown shell marker", text)
	}
	if !strings.Contains(text, "<pre><code>echo 1</code></pre>") {
		t.Fatalf("rendered tool = %q, want command in generic code block", text)
	}
}

func TestRenderOutputPanelEscapesHTMLInsideCodeBlock(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	thread := model.Thread{ID: "thread-output-html", Title: "Output HTML", ProjectName: "Codex"}
	text, _ := service.renderOutputPanel(context.Background(), thread, &appserver.ThreadReadSnapshot{
		LatestTurnID:     "turn-output-html",
		LatestToolOutput: "<tag>value & more</tag>\n",
	})
	if !hasHeaderKind(text, "Output") || !strings.Contains(text, "\n<pre><code>") {
		t.Fatalf("rendered output = %q, want plain header before HTML code block", text)
	}
	if !hasThreadChip(text, thread.ID) {
		t.Fatalf("rendered output = %q, want thread identity chip", text)
	}
	if strings.Contains(text, "<tag>value") {
		t.Fatalf("rendered output = %q, want escaped html", text)
	}
	if !strings.Contains(text, "&lt;tag&gt;value &amp; more&lt;/tag&gt;") {
		t.Fatalf("rendered output = %q, want escaped payload", text)
	}
}

func TestRenderOutputPanelFitsAfterHTMLEscaping(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	thread := model.Thread{ID: "thread-output-fit", Title: "Output fit", ProjectName: "Codex"}
	text, _ := service.renderOutputPanel(context.Background(), thread, &appserver.ThreadReadSnapshot{
		LatestTurnID:     "turn-output-fit",
		LatestToolOutput: strings.Repeat("<tag>&", 2000),
	})
	if len(text) > outputMessageLimit {
		t.Fatalf("rendered output length = %d, want <= %d", len(text), outputMessageLimit)
	}
	if !hasHeaderKind(text, "Output") || !strings.Contains(text, "\n<pre><code>") {
		t.Fatalf("rendered output = %q, want plain header before HTML code block", text)
	}
	if !hasThreadChip(text, thread.ID) {
		t.Fatalf("rendered output = %q, want thread identity chip", text)
	}
}

func TestSyncThreadPanelToTargetSkipsRecentTerminalReplayFromBeforeObserveEnable(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-recent-before-enable",
		Title:       "Completed before observe",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Add(-30 * time.Second).Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.SetState(ctx, "observer.global_since_unix", strconv.FormatInt(time.Now().UTC().Unix(), 10)); err != nil {
		t.Fatalf("SetState(observer.global_since_unix) failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-before-enable",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-fp-before-enable",
		LatestFinalText:  "Should stay quiet.",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "global_observer")

	if len(sender.messages) != 0 {
		t.Fatalf("message count = %d, want 0 for completion from before /observe all; messages=%#v", len(sender.messages), sender.messages)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel != nil {
		t.Fatalf("expected no panel for completion from before /observe all, got %#v", panel)
	}
}

func TestLegacyTerminalPanelSeedsFinalFingerprintWithoutResending(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-legacy-terminal",
		Title:       "Legacy terminal",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-legacy",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-fp-legacy",
		LatestFinalText:  "Legacy final text.",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	panel, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           target.ChatID,
		TopicID:          target.TopicID,
		ProjectName:      thread.ProjectName,
		ThreadID:         thread.ID,
		SourceMode:       "global_observer",
		SummaryMessageID: 11,
		ToolMessageID:    12,
		OutputMessageID:  13,
		CurrentTurnID:    "turn-legacy",
		Status:           "completed",
		ArchiveEnabled:   true,
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "global_observer")

	if len(sender.messages) != 0 {
		t.Fatalf("message count = %d, want 0 for legacy terminal replay; messages=%#v", len(sender.messages), sender.messages)
	}
	refreshed, err := service.store.GetThreadPanelByID(ctx, panel.ID)
	if err != nil {
		t.Fatalf("GetThreadPanelByID failed: %v", err)
	}
	if refreshed == nil || refreshed.LastFinalNoticeFP != "final-fp-legacy" {
		t.Fatalf("LastFinalNoticeFP = %#v, want final-fp-legacy", refreshed)
	}
}

func TestNewTerminalTurnAfterExistingPanelStillSendsFinal(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-new-terminal-final",
		Title:       "Fresh terminal after history",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:            target.ChatID,
		TopicID:           target.TopicID,
		ProjectName:       thread.ProjectName,
		ThreadID:          thread.ID,
		SourceMode:        "global_observer",
		SummaryMessageID:  11,
		ToolMessageID:     12,
		OutputMessageID:   13,
		CurrentTurnID:     "turn-old",
		Status:            "completed",
		ArchiveEnabled:    true,
		LastFinalNoticeFP: "final-old",
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread: model.Thread{
			ID:          thread.ID,
			Title:       thread.Title,
			ProjectName: thread.ProjectName,
			CWD:         thread.CWD,
			UpdatedAt:   thread.UpdatedAt,
		},
		LatestTurnID:     "turn-new",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-new",
		LatestFinalText:  "NEW FINAL",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "global_observer")

	finalCount := 0
	for _, message := range sender.edits {
		if hasHeaderKind(message.text, "Final") {
			finalCount++
		}
	}
	if finalCount != 1 {
		t.Fatalf("final edit count = %d, want 1; edits=%#v", finalCount, sender.edits)
	}
}

func TestFinalCardDetailsCallbacksEditSameMessageAndExportToolsFile(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-details",
		Title:       "Details flow",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-details",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-details",
		LatestFinalText:  "Done.",
		DetailItems: []model.DetailItem{
			{ID: "c1", Kind: model.DetailItemCommentary, Text: "First.", CommentaryIndex: 1},
			{ID: "c2", Kind: model.DetailItemCommentary, Text: "Second with `code`.", CommentaryIndex: 2},
			{ID: "t2", Kind: model.DetailItemTool, Label: "pwsh -Command rg test", Status: "completed", CommentaryIndex: 2},
			{ID: "o2", Kind: model.DetailItemOutput, Output: "match\n", CommentaryIndex: 2},
			{ID: "c3", Kind: model.DetailItemCommentary, Text: "Third.", CommentaryIndex: 3},
			{ID: "c4", Kind: model.DetailItemCommentary, Text: "Fourth.", CommentaryIndex: 4},
			{ID: "c5", Kind: model.DetailItemCommentary, Text: "Fifth.", CommentaryIndex: 5},
			{ID: "f1", Kind: model.DetailItemFinal, Text: "Done.", CommentaryIndex: 5},
		},
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "explicit")
	if len(sender.edits) != 1 || !hasHeaderKind(sender.edits[0].text, "Final") {
		t.Fatalf("initial edits = %#v, want one final card edit", sender.edits)
	}
	cardMessageID := sender.edits[0].messageID
	detailsToken := buttonToken(sender.edits[0].buttons, "Details")
	if detailsToken == "" {
		t.Fatalf("final card buttons = %#v, want Details", sender.edits[0].buttons)
	}

	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, detailsToken); err != nil {
		t.Fatalf("HandleCallback(details) failed: %v", err)
	}
	if len(sender.edits) < 2 {
		t.Fatalf("edits = %#v, want details edit", sender.edits)
	}
	details := sender.edits[len(sender.edits)-1]
	if details.messageID != cardMessageID {
		t.Fatalf("details message id = %d, want same %d", details.messageID, cardMessageID)
	}
	if !strings.Contains(details.text, "[commentary 1]") || !strings.Contains(details.text, "[commentary 4]") || strings.Contains(details.text, "Fifth.") {
		t.Fatalf("details page text = %q, want first four commentary entries only", details.text)
	}

	nextToken := buttonToken(details.buttons, ">")
	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, nextToken); err != nil {
		t.Fatalf("HandleCallback(next) failed: %v", err)
	}
	next := sender.edits[len(sender.edits)-1]
	if !strings.Contains(next.text, "[commentary 5]") || !strings.Contains(next.text, "Fifth.") {
		t.Fatalf("next details text = %q, want fifth commentary", next.text)
	}

	toolOnToken := buttonToken(details.buttons, "Tool on")
	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, toolOnToken); err != nil {
		t.Fatalf("HandleCallback(tool on) failed: %v", err)
	}
	toolMode := sender.edits[len(sender.edits)-1]
	if !strings.Contains(toolMode.text, "[Tool]") || !strings.Contains(toolMode.text, "[Output]") {
		t.Fatalf("tool mode text = %q, want related tool/output", toolMode.text)
	}

	toolNextToken := buttonToken(toolMode.buttons, ">")
	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, toolNextToken); err != nil {
		t.Fatalf("HandleCallback(tool next) failed: %v", err)
	}
	toolNext := sender.edits[len(sender.edits)-1]
	if !strings.Contains(toolNext.text, "[commentary 3]") || strings.Contains(toolNext.text, "[commentary 4]") {
		t.Fatalf("tool next text = %q, want exactly next commentary without skipping", toolNext.text)
	}

	fileToken := buttonToken(toolMode.buttons, "Tools file")
	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, fileToken); err != nil {
		t.Fatalf("HandleCallback(tools file) failed: %v", err)
	}
	if len(sender.documents) != 1 {
		t.Fatalf("documents = %#v, want one tools file", sender.documents)
	}
	if sender.documents[0].filePath != "" {
		t.Fatalf("tools file used path %q, want in-memory document data", sender.documents[0].filePath)
	}
	if !strings.Contains(string(sender.documents[0].data), "[Details tools]") || !strings.Contains(string(sender.documents[0].data), "[Tool]") {
		t.Fatalf("tools file data = %q, want details tool content", string(sender.documents[0].data))
	}

	backToken := buttonToken(toolMode.buttons, "Back")
	if _, err := service.HandleCallback(ctx, target.ChatID, target.TopicID, cardMessageID, 123456789, backToken); err != nil {
		t.Fatalf("HandleCallback(back) failed: %v", err)
	}
	back := sender.edits[len(sender.edits)-1]
	if !hasHeaderKind(back.text, "Final") {
		t.Fatalf("back text = %q, want final card", back.text)
	}
}

func TestDetailsCallbacksUsePanelTurnInsteadOfLatestThreadTurn(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	threadPayload := map[string]any{
		"id":     "thread-details-history",
		"name":   "Details history",
		"cwd":    `C:\Users\you\Projects\Codex`,
		"status": "completed",
		"turns": []any{
			map[string]any{
				"id":     "turn-old",
				"status": "completed",
				"items": []any{
					map[string]any{"id": "old-c1", "type": "agentMessage", "phase": "commentary", "text": "Old commentary."},
					map[string]any{"id": "old-f1", "type": "agentMessage", "phase": "final_answer", "text": "Old final."},
				},
			},
			map[string]any{
				"id":     "turn-new",
				"status": "completed",
				"items": []any{
					map[string]any{"id": "new-c1", "type": "agentMessage", "phase": "commentary", "text": "New commentary must not leak."},
					map[string]any{"id": "new-f1", "type": "agentMessage", "phase": "final_answer", "text": "New final must not leak."},
				},
			},
		},
	}
	latest := appserver.SnapshotFromThreadRead(threadPayload)
	if err := service.store.UpsertThread(ctx, latest.Thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.UpsertSnapshot(ctx, latest.Thread.ID, appserver.CompactSnapshot(nil, latest, time.Now().UTC())); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	panel, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:            123456789,
		TopicID:           0,
		ProjectName:       latest.Thread.ProjectName,
		ThreadID:          latest.Thread.ID,
		SourceMode:        model.PanelSourceExplicit,
		SummaryMessageID:  101,
		ToolMessageID:     102,
		OutputMessageID:   103,
		CurrentTurnID:     "turn-old",
		Status:            "completed",
		ArchiveEnabled:    true,
		LastFinalNoticeFP: "old-final-fp",
		LastFinalCardHash: "old-card-hash",
		DetailsViewJSON:   model.MustJSON(model.DetailsViewState{}),
		LastSummaryHash:   "old-card-hash",
		LastToolHash:      "old-tool",
		LastOutputHash:    "old-output",
	})
	if err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}
	token := service.callbackButton(ctx, "Details", "details_open", latest.Thread.ID, "turn-old", "", map[string]any{"panel_id": panel.ID, "page": 0}).CallbackData

	if _, err := service.HandleCallback(ctx, 123456789, 0, 101, 123456789, token); err != nil {
		t.Fatalf("HandleCallback(details) failed: %v", err)
	}
	if len(sender.edits) != 1 {
		t.Fatalf("edits = %#v, want one details edit", sender.edits)
	}
	text := sender.edits[0].text
	if !strings.Contains(text, "Old commentary.") || strings.Contains(text, "New commentary must not leak.") {
		t.Fatalf("details text = %q, want panel turn details only", text)
	}
}

func TestTerminalSyncAdoptsActivePanelWithoutTurnID(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-adopt-turn",
		Title:       "Adopt turn",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}
	if _, err := service.store.CreateThreadPanel(ctx, model.ThreadPanel{
		ChatID:           target.ChatID,
		TopicID:          target.TopicID,
		ProjectName:      thread.ProjectName,
		ThreadID:         thread.ID,
		SourceMode:       "explicit",
		SummaryMessageID: 101,
		ToolMessageID:    102,
		OutputMessageID:  103,
		CurrentTurnID:    "",
		Status:           "inProgress",
		ArchiveEnabled:   true,
	}); err != nil {
		t.Fatalf("CreateThreadPanel failed: %v", err)
	}

	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-adopted",
		LatestTurnStatus: "completed",
		LatestFinalFP:    "final-adopted",
		LatestFinalText:  "ADOPTED_OK",
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, "global_observer")

	if len(sender.messages) != 0 {
		t.Fatalf("messages = %#v, want no new trio when active panel adopts turn", sender.messages)
	}
	if len(sender.edits) == 0 || sender.edits[len(sender.edits)-1].messageID != 101 || !hasHeaderKind(sender.edits[len(sender.edits)-1].text, "Final") {
		t.Fatalf("edits = %#v, want final edit on existing summary message 101", sender.edits)
	}
	if len(sender.deletes) != 2 || sender.deletes[0].messageID != 102 || sender.deletes[1].messageID != 103 {
		t.Fatalf("deletes = %#v, want existing tool/output delete", sender.deletes)
	}
}

func TestSyncThreadPanelCreatesRouteablePlanPromptAndDedupes(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-plan-card",
		Title:       "Plan card",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:              thread,
		LatestTurnID:        "turn-plan-card",
		LatestTurnStatus:    "active[waitingOnUserInput]",
		WaitingOnReply:      true,
		LatestProgressText:  "Need input.",
		LatestProgressFP:    "plan-progress-fp",
		LatestAgentMessages: []string{"Need input."},
		PlanPrompt: &model.PlanPrompt{
			PromptID:    "synthetic:thread-plan-card:turn-plan-card:abc",
			Source:      model.PromptSourceSyntheticPoll,
			ThreadID:    thread.ID,
			TurnID:      "turn-plan-card",
			ItemID:      "plan-item-1",
			Question:    "Choose next step?",
			Options:     []string{"Continue", "Revise"},
			Fingerprint: "plan-fp-1",
			Status:      "waiting for input",
		},
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)
	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 6 {
		t.Fatalf("message count = %d, want New run + [User placeholder] + [Plan] + trio once: %#v", len(sender.messages), sender.messages)
	}
	if !strings.Contains(sender.messages[0].text, "New run:") {
		t.Fatalf("first message = %q, want New run before [User]/[Plan]", sender.messages[0].text)
	}
	if !hasHeaderKind(sender.messages[1].text, "User") || !strings.Contains(sender.messages[1].text, "User prompt was not available") {
		t.Fatalf("second message = %q, want [User] placeholder before [Plan]", sender.messages[1].text)
	}
	if !hasHeaderKind(sender.messages[2].text, "Plan") {
		t.Fatalf("third message = %q, want [Plan] prompt before trio", sender.messages[2].text)
	}
	if !strings.Contains(sender.messages[2].text, "Choose next step?") {
		t.Fatalf("plan prompt text = %q, want question", sender.messages[2].text)
	}
	if got := buttonToken(sender.messages[2].buttons, "Continue"); got == "" {
		t.Fatalf("plan prompt buttons = %#v, want structured Continue button", sender.messages[2].buttons)
	}
	panel, err := service.store.GetCurrentThreadPanel(ctx, target.ChatID, target.TopicID, thread.ID)
	if err != nil {
		t.Fatalf("GetCurrentThreadPanel failed: %v", err)
	}
	if panel == nil || panel.UserMessageID != sender.messages[1].messageID || panel.PlanPromptMessageID != sender.messages[2].messageID || panel.LastPlanPromptFP != "plan-fp-1" {
		t.Fatalf("panel plan prompt state = %#v, want user id %d and plan id %d / plan-fp-1", panel, sender.messages[1].messageID, sender.messages[2].messageID)
	}
	route, err := service.store.ResolveMessageRoute(ctx, target.ChatID, target.TopicID, sender.messages[2].messageID)
	if err != nil {
		t.Fatalf("ResolveMessageRoute failed: %v", err)
	}
	if route == nil || route.ThreadID != thread.ID || route.TurnID != "turn-plan-card" || route.ItemID != "plan-item-1" || route.EventID != "plan-fp-1" {
		t.Fatalf("plan route = %#v, want thread/turn/item/fp route", route)
	}
}

func TestSyncThreadPanelCreatesServerRequestPlanPromptRoute(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	sender := &recordingSender{}
	service.SetSender(sender)
	ctx := context.Background()

	thread := model.Thread{
		ID:          "thread-plan-request",
		Title:       "Plan request",
		ProjectName: "Codex",
		CWD:         `C:\Users\you\Projects\Codex`,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	if err := service.store.UpsertThread(ctx, thread); err != nil {
		t.Fatalf("UpsertThread failed: %v", err)
	}
	if err := service.store.SavePendingApproval(ctx, model.PendingApproval{
		RequestID:   "request-plan-1",
		ThreadID:    thread.ID,
		TurnID:      "turn-plan-request",
		ItemID:      "request-item-1",
		PromptKind:  "user_input",
		Question:    "Pick deployment target?",
		PayloadJSON: `{"questions":[{"id":"target","header":"Target","question":"Pick deployment target?","options":[{"label":"staging","description":"Use staging."},{"label":"production","description":"Use production."}]}]}`,
		Status:      "pending",
		UpdatedAt:   model.NowString(),
	}); err != nil {
		t.Fatalf("SavePendingApproval failed: %v", err)
	}
	snapshot := appserver.CompactSnapshot(nil, appserver.ThreadReadSnapshot{
		Thread:           thread,
		LatestTurnID:     "turn-plan-request",
		LatestTurnStatus: "active[waitingOnUserInput]",
		WaitingOnReply:   true,
	}, time.Now().UTC())
	if err := service.store.UpsertSnapshot(ctx, thread.ID, snapshot); err != nil {
		t.Fatalf("UpsertSnapshot failed: %v", err)
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(123456789, 0), ChatID: 123456789, TopicID: 0, Enabled: true}

	service.syncThreadPanelToTarget(ctx, target, thread.ID, false, model.PanelSourceGlobalObserver)

	if len(sender.messages) != 6 {
		t.Fatalf("message count = %d, want New run + [User placeholder] + [Plan] + trio: %#v", len(sender.messages), sender.messages)
	}
	if got := buttonToken(sender.messages[2].buttons, "staging"); got == "" {
		t.Fatalf("plan prompt buttons = %#v, want staging button", sender.messages[2].buttons)
	}
	route, err := service.store.ResolveMessageRoute(ctx, target.ChatID, target.TopicID, sender.messages[2].messageID)
	if err != nil {
		t.Fatalf("ResolveMessageRoute failed: %v", err)
	}
	if route == nil || route.EventID != "plan_request:request-plan-1" {
		t.Fatalf("plan request route = %#v, want plan_request event id", route)
	}
}

func buttonToken(rows [][]model.ButtonSpec, text string) string {
	for _, row := range rows {
		for _, button := range row {
			if button.Text == text {
				return button.CallbackData
			}
		}
	}
	return ""
}
