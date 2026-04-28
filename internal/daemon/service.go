package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"codex-telegram-remote-go/internal/appserver"
	"codex-telegram-remote-go/internal/config"
	"codex-telegram-remote-go/internal/model"
	"codex-telegram-remote-go/internal/storage"
)

type Session interface {
	Start(ctx context.Context) error
	Close() error
	Subscribe() <-chan appserver.Event
	ThreadList(ctx context.Context, limit int, cursor string) (map[string]any, error)
	ThreadRead(ctx context.Context, threadID string, includeTurns bool) (map[string]any, error)
	ThreadResume(ctx context.Context, threadID, cwd string) (map[string]any, error)
	ThreadStart(ctx context.Context, cwd string) (map[string]any, error)
	TurnStart(ctx context.Context, threadID, message, cwd string) (map[string]any, error)
	TurnSteer(ctx context.Context, threadID, turnID, message string) (map[string]any, error)
	TurnInterrupt(ctx context.Context, threadID, turnID string) error
	RespondServerRequest(ctx context.Context, requestID string, result map[string]any) error
	StderrTail() []string
}

type Sender interface {
	SendMessage(ctx context.Context, chatID, topicID int64, text string, buttons [][]model.ButtonSpec) (int64, error)
	SendRenderedMessages(ctx context.Context, chatID, topicID int64, messages []model.RenderedMessage, buttons [][]model.ButtonSpec) ([]int64, error)
	EditMessage(ctx context.Context, chatID, topicID, messageID int64, text string, buttons [][]model.ButtonSpec) error
	EditRenderedMessage(ctx context.Context, chatID, topicID, messageID int64, rendered model.RenderedMessage, buttons [][]model.ButtonSpec) error
	DeleteMessage(ctx context.Context, chatID, topicID, messageID int64) error
	SendDocument(ctx context.Context, chatID, topicID int64, fileName, filePath, caption string) (int64, error)
}

type DirectResponse struct {
	Text         string
	CallbackText string
	Buttons      [][]model.ButtonSpec
	ThreadID     string
	TurnID       string
	ItemID       string
	EventID      string
}

type Service struct {
	cfg   config.Config
	store *storage.Store

	liveFactory func() Session
	pollFactory func() Session

	mu            sync.RWMutex
	live          Session
	poll          Session
	liveEvents    <-chan appserver.Event
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	panelMu       sync.Mutex
	sender        Sender
	started       bool
	startedAt     time.Time
	ready         bool
	phase         string
	lastError     string
	liveConnected bool
	pollConnected bool
}

const observerRecentThreadLimit = 50

func New(cfg config.Config) (*Service, error) {
	if err := cfg.Paths.Ensure(); err != nil {
		return nil, err
	}
	store, err := storage.Open(cfg.Paths.DBPath)
	if err != nil {
		return nil, err
	}
	service := &Service{
		cfg:   cfg,
		store: store,
		phase: "created",
	}
	service.liveFactory = func() Session {
		return appserver.NewClient(cfg.CodexBin, cfg.AppServerListen, cfg.DefaultCWD, cfg.RequestTimeout)
	}
	service.pollFactory = func() Session {
		return appserver.NewClient(cfg.CodexBin, cfg.AppServerListen, cfg.DefaultCWD, cfg.RequestTimeout)
	}
	service.live = service.liveFactory()
	service.poll = service.pollFactory()
	return service, nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	cancel := s.cancel
	started := s.started
	live := s.live
	poll := s.poll
	s.started = false
	s.cancel = nil
	s.mu.Unlock()
	if started && cancel != nil {
		cancel()
	}
	s.wg.Wait()
	if live != nil {
		_ = live.Close()
	}
	if poll != nil {
		_ = poll.Close()
	}
	return s.store.Close()
}

func (s *Service) SetSender(sender Sender) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sender = sender
}

func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.started = true
	s.startedAt = time.Now().UTC()
	s.ready = true
	s.phase = "ready"
	s.lastError = ""
	s.liveConnected = false
	s.pollConnected = false
	s.mu.Unlock()

	_ = s.store.SetState(runCtx, "daemon.phase", "ready")
	_ = s.store.SetState(runCtx, "daemon.ready", "true")
	_ = s.store.SetState(runCtx, "daemon.started_at", s.startedAt.Format(time.RFC3339Nano))
	_ = s.store.SetState(runCtx, "daemon.last_error", "")
	s.cleanupTempArtifacts(runCtx)

	s.spawn(runCtx, s.ensureSessions)
	s.spawn(runCtx, s.indexLoop)
	s.spawn(runCtx, s.attachLoop)
	s.spawn(runCtx, s.pollLoop)
	s.spawn(runCtx, s.deliveryLoop)
	s.spawn(runCtx, s.controlLoop)
	return nil
}

func (s *Service) Doctor(ctx context.Context) (map[string]any, error) {
	backlog, _ := s.store.DeliveryQueueBacklog(ctx)
	state, _ := s.store.ListState(ctx)
	return map[string]any{
		"config":           s.cfg,
		"delivery_backlog": backlog,
		"daemon_state":     state,
	}, nil
}

func (s *Service) StatusSnapshot(ctx context.Context, chatID, topicID int64) (string, error) {
	contextState, err := s.store.GetChatContext(ctx, chatID, topicID)
	if err != nil {
		return "", err
	}
	threadCount, _ := s.store.CountThreads(ctx)
	backlog, _ := s.store.DeliveryQueueBacklog(ctx)
	boundIDs, _ := s.store.ListBoundThreadIDs(ctx)
	globalObserver, configured, _ := s.store.GetGlobalObserverTarget(ctx)
	panelMode := s.panelMode(ctx)
	s.mu.RLock()
	ready := s.ready
	phase := s.phase
	liveConnected := s.liveConnected
	pollConnected := s.pollConnected
	startedAt := s.startedAt
	lastError := s.lastError
	s.mu.RUnlock()
	lines := []string{
		"Go core status",
		fmt.Sprintf("Ready: %t", ready),
		fmt.Sprintf("Phase: %s", phase),
		fmt.Sprintf("Live app-server: %t", liveConnected),
		fmt.Sprintf("Poll app-server: %t", pollConnected),
		fmt.Sprintf("Panel mode: %s", panelMode),
		fmt.Sprintf("Tracked threads: %d", len(boundIDs)+threadCount),
		fmt.Sprintf("Cached threads: %d", threadCount),
		fmt.Sprintf("Delivery backlog: %d", backlog),
	}
	switch {
	case configured && globalObserver != nil && globalObserver.Enabled:
		lines = append(lines, fmt.Sprintf("Global observer: on -> %s", model.ChatKey(globalObserver.ChatID, globalObserver.TopicID)))
	case configured:
		lines = append(lines, "Global observer: off")
	default:
		lines = append(lines, "Global observer: default-on fallback")
	}
	if !startedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("Started: %s", startedAt.Format(time.RFC3339)))
	}
	if strings.TrimSpace(lastError) != "" {
		lines = append(lines, fmt.Sprintf("Last error: %s", lastError))
	}
	lines = append(lines, "")
	switch {
	case contextState.Binding != nil && contextState.Thread != nil && contextState.ObserverEnabled:
		lines = append(lines, "Mode: Bound thread + global observer sink", fmt.Sprintf("Thread: %s", contextState.Thread.Label()))
	case contextState.ObserverEnabled:
		lines = append(lines, "Mode: Global observer sink")
	case contextState.Binding != nil && contextState.Thread != nil:
		lines = append(lines, "Mode: Bound thread", fmt.Sprintf("Thread: %s", contextState.Thread.Label()))
	case contextState.Binding != nil:
		lines = append(lines, "Mode: Bound thread", fmt.Sprintf("Thread ID: %s", contextState.Binding.ThreadID))
	default:
		lines = append(lines, "Mode: Unbound", "Use /threads, /projects, or /bind <thread>.")
	}
	if contextState.Thread != nil {
		if _, snapshot, err := s.loadThreadPanelSnapshot(ctx, contextState.Thread.ID); err == nil && snapshot != nil {
			if prompt := effectivePlanPrompt(nil, snapshot); prompt != nil {
				lines = append(lines, fmt.Sprintf("Active Plan prompt: %s", trimPreview(prompt.Question)))
			}
		}
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Service) HandleMessage(ctx context.Context, chatID, topicID, userID int64, text string, replyToMessageID int64) (*DirectResponse, error) {
	if !s.IsAllowed(userID, chatID) {
		return nil, nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return &DirectResponse{Text: "Plain text messages only right now. Send text, or use /context for routing help."}, nil
	}
	if strings.HasPrefix(text, "/") {
		return s.handleCommand(ctx, chatID, topicID, text, replyToMessageID)
	}
	return s.handlePlainText(ctx, chatID, topicID, text, replyToMessageID)
}

func (s *Service) HandleCallback(ctx context.Context, chatID, topicID, messageID, userID int64, token string) (*DirectResponse, error) {
	if !s.IsAllowed(userID, chatID) {
		return nil, nil
	}
	route, err := s.store.GetCallbackRoute(ctx, token)
	if err != nil {
		return nil, err
	}
	if route == nil || route.Status != model.CallbackStatusActive {
		return &DirectResponse{Text: "This button is stale. Use /show <thread> or /repair."}, nil
	}
	var payload map[string]any
	if route.PayloadJSON != "" {
		_ = json.Unmarshal([]byte(route.PayloadJSON), &payload)
	}
	switch route.Action {
	case "details_open", "details_prev", "details_next", "details_back", "details_tool_toggle":
		return s.handleDetailsCallback(ctx, chatID, topicID, messageID, route, payload)
	case "details_tools_file":
		return s.sendDetailsToolsFile(ctx, chatID, topicID, route, payload)
	case "show_thread":
		return s.showThread(ctx, chatID, topicID, route.ThreadID, true)
	case "show_context":
		text, err := s.StatusSnapshot(ctx, chatID, topicID)
		if err != nil {
			return nil, err
		}
		return &DirectResponse{Text: text}, nil
	case "bind_here":
		if err := s.store.SetBinding(ctx, chatID, topicID, route.ThreadID, model.BindingModeBound); err != nil {
			return nil, err
		}
		s.kickBootstrap()
		return &DirectResponse{CallbackText: fmt.Sprintf("Bound this chat to %s.", route.ThreadID)}, nil
	case "observe_all":
		if err := s.store.SetGlobalObserverTarget(ctx, chatID, topicID, true); err != nil {
			return nil, err
		}
		s.kickBootstrap()
		return &DirectResponse{CallbackText: "Global observer moved here."}, nil
	case "reply_hint":
		return &DirectResponse{Text: fmt.Sprintf("Reply to this thread with:\n/reply %s <text>", route.ThreadID)}, nil
	case "stop_turn":
		return s.interruptTurn(ctx, chatID, topicID, route.ThreadID, route.TurnID)
	case "arm_steer":
		panel, _ := s.store.GetCurrentThreadPanel(ctx, chatID, topicID, route.ThreadID)
		panelID := int64(0)
		if panel != nil {
			panelID = panel.ID
		}
		if err := s.armSteer(ctx, chatID, topicID, route.ThreadID, route.TurnID, panelID); err != nil {
			return nil, err
		}
		return &DirectResponse{CallbackText: "Следующее сообщение пойдёт в этот thread."}, nil
	case "approve", "approve_session":
		decision := "accept"
		if route.Action == "approve_session" {
			decision = "acceptForSession"
		}
		return s.approve(ctx, route.RequestID, decision)
	case "deny", "cancel":
		decision := "decline"
		if route.Action == "cancel" {
			decision = "cancel"
		}
		return s.approve(ctx, route.RequestID, decision)
	case "answer_choice":
		return s.answerChoice(ctx, chatID, topicID, route)
	case "get_full_log":
		return s.sendFullLogArchive(ctx, chatID, topicID, route.ThreadID)
	default:
		return &DirectResponse{Text: "This button is not implemented in the Go core yet."}, nil
	}
}

func (s *Service) RegisterDirectDelivery(ctx context.Context, chatID, topicID, messageID int64, response *DirectResponse) error {
	if response == nil || response.ThreadID == "" {
		return nil
	}
	return s.store.PutMessageRoute(ctx, model.MessageRoute{
		ChatID:    chatID,
		TopicID:   topicID,
		MessageID: messageID,
		ThreadID:  response.ThreadID,
		TurnID:    response.TurnID,
		ItemID:    response.ItemID,
		EventID:   response.EventID,
		CreatedAt: model.NowString(),
	})
}

func (s *Service) RequestRepair(ctx context.Context, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "manual"
	}
	return s.store.SetState(ctx, "control.repair_request", fmt.Sprintf("%s|%s", time.Now().UTC().Format(time.RFC3339Nano), reason))
}

func (s *Service) IsAllowed(userID, chatID int64) bool {
	if len(s.cfg.AllowedUserIDs) > 0 && !containsInt64(s.cfg.AllowedUserIDs, userID) {
		return false
	}
	if len(s.cfg.AllowedChatIDs) > 0 && !containsInt64(s.cfg.AllowedChatIDs, chatID) {
		return false
	}
	return true
}

func (s *Service) spawn(ctx context.Context, fn func(context.Context)) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		fn(ctx)
	}()
}

func (s *Service) ensureSessions(ctx context.Context) {
	s.ensureLiveSession(ctx)
	s.ensurePollSession(ctx)
	s.bootstrapTrackedState(ctx)
}

func (s *Service) ensureLiveSession(ctx context.Context) {
	s.mu.RLock()
	client := s.live
	connected := s.liveConnected
	s.mu.RUnlock()
	if client == nil || connected {
		return
	}
	sessionCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	if err := client.Start(sessionCtx); err != nil {
		s.setError(ctx, err)
		return
	}
	s.mu.Lock()
	s.liveConnected = true
	s.liveEvents = client.Subscribe()
	s.mu.Unlock()
	_ = s.store.SetState(ctx, "appserver.live_connected", "true")
	s.spawn(ctx, s.liveEventLoop)
}

func (s *Service) ensurePollSession(ctx context.Context) {
	s.mu.RLock()
	client := s.poll
	connected := s.pollConnected
	s.mu.RUnlock()
	if client == nil || connected {
		return
	}
	sessionCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	if err := client.Start(sessionCtx); err != nil {
		s.setError(ctx, err)
		return
	}
	s.mu.Lock()
	s.pollConnected = true
	s.mu.Unlock()
	_ = s.store.SetState(ctx, "appserver.poll_connected", "true")
}

func (s *Service) liveEventLoop(ctx context.Context) {
	s.mu.RLock()
	ch := s.liveEvents
	live := s.live
	s.mu.RUnlock()
	if ch == nil || live == nil {
		return
	}
	defer func() {
		s.mu.Lock()
		s.liveConnected = false
		s.liveEvents = nil
		s.mu.Unlock()
		_ = s.store.SetState(context.Background(), "appserver.live_connected", "false")
		if ctx.Err() == nil {
			_ = s.RequestRepair(context.Background(), "live_event_loop_closed")
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			s.handleLiveEvent(ctx, live, event)
		}
	}
}

func (s *Service) handleLiveEvent(ctx context.Context, live Session, event appserver.Event) {
	if event.Channel == "transport_error" {
		s.setError(ctx, fmt.Errorf("app-server transport error: %v", event.Params))
		return
	}
	threadID := threadIDFromEvent(event)
	if threadID != "" {
		_ = s.store.MarkLiveEvent(ctx, threadID, model.NowString())
	}
	if approval, ok := appserver.PendingApprovalFromServerRequest(event); ok {
		_ = s.store.SavePendingApproval(ctx, *approval)
		if approval.ThreadID != "" {
			if refreshed, err := s.refreshThread(ctx, live, approval.ThreadID); err == nil && refreshed != nil {
				_ = refreshed
			}
			s.syncThreadPanel(ctx, approval.ThreadID)
		}
		return
	}
	if strings.EqualFold(event.Method, "serverRequest/resolved") {
		if requestID := fmt.Sprintf("%v", event.Params["requestId"]); requestID != "" {
			_ = s.store.UpdatePendingApprovalStatus(ctx, requestID, "resolved")
			if pending, err := s.store.GetPendingApproval(ctx, requestID); err == nil && pending != nil && pending.ThreadID != "" {
				s.syncThreadPanel(ctx, pending.ThreadID)
			}
		}
		return
	}
	if threadID != "" {
		previous, _ := s.store.GetSnapshot(ctx, threadID)
		interactiveSync := s.threadNeedsLiveSync(ctx, threadID)
		if _, err := s.refreshThread(ctx, live, threadID); err != nil {
			s.noteSessionError(ctx, "live_refresh", err)
			return
		}
		if !interactiveSync {
			_, current, err := s.loadThreadPanelSnapshot(ctx, threadID)
			if err != nil || current == nil || !snapshotHasPassiveChange(previous, current) {
				return
			}
		}
		s.syncThreadPanel(ctx, threadID)
	}
}

func (s *Service) indexLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.IndexRefreshInterval)
	defer ticker.Stop()
	for {
		s.syncThreads(ctx, 200)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) attachLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.AttachRefreshInterval)
	defer ticker.Stop()
	for {
		s.attachTracked(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.ObserverPollInterval)
	defer ticker.Stop()
	for {
		s.refreshObserverIndex(ctx)
		s.pollTracked(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) refreshObserverIndex(ctx context.Context) {
	if !s.hasBackgroundTargets(ctx) {
		return
	}
	s.syncThreads(ctx, observerRecentThreadLimit)
}

func (s *Service) deliveryLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		s.processDeliveryBatch(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) controlLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		s.reconcileSessions(ctx)
		value, _ := s.store.GetState(ctx, "control.repair_request")
		if strings.TrimSpace(value) != "" {
			s.repairSessions(ctx)
			_ = s.store.SetState(ctx, "control.repair_request", "")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) reconcileSessions(ctx context.Context) {
	s.mu.RLock()
	liveConnected := s.liveConnected
	pollConnected := s.pollConnected
	s.mu.RUnlock()
	if !liveConnected {
		s.ensureLiveSession(ctx)
	}
	if !pollConnected {
		s.ensurePollSession(ctx)
	}
}

func (s *Service) repairSessions(ctx context.Context) {
	s.mu.Lock()
	oldLive := s.live
	oldPoll := s.poll
	s.liveConnected = false
	s.pollConnected = false
	s.live = s.liveFactory()
	s.poll = s.pollFactory()
	s.liveEvents = nil
	s.lastError = ""
	s.mu.Unlock()
	if oldLive != nil {
		_ = oldLive.Close()
	}
	if oldPoll != nil {
		_ = oldPoll.Close()
	}
	rechecked, _ := s.store.MarkAllPendingApprovals(ctx, "needs_recheck")
	_ = s.store.SetState(ctx, "repair.last_rechecked", strconv.FormatInt(rechecked, 10))
	_ = s.store.SetState(ctx, "appserver.live_connected", "false")
	_ = s.store.SetState(ctx, "appserver.poll_connected", "false")
	s.ensureSessions(ctx)
}

func (s *Service) bootstrapTrackedState(ctx context.Context) {
	s.syncThreads(ctx, 200)
	s.attachTracked(ctx)
	s.pollTracked(ctx)
}

func (s *Service) syncThreads(ctx context.Context, limit int) {
	s.mu.RLock()
	live := s.live
	poll := s.poll
	liveConnected := s.liveConnected
	pollConnected := s.pollConnected
	s.mu.RUnlock()
	var client Session
	if liveConnected {
		client = live
	} else if pollConnected {
		client = poll
	}
	if client == nil {
		return
	}
	if limit <= 0 {
		limit = 100
	}
	cursor := ""
	remaining := limit
	pageSize := 25
	for remaining > 0 {
		requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		result, err := client.ThreadList(requestCtx, min(pageSize, remaining), cursor)
		cancel()
		if err != nil {
			s.noteSessionError(ctx, "thread_list", err)
			return
		}
		threads := appserver.ThreadsFromList(result)
		if len(threads) == 0 {
			return
		}
		for _, thread := range threads {
			_ = s.store.UpsertThread(ctx, thread)
		}
		remaining -= len(threads)
		nextCursor, _ := result["nextCursor"].(string)
		if strings.TrimSpace(nextCursor) == "" {
			return
		}
		cursor = nextCursor
	}
}

func (s *Service) attachTracked(ctx context.Context) {
	s.mu.RLock()
	live := s.live
	connected := s.liveConnected
	s.mu.RUnlock()
	if !connected || live == nil {
		return
	}
	seen := map[string]struct{}{}
	for _, threadID := range append(s.boundThreadIDs(ctx), s.currentPanelThreadIDs(ctx)...) {
		if _, ok := seen[threadID]; ok {
			continue
		}
		seen[threadID] = struct{}{}
		thread, err := s.store.GetThread(ctx, threadID)
		if err != nil || thread == nil {
			continue
		}
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err = live.ThreadResume(requestCtx, thread.ID, thread.CWD)
		cancel()
		if err != nil {
			s.setError(ctx, fmt.Errorf("thread_resume(bound): %w", err))
		}
	}
}

func (s *Service) pollTracked(ctx context.Context) {
	s.mu.RLock()
	poll := s.poll
	connected := s.pollConnected
	s.mu.RUnlock()
	if !connected || poll == nil {
		return
	}
	threads := s.trackedThreads(ctx, observerRecentThreadLimit)
	if len(threads) == 0 {
		return
	}
	for _, thread := range threads {
		snapshot, _ := s.store.GetSnapshot(ctx, thread.ID)
		catchup := s.threadNeedsCatchupPolling(ctx, thread, snapshot)
		if snapshot != nil && snapshot.LastRichLiveEventAt != "" {
			if time.Since(parseTime(snapshot.LastRichLiveEventAt)) < maxDuration(10*time.Second, s.cfg.ObserverPollInterval*2) {
				continue
			}
		}
		requestCtx, cancel := context.WithTimeout(ctx, maxDuration(10*time.Second, s.cfg.ObserverPollInterval*2))
		payload, err := poll.ThreadRead(requestCtx, thread.ID, true)
		cancel()
		if err != nil {
			requestCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
			payload, err = poll.ThreadRead(requestCtx, thread.ID, false)
			cancel()
		}
		if err != nil {
			s.noteSessionError(ctx, "thread_read", err)
			continue
		}
		current := appserver.SnapshotFromThreadRead(payload)
		current.Thread.Raw, _ = json.Marshal(payload)
		current.Thread = mergeThreadMetadata(current.Thread, thread)
		_ = applySessionTailToolOverlay(current.Thread, &current)
		_ = s.store.UpsertThread(ctx, current.Thread)
		nextSnapshot := appserver.CompactSnapshot(snapshot, current, time.Now().UTC())
		if current.LatestTurnStatus == "inProgress" || current.SessionTailActiveTool || current.WaitingOnApproval || current.WaitingOnReply {
			nextSnapshot.NextPollAfter = model.TimeString(time.Now().UTC().Add(s.cfg.ObserverPollInterval).Format(time.RFC3339Nano))
		} else {
			nextSnapshot.NextPollAfter = model.TimeString(time.Now().UTC().Add(30 * time.Second).Format(time.RFC3339Nano))
		}
		_ = s.store.UpsertSnapshot(ctx, current.Thread.ID, nextSnapshot)
		if catchup || s.threadNeedsLiveSync(ctx, current.Thread.ID) || snapshotHasPassiveChange(snapshot, &current) {
			s.syncThreadPanel(ctx, current.Thread.ID)
		}
	}
}

func (s *Service) processDeliveryBatch(ctx context.Context) {
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender == nil {
		return
	}
	items, err := s.store.ClaimDeliveryBatch(ctx, 10)
	if err != nil || len(items) == 0 {
		return
	}
	for _, item := range items {
		var payload model.DeliveryPayload
		if err := json.Unmarshal([]byte(item.PayloadJSON), &payload); err != nil {
			_ = s.store.RecordDeliveryAttempt(ctx, item.ID, item.RetryCount+1, "decode_error", err.Error())
			_ = s.store.FailDelivery(ctx, item.ID, item.RetryCount+1, time.Now().UTC().Add(s.cfg.DeliveryRetryBase), err.Error(), true)
			continue
		}
		messageID, err := sender.SendMessage(ctx, item.ChatID, item.TopicID, payload.Text, payload.Buttons)
		if err != nil {
			attempt := item.RetryCount + 1
			_ = s.store.RecordDeliveryAttempt(ctx, item.ID, attempt, "send_error", err.Error())
			dead := attempt >= s.cfg.DeliveryMaxAttempts
			backoff := s.cfg.DeliveryRetryBase * time.Duration(1<<min(attempt-1, 4))
			_ = s.store.FailDelivery(ctx, item.ID, attempt, time.Now().UTC().Add(backoff), err.Error(), dead)
			s.setError(ctx, err)
			continue
		}
		_ = s.store.RecordDeliveryAttempt(ctx, item.ID, item.RetryCount+1, "delivered", "")
		_ = s.store.CompleteDelivery(ctx, item.ID)
		if payload.ThreadID != "" {
			_ = s.store.PutMessageRoute(ctx, model.MessageRoute{
				ChatID:    item.ChatID,
				TopicID:   item.TopicID,
				MessageID: messageID,
				ThreadID:  payload.ThreadID,
				TurnID:    payload.TurnID,
				ItemID:    payload.ItemID,
				EventID:   payload.EventID,
				CreatedAt: model.NowString(),
			})
		}
	}
}

func (s *Service) handleCommand(ctx context.Context, chatID, topicID int64, raw string, replyToMessageID int64) (*DirectResponse, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), " ", 2)
	command := strings.ToLower(strings.SplitN(parts[0], "@", 2)[0])
	rest := ""
	if len(parts) > 1 {
		rest = strings.TrimSpace(parts[1])
	}
	switch command {
	case "/start":
		return &DirectResponse{Text: "ctr-go is online.\nUse /status, /threads, /projects, /context, or /observe all."}, nil
	case "/help":
		return &DirectResponse{Text: "Commands:\n/start\n/help\n/threads [limit|search]\n/projects\n/show <thread>\n/bind <thread>\n/reply <thread> <text>\n/context\n/observe all|off\n/panelmode [per_run|stable]\n/status\n/repair\n/stop [thread]\n/approve <request_id>\n/deny <request_id>"}, nil
	case "/status":
		text, err := s.StatusSnapshot(ctx, chatID, topicID)
		if err != nil {
			return nil, err
		}
		return &DirectResponse{Text: text}, nil
	case "/context", "/whereami":
		text, err := s.contextCard(ctx, chatID, topicID)
		if err != nil {
			return nil, err
		}
		return &DirectResponse{Text: text}, nil
	case "/observe":
		switch strings.ToLower(rest) {
		case "all", "on":
			if err := s.store.SetGlobalObserverTarget(ctx, chatID, topicID, true); err != nil {
				return nil, err
			}
			s.kickBootstrap()
			return &DirectResponse{Text: "Global observer enabled here."}, nil
		case "off", "none":
			if err := s.store.SetGlobalObserverTarget(ctx, chatID, topicID, false); err != nil {
				return nil, err
			}
			return &DirectResponse{Text: "Global observer disabled."}, nil
		default:
			return &DirectResponse{Text: "Usage: /observe all|off"}, nil
		}
	case "/panelmode":
		if strings.TrimSpace(rest) == "" {
			return &DirectResponse{Text: fmt.Sprintf("Current panel mode: %s\nUse /panelmode per_run or /panelmode stable.", s.panelMode(ctx))}, nil
		}
		mode := normalizeRuntimePanelMode(rest)
		if mode == "" {
			return &DirectResponse{Text: "Usage: /panelmode per_run|stable"}, nil
		}
		if err := s.store.SetState(ctx, "ui.panel_mode", mode); err != nil {
			return nil, err
		}
		return &DirectResponse{Text: fmt.Sprintf("Panel mode switched to %s.", mode)}, nil
	case "/threads":
		return s.threadsOverview(ctx, rest)
	case "/projects":
		return s.projectsOverview(ctx)
	case "/show":
		decision, err := s.resolveRoute(ctx, chatID, topicID, rest, replyToMessageID)
		if err != nil {
			return nil, err
		}
		if decision.ThreadID == "" {
			return &DirectResponse{Text: "Usage: /show <thread> or reply to a thread message."}, nil
		}
		return s.showThread(ctx, chatID, topicID, decision.ThreadID, true)
	case "/bind":
		decision, err := s.resolveRoute(ctx, chatID, topicID, rest, replyToMessageID)
		if err != nil {
			return nil, err
		}
		if decision.ThreadID == "" {
			return &DirectResponse{Text: "Usage: /bind <thread> or reply to a thread message."}, nil
		}
		if err := s.store.SetBinding(ctx, chatID, topicID, decision.ThreadID, model.BindingModeBound); err != nil {
			return nil, err
		}
		s.kickBootstrap()
		return &DirectResponse{Text: fmt.Sprintf("Bound this chat to %s.", decision.ThreadID)}, nil
	case "/reply":
		replyParts := strings.SplitN(rest, " ", 2)
		if len(replyParts) < 2 {
			return &DirectResponse{Text: "Usage: /reply <thread> <text>"}, nil
		}
		return s.sendInputToThread(ctx, chatID, topicID, replyParts[0], replyParts[1])
	case "/repair":
		if err := s.RequestRepair(ctx, "telegram"); err != nil {
			return nil, err
		}
		return &DirectResponse{Text: "Repair requested. App-server sessions will be recreated in the background."}, nil
	case "/stop":
		return s.stopThread(ctx, chatID, topicID, rest, replyToMessageID)
	case "/approve":
		if strings.TrimSpace(rest) == "" {
			return &DirectResponse{Text: "Use approval buttons or /approve <request_id>."}, nil
		}
		return s.approve(ctx, rest, "accept")
	case "/deny":
		if strings.TrimSpace(rest) == "" {
			return &DirectResponse{Text: "Use deny button or /deny <request_id>."}, nil
		}
		return s.approve(ctx, rest, "decline")
	default:
		return &DirectResponse{Text: "Unknown command. Use /help."}, nil
	}
}

func (s *Service) handlePlainText(ctx context.Context, chatID, topicID int64, text string, replyToMessageID int64) (*DirectResponse, error) {
	decision, err := s.resolveRoute(ctx, chatID, topicID, "", replyToMessageID)
	if err != nil {
		return nil, err
	}
	if decision.ThreadID == "" {
		return &DirectResponse{Text: "No bound thread. Use /threads, /projects, /bind <thread>, or reply to a thread card."}, nil
	}
	if strings.TrimSpace(decision.RequestID) != "" {
		return s.respondUserInputRequest(ctx, decision.RequestID, text)
	}
	return s.sendInputToThreadTurn(ctx, chatID, topicID, decision.ThreadID, decision.TurnID, text)
}

func (s *Service) sendInputToThread(ctx context.Context, chatID, topicID int64, threadID, text string) (*DirectResponse, error) {
	return s.sendInputToThreadTurn(ctx, chatID, topicID, threadID, "", text)
}

func (s *Service) sendInputToThreadTurn(ctx context.Context, chatID, topicID int64, threadID, routeTurnID, text string) (*DirectResponse, error) {
	thread, _ := s.store.GetThread(ctx, threadID)
	if thread == nil {
		return &DirectResponse{Text: fmt.Sprintf("Unknown thread: %s", threadID)}, nil
	}
	s.mu.RLock()
	live := s.live
	connected := s.liveConnected
	s.mu.RUnlock()
	if !connected || live == nil {
		return &DirectResponse{Text: "Live app-server session is not ready yet. Try /status or /repair."}, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	_, err := live.ThreadResume(requestCtx, threadID, thread.CWD)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	steerState, _ := s.resolveArmedSteer(ctx, chatID, topicID)
	if steerState != nil && steerState.ThreadID == threadID && strings.TrimSpace(steerState.TurnID) != "" {
		result, err = live.TurnSteer(requestCtx, threadID, steerState.TurnID, text)
		if err == nil {
			_ = s.store.ClearSteerState(ctx, chatID, topicID)
		}
	}
	if (result == nil || err != nil) && strings.TrimSpace(routeTurnID) != "" {
		result, err = live.TurnSteer(requestCtx, threadID, routeTurnID, text)
	}
	if result == nil || err != nil {
		result, err = live.TurnStart(requestCtx, threadID, text, thread.CWD)
		if err != nil {
			return nil, err
		}
		_ = s.store.ClearSteerState(ctx, chatID, topicID)
	}
	turn := appserverThreadTurnID(result)
	if strings.TrimSpace(turn) == "" && strings.TrimSpace(routeTurnID) != "" && err == nil {
		turn = routeTurnID
	}
	if strings.TrimSpace(turn) != "" {
		_ = s.markTelegramOriginTurn(ctx, threadID, turn)
	}
	_, _ = s.refreshThread(ctx, live, threadID)
	explicitTarget := model.ObserverTarget{
		ChatKey: model.ChatKey(chatID, topicID),
		ChatID:  chatID,
		TopicID: topicID,
		Enabled: true,
	}
	s.syncThreadPanelToTarget(ctx, explicitTarget, threadID, true, model.PanelSourceTelegramInput)
	return &DirectResponse{ThreadID: threadID, TurnID: turn}, nil
}

func (s *Service) stopThread(ctx context.Context, chatID, topicID int64, explicitThreadID string, replyToMessageID int64) (*DirectResponse, error) {
	decision, err := s.resolveRoute(ctx, chatID, topicID, explicitThreadID, replyToMessageID)
	if err != nil {
		return nil, err
	}
	if decision.ThreadID == "" {
		return &DirectResponse{Text: "No thread target for /stop. Use /stop <thread> or reply to a thread message."}, nil
	}
	return s.interruptTurn(ctx, chatID, topicID, decision.ThreadID, "")
}

func (s *Service) interruptTurn(ctx context.Context, chatID, topicID int64, threadID, turnID string) (*DirectResponse, error) {
	if strings.TrimSpace(threadID) == "" {
		return &DirectResponse{CallbackText: "No thread target for stop."}, nil
	}
	thread, _ := s.store.GetThread(ctx, threadID)
	s.mu.RLock()
	live := s.live
	connected := s.liveConnected
	s.mu.RUnlock()
	if !connected || live == nil {
		return &DirectResponse{Text: "Live app-server session is not ready yet. Try /status or /repair."}, nil
	}
	if thread != nil {
		requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
		_, _ = live.ThreadResume(requestCtx, threadID, thread.CWD)
		cancel()
	}
	if refreshed, err := s.refreshThread(ctx, live, threadID); err == nil && refreshed != nil {
		thread = refreshed
	}
	if thread != nil && strings.TrimSpace(thread.ActiveTurnID) != "" {
		turnID = thread.ActiveTurnID
	}
	if strings.TrimSpace(turnID) == "" {
		explicitTarget := model.ObserverTarget{ChatKey: model.ChatKey(chatID, topicID), ChatID: chatID, TopicID: topicID, Enabled: true}
		s.syncThreadPanelToTarget(ctx, explicitTarget, threadID, false, model.PanelSourceExplicit)
		return &DirectResponse{CallbackText: "Thread is already idle."}, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	if err := live.TurnInterrupt(requestCtx, threadID, turnID); err != nil {
		return nil, err
	}
	label := threadID
	if thread != nil {
		label = thread.Label()
	}
	explicitTarget := model.ObserverTarget{ChatKey: model.ChatKey(chatID, topicID), ChatID: chatID, TopicID: topicID, Enabled: true}
	s.syncThreadPanelToTarget(ctx, explicitTarget, threadID, false, model.PanelSourceExplicit)
	return &DirectResponse{CallbackText: fmt.Sprintf("Interrupt requested for %s.", label), ThreadID: threadID, TurnID: turnID}, nil
}

func (s *Service) approve(ctx context.Context, requestID, decision string) (*DirectResponse, error) {
	approval, err := s.store.GetPendingApproval(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if approval == nil {
		return &DirectResponse{Text: fmt.Sprintf("Unknown approval request: %s", requestID)}, nil
	}
	s.mu.RLock()
	live := s.live
	connected := s.liveConnected
	s.mu.RUnlock()
	if !connected || live == nil {
		return &DirectResponse{Text: "Live app-server session is not ready yet. Try /repair."}, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := live.RespondServerRequest(requestCtx, requestID, map[string]any{"decision": decision}); err != nil {
		return nil, err
	}
	_ = s.store.UpdatePendingApprovalStatus(ctx, requestID, "resolved:"+decision)
	s.syncThreadPanel(ctx, approval.ThreadID)
	return &DirectResponse{CallbackText: fmt.Sprintf("Approval %s resolved.", requestID), ThreadID: approval.ThreadID}, nil
}

func (s *Service) answerChoice(ctx context.Context, chatID, topicID int64, route *model.CallbackRoute) (*DirectResponse, error) {
	if route == nil {
		return &DirectResponse{CallbackText: "No pending question for this button."}, nil
	}
	var payload map[string]any
	if strings.TrimSpace(route.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(route.PayloadJSON), &payload)
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", payload["text"]))
	if text == "" {
		return &DirectResponse{CallbackText: "Answer option is empty."}, nil
	}
	if strings.TrimSpace(route.RequestID) == "" {
		response, err := s.sendInputToThreadTurn(ctx, chatID, topicID, route.ThreadID, route.TurnID, text)
		if err != nil {
			return nil, err
		}
		if response == nil {
			response = &DirectResponse{}
		}
		response.CallbackText = "Ответ отправлен."
		return response, nil
	}
	response, err := s.respondUserInputRequest(ctx, route.RequestID, text)
	if err != nil {
		return nil, err
	}
	if response == nil {
		response = &DirectResponse{}
	}
	response.CallbackText = "Ответ отправлен."
	return response, nil
}

func (s *Service) threadsOverview(ctx context.Context, rest string) (*DirectResponse, error) {
	limit := 8
	search := ""
	if trimmed := strings.TrimSpace(rest); trimmed != "" {
		if parsed, err := strconv.Atoi(trimmed); err == nil {
			limit = parsed
		} else {
			search = trimmed
		}
	}
	threads, err := s.store.ListThreads(ctx, limit, search)
	if err != nil {
		return nil, err
	}
	lines := []string{"All chats"}
	if len(threads) == 0 {
		lines = append(lines, "No cached threads yet. Try /status or wait for sync.")
		return &DirectResponse{Text: strings.Join(lines, "\n")}, nil
	}
	buttons := [][]model.ButtonSpec{}
	for index, thread := range threads {
		lines = append(lines, fmt.Sprintf("%d. %s\n   %s | %s | %s\n   %s", index+1, strings.TrimSpace(thread.Title), thread.ProjectName, thread.DirectoryName, thread.ShortID(), trimPreview(thread.LastPreview)))
		buttons = append(buttons, []model.ButtonSpec{s.callbackButton(ctx, fmt.Sprintf("Open %d", index+1), "show_thread", thread.ID, "", "", nil)})
	}
	return &DirectResponse{Text: strings.Join(lines, "\n"), Buttons: buttons}, nil
}

func (s *Service) projectsOverview(ctx context.Context) (*DirectResponse, error) {
	grouped, err := s.store.ListProjectGroups(ctx)
	if err != nil {
		return nil, err
	}
	if len(grouped) == 0 {
		return &DirectResponse{Text: "No cached projects yet. Try /status or wait for sync."}, nil
	}
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := []string{"Projects"}
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s (%d thread(s))", key, len(grouped[key])))
		for _, thread := range grouped[key] {
			lines = append(lines, fmt.Sprintf("- %s | %s", thread.Title, thread.ShortID()))
		}
	}
	return &DirectResponse{Text: strings.Join(lines, "\n")}, nil
}

func (s *Service) showThread(ctx context.Context, chatID, topicID int64, threadID string, forceNew bool) (*DirectResponse, error) {
	thread, err := s.store.GetThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return &DirectResponse{Text: fmt.Sprintf("Unknown thread: %s", threadID)}, nil
	}
	s.mu.RLock()
	live := s.live
	liveConnected := s.liveConnected
	poll := s.poll
	pollConnected := s.pollConnected
	s.mu.RUnlock()
	switch {
	case liveConnected && live != nil:
		if refreshed, refreshErr := s.refreshThread(ctx, live, threadID); refreshErr == nil && refreshed != nil {
			thread = refreshed
		}
	case pollConnected && poll != nil:
		if refreshed, refreshErr := s.refreshThread(ctx, poll, threadID); refreshErr == nil && refreshed != nil {
			thread = refreshed
		}
	}
	target := model.ObserverTarget{
		ChatKey: model.ChatKey(chatID, topicID),
		ChatID:  chatID,
		TopicID: topicID,
		Enabled: true,
	}
	s.syncThreadPanelToTarget(ctx, target, thread.ID, forceNew, model.PanelSourceExplicit)
	return &DirectResponse{ThreadID: thread.ID}, nil
}

func (s *Service) contextCard(ctx context.Context, chatID, topicID int64) (string, error) {
	contextState, err := s.store.GetChatContext(ctx, chatID, topicID)
	if err != nil {
		return "", err
	}
	lines := []string{"Current context"}
	lines = append(lines, fmt.Sprintf("Panel mode: %s", s.panelMode(ctx)))
	switch {
	case contextState.Binding != nil && contextState.Thread != nil && contextState.ObserverEnabled:
		lines = append(lines, "Mode: Bound thread + global observer sink", fmt.Sprintf("Thread: %s", contextState.Thread.Label()), fmt.Sprintf("CWD: %s", contextState.Thread.CWD), "/observe off")
	case contextState.ObserverEnabled:
		lines = append(lines, "Mode: Global observer sink", "Passive monitoring is enabled here.", "/observe off")
	case contextState.Binding != nil && contextState.Thread != nil:
		lines = append(lines, "Mode: Bound thread", fmt.Sprintf("Thread: %s", contextState.Thread.Label()), fmt.Sprintf("CWD: %s", contextState.Thread.CWD))
	case contextState.Binding != nil:
		lines = append(lines, "Mode: Bound thread", fmt.Sprintf("Thread ID: %s", contextState.Binding.ThreadID))
	default:
		lines = append(lines, "Mode: Unbound", "Use /threads or /projects to choose a thread.", "/observe all")
	}
	if contextState.Thread != nil {
		if _, snapshot, err := s.loadThreadPanelSnapshot(ctx, contextState.Thread.ID); err == nil && snapshot != nil {
			if prompt := effectivePlanPrompt(nil, snapshot); prompt != nil {
				lines = append(lines, "", "Active Plan prompt:", trimPreview(prompt.Question))
			}
		}
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Service) panelMode(ctx context.Context) string {
	if mode, err := s.store.GetState(ctx, "ui.panel_mode"); err == nil {
		if normalized := normalizeRuntimePanelMode(mode); normalized != "" {
			return normalized
		}
	}
	if normalized := normalizeRuntimePanelMode(s.cfg.PanelMode); normalized != "" {
		return normalized
	}
	return model.PanelModePerRun
}

func normalizeRuntimePanelMode(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case model.PanelModePerRun:
		return model.PanelModePerRun
	case model.PanelModeStable:
		return model.PanelModeStable
	default:
		return ""
	}
}

func (s *Service) resolveRoute(ctx context.Context, chatID, topicID int64, explicitThreadID string, replyToMessageID int64) (model.RouteDecision, error) {
	if strings.TrimSpace(explicitThreadID) != "" {
		return model.RouteDecision{ThreadID: strings.TrimSpace(explicitThreadID), Source: model.RouteSourceExplicit}, nil
	}
	if replyToMessageID != 0 {
		route, err := s.store.ResolveMessageRoute(ctx, chatID, topicID, replyToMessageID)
		if err != nil {
			return model.RouteDecision{}, err
		}
		if route != nil {
			return model.RouteDecision{ThreadID: route.ThreadID, TurnID: route.TurnID, RequestID: requestIDFromRouteEvent(route.EventID), Source: model.RouteSourceReply}, nil
		}
	}
	steerState, err := s.resolveArmedSteer(ctx, chatID, topicID)
	if err != nil {
		return model.RouteDecision{}, err
	}
	if steerState != nil && strings.TrimSpace(steerState.ThreadID) != "" {
		return model.RouteDecision{ThreadID: steerState.ThreadID, Source: model.RouteSourceSteer}, nil
	}
	binding, err := s.store.GetBinding(ctx, chatID, topicID)
	if err != nil {
		return model.RouteDecision{}, err
	}
	if binding != nil {
		return model.RouteDecision{ThreadID: binding.ThreadID, Source: model.RouteSourceBinding}, nil
	}
	return model.RouteDecision{Source: model.RouteSourceNone}, nil
}

func (s *Service) respondUserInputRequest(ctx context.Context, requestID, text string) (*DirectResponse, error) {
	approval, err := s.store.GetPendingApproval(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if approval == nil {
		return &DirectResponse{Text: fmt.Sprintf("Unknown input request: %s", requestID)}, nil
	}
	s.mu.RLock()
	live := s.live
	connected := s.liveConnected
	s.mu.RUnlock()
	if !connected || live == nil {
		return &DirectResponse{Text: "Live app-server session is not ready yet. Try /repair."}, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := live.RespondServerRequest(requestCtx, requestID, userInputResponsePayload(approval.PayloadJSON, text)); err != nil {
		return &DirectResponse{Text: fmt.Sprintf("Could not send answer: %v", err)}, nil
	}
	_ = s.store.UpdatePendingApprovalStatus(ctx, requestID, "resolved:reply")
	s.syncThreadPanel(ctx, approval.ThreadID)
	return &DirectResponse{ThreadID: approval.ThreadID, TurnID: approval.TurnID}, nil
}

func userInputResponsePayload(payloadJSON, text string) map[string]any {
	var payload map[string]any
	if strings.TrimSpace(payloadJSON) != "" {
		_ = json.Unmarshal([]byte(payloadJSON), &payload)
	}
	questions, _ := payload["questions"].([]any)
	if len(questions) == 0 {
		return map[string]any{
			"text":     text,
			"value":    text,
			"response": text,
			"input":    text,
		}
	}
	answers := map[string]any{}
	for _, rawQuestion := range questions {
		question, _ := rawQuestion.(map[string]any)
		if question == nil {
			continue
		}
		id := strings.TrimSpace(fmt.Sprintf("%v", question["id"]))
		if id == "" || id == "<nil>" {
			continue
		}
		answers[id] = map[string]any{"answers": []string{text}}
	}
	if len(answers) == 0 {
		return map[string]any{
			"text":     text,
			"value":    text,
			"response": text,
			"input":    text,
		}
	}
	return map[string]any{"answers": answers}
}

func requestIDFromRouteEvent(eventID string) string {
	eventID = strings.TrimSpace(eventID)
	if !strings.HasPrefix(eventID, "plan_request:") {
		return ""
	}
	return strings.TrimPrefix(eventID, "plan_request:")
}

func (s *Service) enqueueObserverEvent(ctx context.Context, event model.ObserverEvent) {
	response := s.renderObserverEvent(ctx, event)
	s.enqueueRenderedToBackgroundTargets(ctx, response, event.ThreadID, event.TurnID, event.ItemID, event.EventID)
}

func (s *Service) enqueueRenderedToBackgroundTargets(ctx context.Context, response *DirectResponse, threadID, turnID, itemID, eventID string) {
	if response == nil {
		return
	}
	targets, err := s.backgroundTargets(ctx)
	if err != nil {
		s.setError(ctx, err)
		return
	}
	for _, target := range targets {
		payload := model.DeliveryPayload{
			Text:     response.Text,
			ThreadID: threadID,
			TurnID:   turnID,
			ItemID:   itemID,
			EventID:  eventID,
			Buttons:  response.Buttons,
		}
		item := model.DeliveryQueueItem{
			EventID:     eventID,
			ChatKey:     model.ChatKey(target.ChatID, target.TopicID),
			ChatID:      target.ChatID,
			TopicID:     target.TopicID,
			ThreadID:    threadID,
			Kind:        "observer",
			Status:      model.DeliveryStatusPending,
			AvailableAt: model.NowString(),
			PayloadJSON: storage.MustJSON(payload),
			CreatedAt:   model.NowString(),
			UpdatedAt:   model.NowString(),
		}
		_ = s.store.EnqueueDelivery(ctx, item)
	}
}

func (s *Service) backgroundTargets(ctx context.Context) ([]model.ObserverTarget, error) {
	target, err := s.currentBackgroundTarget(ctx)
	if err != nil {
		return nil, err
	}
	if target == nil || !target.Enabled {
		return nil, nil
	}
	return []model.ObserverTarget{*target}, nil
}

func (s *Service) defaultBackgroundTargets() []model.ObserverTarget {
	if len(s.cfg.AllowedUserIDs) == 1 {
		return []model.ObserverTarget{{ChatKey: model.ChatKey(s.cfg.AllowedUserIDs[0], 0), ChatID: s.cfg.AllowedUserIDs[0], TopicID: 0, Enabled: true}}
	}
	if len(s.cfg.AllowedUserIDs) == 0 && len(s.cfg.AllowedChatIDs) == 1 {
		return []model.ObserverTarget{{ChatKey: model.ChatKey(s.cfg.AllowedChatIDs[0], 0), ChatID: s.cfg.AllowedChatIDs[0], TopicID: 0, Enabled: true}}
	}
	return nil
}

func (s *Service) hasBackgroundTargets(ctx context.Context) bool {
	targets, err := s.backgroundTargets(ctx)
	return err == nil && len(targets) > 0
}

func (s *Service) trackedThreads(ctx context.Context, limit int) []model.Thread {
	seen := map[string]struct{}{}
	out := []model.Thread{}
	backgroundEnabled := s.hasBackgroundTargets(ctx)
	for _, threadID := range append(s.boundThreadIDs(ctx), s.currentPanelThreadIDs(ctx)...) {
		thread, err := s.store.GetThread(ctx, threadID)
		if err != nil || thread == nil {
			continue
		}
		seen[thread.ID] = struct{}{}
		out = append(out, *thread)
	}
	recent, _ := s.store.ListThreads(ctx, limit, "")
	for _, thread := range recent {
		if _, ok := seen[thread.ID]; ok {
			continue
		}
		if !threadLooksActiveForPolling(thread) {
			snapshot, _ := s.store.GetSnapshot(ctx, thread.ID)
			if !backgroundEnabled || (!s.threadNeedsCatchupPolling(ctx, thread, snapshot) && !threadHasActiveSessionTailTool(thread, "")) {
				continue
			}
		}
		seen[thread.ID] = struct{}{}
		out = append(out, thread)
	}
	return out
}

func (s *Service) boundThreadIDs(ctx context.Context) []string {
	boundIDs, err := s.store.ListBoundThreadIDs(ctx)
	if err != nil {
		return nil
	}
	return boundIDs
}

func (s *Service) currentPanelThreadIDs(ctx context.Context) []string {
	threads, err := s.store.ListThreads(ctx, 100, "")
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(threads))
	for _, thread := range threads {
		panels, err := s.store.ListCurrentPanelsForThread(ctx, thread.ID)
		if err != nil || len(panels) == 0 {
			continue
		}
		track := false
		for _, panel := range panels {
			if shouldTrackCurrentPanel(panel) || threadHasActiveSessionTailTool(thread, panel.CurrentTurnID) {
				track = true
				break
			}
		}
		if !track {
			continue
		}
		if _, ok := seen[thread.ID]; ok {
			continue
		}
		seen[thread.ID] = struct{}{}
		out = append(out, thread.ID)
	}
	return out
}

func shouldTrackCurrentPanel(panel model.ThreadPanel) bool {
	status := strings.TrimSpace(panel.Status)
	if status == "" {
		return true
	}
	return !isTerminalStatus(status)
}

func (s *Service) threadNeedsLiveSync(ctx context.Context, threadID string) bool {
	for _, boundID := range s.boundThreadIDs(ctx) {
		if boundID == threadID {
			return true
		}
	}
	panels, err := s.store.ListCurrentPanelsForThread(ctx, threadID)
	if err != nil {
		return false
	}
	for _, panel := range panels {
		if shouldTrackCurrentPanel(panel) {
			return true
		}
	}
	return false
}

func threadLooksActiveForPolling(thread model.Thread) bool {
	if strings.TrimSpace(thread.ActiveTurnID) != "" {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(thread.Status))
	if status == "" {
		return false
	}
	if strings.Contains(status, "waitingon") || strings.Contains(status, "inprogress") {
		return true
	}
	switch status {
	case "idle", "notloaded", "not_loaded", "completed", "interrupted", "failed", "cancelled", "canceled":
		return false
	default:
		return false
	}
}

func snapshotHasPassiveChange(previous *model.ThreadSnapshotState, current *appserver.ThreadReadSnapshot) bool {
	if current == nil {
		return false
	}
	if previous == nil {
		return threadLooksActiveForPolling(current.Thread) || current.WaitingOnApproval || current.WaitingOnReply
	}
	if strings.TrimSpace(previous.LastSeenTurnID) != "" &&
		previous.LastSeenTurnID == strings.TrimSpace(current.LatestTurnID) &&
		isTerminalStatus(previous.LastSeenTurnStatus) &&
		isTerminalStatus(current.LatestTurnStatus) {
		return false
	}
	return len(appserver.DiffSnapshot(previous, *current)) > 0
}

func (s *Service) threadNeedsCatchupPolling(ctx context.Context, thread model.Thread, snapshot *model.ThreadSnapshotState) bool {
	updatedAt := time.Unix(thread.UpdatedAt, 0).UTC()
	if thread.UpdatedAt <= 0 || updatedAt.IsZero() {
		return false
	}
	if sinceUnix := s.globalObserverSinceUnix(ctx); sinceUnix > 0 && thread.UpdatedAt < sinceUnix {
		return false
	}
	if time.Since(updatedAt) > s.catchupWindow() {
		return false
	}
	if snapshot == nil {
		return true
	}
	if thread.UpdatedAt > snapshot.ThreadUpdatedAt {
		return true
	}
	if snapshot.LastSeenThreadStatus != "" && !strings.EqualFold(strings.TrimSpace(snapshot.LastSeenThreadStatus), strings.TrimSpace(thread.Status)) {
		return true
	}
	if snapshot.LastSeenTurnID != "" && thread.ActiveTurnID != "" && snapshot.LastSeenTurnID != thread.ActiveTurnID {
		return true
	}
	return false
}

func (s *Service) catchupWindow() time.Duration {
	return maxDuration(90*time.Second, s.cfg.IndexRefreshInterval*2)
}

func mergeThreadMetadata(current, fallback model.Thread) model.Thread {
	if strings.TrimSpace(current.ID) == "" {
		current.ID = fallback.ID
	}
	if strings.TrimSpace(current.Title) == "" {
		current.Title = fallback.Title
	}
	if strings.TrimSpace(current.CWD) == "" {
		current.CWD = fallback.CWD
	}
	if strings.TrimSpace(current.ProjectName) == "" {
		current.ProjectName = fallback.ProjectName
	}
	if strings.TrimSpace(current.DirectoryName) == "" {
		current.DirectoryName = fallback.DirectoryName
	}
	if current.UpdatedAt == 0 {
		current.UpdatedAt = fallback.UpdatedAt
	}
	if strings.TrimSpace(current.Status) == "" {
		current.Status = fallback.Status
	}
	if strings.TrimSpace(current.LastPreview) == "" {
		current.LastPreview = fallback.LastPreview
	}
	if strings.TrimSpace(current.ActiveTurnID) == "" {
		current.ActiveTurnID = fallback.ActiveTurnID
	}
	if strings.TrimSpace(current.PreferredModel) == "" {
		current.PreferredModel = fallback.PreferredModel
	}
	if strings.TrimSpace(current.PermissionsMode) == "" {
		current.PermissionsMode = fallback.PermissionsMode
	}
	if len(current.Raw) == 0 {
		current.Raw = fallback.Raw
	}
	if !current.Archived {
		current.Archived = fallback.Archived
	}
	return current
}

func (s *Service) globalObserverSinceUnix(ctx context.Context) int64 {
	if sinceUnix, ok, err := s.store.GetGlobalObserverSinceUnix(ctx); err == nil && ok && sinceUnix > 0 {
		return sinceUnix
	}
	s.mu.RLock()
	startedAt := s.startedAt
	s.mu.RUnlock()
	if !startedAt.IsZero() {
		return startedAt.UTC().Unix()
	}
	raw, err := s.store.GetState(ctx, "daemon.started_at")
	if err != nil {
		return 0
	}
	if startedAt := parseTime(model.TimeString(raw)); !startedAt.IsZero() {
		return startedAt.UTC().Unix()
	}
	return 0
}

func (s *Service) refreshThread(ctx context.Context, client Session, threadID string) (*model.Thread, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	payload, err := client.ThreadRead(requestCtx, threadID, true)
	if err != nil {
		payload, err = client.ThreadRead(requestCtx, threadID, false)
		if err != nil {
			return nil, err
		}
	}
	current := appserver.SnapshotFromThreadRead(payload)
	current.Thread.Raw, _ = json.Marshal(payload)
	thread := current.Thread
	if existing, _ := s.store.GetThread(ctx, threadID); existing != nil {
		thread = mergeThreadMetadata(thread, *existing)
	} else if thread.ID == "" {
		thread.ID = threadID
	}
	current.Thread = thread
	_ = applySessionTailToolOverlay(current.Thread, &current)
	thread = current.Thread
	if err := s.store.UpsertThread(ctx, thread); err != nil {
		return nil, err
	}
	previous, err := s.store.GetSnapshot(ctx, threadID)
	if err != nil {
		return nil, err
	}
	nextSnapshot := appserver.CompactSnapshot(previous, current, time.Now().UTC())
	if current.LatestTurnStatus == "inProgress" || current.SessionTailActiveTool || current.WaitingOnApproval || current.WaitingOnReply {
		nextSnapshot.NextPollAfter = model.TimeString(time.Now().UTC().Add(s.cfg.ObserverPollInterval).Format(time.RFC3339Nano))
	}
	if err := s.store.UpsertSnapshot(ctx, threadID, nextSnapshot); err != nil {
		return nil, err
	}
	return &thread, nil
}

func (s *Service) callbackButton(ctx context.Context, text, action, threadID, turnID, requestID string, payload map[string]any) model.ButtonSpec {
	token := randomToken()
	route := model.CallbackRoute{
		Token:       token,
		Action:      action,
		ThreadID:    threadID,
		TurnID:      turnID,
		RequestID:   requestID,
		Status:      model.CallbackStatusActive,
		PayloadJSON: storage.MustJSON(payload),
		CreatedAt:   model.NowString(),
	}
	_ = s.store.PutCallbackRoute(ctx, route)
	return model.ButtonSpec{Text: text, CallbackData: token}
}

func (s *Service) kickBootstrap() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		s.bootstrapTrackedState(ctx)
	}()
}

func (s *Service) noteSessionError(ctx context.Context, operation string, err error) {
	if err == nil {
		return
	}
	s.setError(ctx, fmt.Errorf("%s: %w", operation, err))
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	_ = s.RequestRepair(ctx, operation)
}

func (s *Service) setError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	message := err.Error()
	s.mu.Lock()
	s.lastError = message
	s.mu.Unlock()
	_ = s.store.SetState(ctx, "daemon.last_error", message)
}

func (s *Service) renderObserverEvent(ctx context.Context, event model.ObserverEvent) *DirectResponse {
	thread := model.Thread{ID: event.ThreadID, Title: event.ThreadTitle, ProjectName: event.ProjectName}
	lines := []string{
		s.visualHeader(ctx, "Event", thread, event.TurnID),
		event.Text,
		"",
		fmt.Sprintf("/show %s", event.ThreadID),
		fmt.Sprintf("/reply %s <text>", event.ThreadID),
	}
	if event.NeedsApproval {
		lines = append(lines, "/status")
	}
	buttons := [][]model.ButtonSpec{
		{
			s.callbackButton(ctx, "Show", "show_thread", event.ThreadID, event.TurnID, "", nil),
			s.callbackButton(ctx, "Bind here", "bind_here", event.ThreadID, event.TurnID, "", nil),
		},
		{
			s.callbackButton(ctx, "Reply", "reply_hint", event.ThreadID, event.TurnID, "", nil),
			s.callbackButton(ctx, "Observe here", "observe_all", event.ThreadID, event.TurnID, "", nil),
		},
	}
	if event.TurnID != "" {
		buttons = append(buttons, []model.ButtonSpec{s.callbackButton(ctx, "Stop", "stop_turn", event.ThreadID, event.TurnID, "", nil)})
	}
	return &DirectResponse{Text: strings.Join(lines, "\n"), Buttons: buttons, ThreadID: event.ThreadID, TurnID: event.TurnID, ItemID: event.ItemID, EventID: event.EventID}
}

func (s *Service) renderPendingApproval(ctx context.Context, approval model.PendingApproval) *DirectResponse {
	thread := model.Thread{ID: approval.ThreadID, Title: approval.ThreadID, ProjectName: "Codex"}
	if loaded, _ := s.store.GetThread(ctx, approval.ThreadID); loaded != nil {
		thread = *loaded
	}
	lines := []string{
		s.visualHeader(ctx, "Approval", thread, approval.TurnID),
		strings.TrimSpace(approval.Question),
		"",
		fmt.Sprintf("/approve %s", approval.RequestID),
		fmt.Sprintf("/deny %s", approval.RequestID),
		fmt.Sprintf("/show %s", approval.ThreadID),
	}
	buttons := [][]model.ButtonSpec{
		{
			s.callbackButton(ctx, "Approve", "approve", approval.ThreadID, approval.TurnID, approval.RequestID, nil),
			s.callbackButton(ctx, "Approve Session", "approve_session", approval.ThreadID, approval.TurnID, approval.RequestID, nil),
		},
		{
			s.callbackButton(ctx, "Deny", "deny", approval.ThreadID, approval.TurnID, approval.RequestID, nil),
			s.callbackButton(ctx, "Cancel", "cancel", approval.ThreadID, approval.TurnID, approval.RequestID, nil),
		},
	}
	return &DirectResponse{Text: strings.Join(lines, "\n"), Buttons: buttons, ThreadID: approval.ThreadID, TurnID: approval.TurnID, ItemID: approval.ItemID, EventID: approval.RequestID}
}

func randomToken() string {
	var bytes [16]byte
	_, _ = rand.Read(bytes[:])
	return hex.EncodeToString(bytes[:])
}

func parseTime(value model.TimeString) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, string(value))
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func containsInt64(haystack []int64, needle int64) bool {
	for _, value := range haystack {
		if value == needle {
			return true
		}
	}
	return false
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func threadIDFromEvent(event appserver.Event) string {
	if event.Params == nil {
		return ""
	}
	if value, ok := event.Params["threadId"].(string); ok {
		return value
	}
	if thread, ok := event.Params["thread"].(map[string]any); ok {
		if value, ok := thread["id"].(string); ok {
			return value
		}
	}
	return ""
}

func appserverThreadTurnID(payload map[string]any) string {
	turn, _ := payload["turn"].(map[string]any)
	if turn == nil {
		return ""
	}
	if id, ok := turn["id"].(string); ok {
		return id
	}
	return ""
}

func trimPreview(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 120 {
		return value
	}
	return value[:117] + "..."
}
