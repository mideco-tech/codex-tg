package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

const (
	terminalGateDeferPrefix             = "turn_terminal.defer_empty_interrupted."
	terminalGateExplicitInterruptPrefix = "turn_terminal.explicit_interrupt."
	terminalGateMinimumGrace            = 90 * time.Second
	terminalGateDefaultHotPollInterval  = 5 * time.Second
)

type terminalGateDecisionKind string

const (
	terminalGateAccept  terminalGateDecisionKind = "accept"
	terminalGateDefer   terminalGateDecisionKind = "defer"
	terminalGateRecover terminalGateDecisionKind = "recover"
)

type terminalGateDecision struct {
	Action                terminalGateDecisionKind
	Reason                string
	ThreadID              string
	TurnID                string
	EmptyInterrupted      bool
	DeferrableInterrupted bool
	ExplicitInterrupt     bool
	Grace                 time.Duration
	FirstSeenAt           time.Time
	LastSeenAt            time.Time
	ExpiresAt             time.Time
	HotPoll               bool
	NextPollAfter         model.TimeString
	DeferKey              string
	ExplicitKey           string
}

type terminalGateState struct {
	ThreadID                  string           `json:"thread_id,omitempty"`
	TurnID                    string           `json:"turn_id,omitempty"`
	FirstSeenAt               model.TimeString `json:"first_seen_at,omitempty"`
	LastSeenAt                model.TimeString `json:"last_seen_at,omitempty"`
	ExpiresAt                 model.TimeString `json:"expires_at,omitempty"`
	NextPollAfter             model.TimeString `json:"next_poll_after,omitempty"`
	HotPollIntervalMillis     int64            `json:"hot_poll_interval_millis,omitempty"`
	EmptyInterruptedSeenCount int              `json:"empty_interrupted_seen_count,omitempty"`
	LastDecision              string           `json:"last_decision,omitempty"`
	LastReason                string           `json:"last_reason,omitempty"`
}

func isEmptyInterruptedSnapshot(snapshot *appserver.ThreadReadSnapshot) bool {
	if snapshot == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(snapshot.LatestTurnStatus), "interrupted") {
		return false
	}
	if strings.TrimSpace(snapshot.Thread.ID) == "" || strings.TrimSpace(snapshot.LatestTurnID) == "" {
		return false
	}
	if snapshot.WaitingOnApproval || snapshot.WaitingOnReply {
		return false
	}
	return !snapshotHasMeaningfulTerminalSignal(snapshot)
}

func isDeferrableInterruptedSnapshot(snapshot *appserver.ThreadReadSnapshot) bool {
	if snapshot == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(snapshot.LatestTurnStatus), "interrupted") {
		return false
	}
	if strings.TrimSpace(snapshot.Thread.ID) == "" || strings.TrimSpace(snapshot.LatestTurnID) == "" {
		return false
	}
	if snapshot.WaitingOnApproval || snapshot.WaitingOnReply {
		return false
	}
	return true
}

func interruptedDeferReason(snapshot *appserver.ThreadReadSnapshot) string {
	if isEmptyInterruptedSnapshot(snapshot) {
		return "empty_interrupted"
	}
	if snapshotHasFinalSignal(snapshot) {
		return "final_interrupted"
	}
	return "partial_interrupted"
}

func snapshotHasFinalSignal(snapshot *appserver.ThreadReadSnapshot) bool {
	if snapshot == nil {
		return false
	}
	if strings.TrimSpace(snapshot.LatestFinalText) != "" || strings.TrimSpace(snapshot.LatestFinalFP) != "" {
		return true
	}
	for _, entry := range snapshot.LatestAgentMessageEntries {
		if strings.EqualFold(strings.TrimSpace(entry.Phase), "final_answer") &&
			(strings.TrimSpace(entry.Text) != "" || strings.TrimSpace(entry.FP) != "") {
			return true
		}
	}
	for _, item := range snapshot.DetailItems {
		if strings.TrimSpace(item.Kind) == model.DetailItemFinal &&
			(strings.TrimSpace(item.Text) != "" ||
				strings.TrimSpace(item.Label) != "" ||
				strings.TrimSpace(item.Output) != "" ||
				strings.TrimSpace(item.FP) != "") {
			return true
		}
	}
	return false
}

func snapshotHasMeaningfulTerminalSignal(snapshot *appserver.ThreadReadSnapshot) bool {
	if snapshot == nil {
		return false
	}
	if strings.TrimSpace(snapshot.LatestFinalText) != "" || strings.TrimSpace(snapshot.LatestFinalFP) != "" {
		return true
	}
	if strings.TrimSpace(snapshot.LatestProgressText) != "" || strings.TrimSpace(snapshot.LatestProgressFP) != "" {
		return true
	}
	if strings.TrimSpace(snapshot.LatestToolID) != "" ||
		strings.TrimSpace(snapshot.LatestToolKind) != "" ||
		strings.TrimSpace(snapshot.LatestToolLabel) != "" ||
		strings.TrimSpace(snapshot.LatestToolOutput) != "" ||
		strings.TrimSpace(snapshot.LatestToolFP) != "" {
		return true
	}
	for _, message := range snapshot.LatestAgentMessages {
		if strings.TrimSpace(message) != "" {
			return true
		}
	}
	for _, entry := range snapshot.LatestAgentMessageEntries {
		if strings.TrimSpace(entry.Text) != "" || strings.TrimSpace(entry.FP) != "" {
			return true
		}
	}
	for _, item := range snapshot.DetailItems {
		switch strings.TrimSpace(item.Kind) {
		case "", model.DetailItemUser:
			continue
		default:
			if strings.TrimSpace(item.Text) != "" ||
				strings.TrimSpace(item.Label) != "" ||
				strings.TrimSpace(item.Output) != "" ||
				strings.TrimSpace(item.FP) != "" {
				return true
			}
		}
	}
	return false
}

func (s *Service) markTelegramOriginExplicitInterrupt(ctx context.Context, threadID, turnID string) error {
	key := terminalGateExplicitInterruptKey(threadID, turnID)
	if key == "" {
		return nil
	}
	now := time.Now().UTC()
	state := terminalGateState{
		ThreadID:     strings.TrimSpace(threadID),
		TurnID:       strings.TrimSpace(turnID),
		FirstSeenAt:  model.TimeString(now.Format(time.RFC3339Nano)),
		LastSeenAt:   model.TimeString(now.Format(time.RFC3339Nano)),
		LastDecision: string(terminalGateAccept),
		LastReason:   "explicit_interrupt",
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.store.SetState(ctx, key, string(payload))
}

func (s *Service) decideTelegramOriginEmptyInterruptedTerminal(ctx context.Context, snapshot *appserver.ThreadReadSnapshot, now time.Time) (terminalGateDecision, error) {
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	decision := terminalGateDecision{
		Action: terminalGateAccept,
		Reason: "not_deferrable_interrupted",
		Grace:  terminalGateGraceDuration(s.cfg.ObserverPollInterval),
	}
	threadID, turnID := terminalGateSnapshotIDs(snapshot)
	decision.ThreadID = threadID
	decision.TurnID = turnID
	decision.DeferKey = terminalGateDeferKey(threadID, turnID)
	decision.ExplicitKey = terminalGateExplicitInterruptKey(threadID, turnID)
	if threadID == "" || turnID == "" {
		decision.Reason = "missing_thread_or_turn"
		return decision, nil
	}

	explicit, err := s.hasTelegramOriginExplicitInterrupt(ctx, threadID, turnID)
	if err != nil {
		return decision, err
	}
	decision.ExplicitInterrupt = explicit

	emptyInterrupted := isEmptyInterruptedSnapshot(snapshot)
	decision.EmptyInterrupted = emptyInterrupted
	deferrableInterrupted := isDeferrableInterruptedSnapshot(snapshot)
	decision.DeferrableInterrupted = deferrableInterrupted
	existing, hasExisting, err := s.loadTelegramOriginEmptyInterruptedDefer(ctx, threadID, turnID, now)
	if err != nil {
		return decision, err
	}
	if !deferrableInterrupted {
		if hasExisting {
			_ = s.clearTelegramOriginEmptyInterruptedDefer(ctx, threadID, turnID)
			decision.Action = terminalGateRecover
			decision.Reason = "snapshot_recovered"
			decision.FirstSeenAt = parseTime(existing.FirstSeenAt)
			decision.LastSeenAt = now
			decision.ExpiresAt = parseTime(existing.ExpiresAt)
			if terminalGateSnapshotNeedsHotPolling(snapshot) {
				decision.HotPoll = true
				decision.NextPollAfter = terminalGateNextPollAfter(now, s.cfg.ObserverPollInterval)
			}
			return decision, nil
		}
		return decision, nil
	}

	if explicit {
		_ = s.clearTelegramOriginEmptyInterruptedDefer(ctx, threadID, turnID)
		decision.Reason = "explicit_interrupt"
		return decision, nil
	}

	if !s.isTelegramOriginTurn(ctx, threadID, turnID) {
		decision.Reason = "not_telegram_origin"
		return decision, nil
	}

	state := existing
	if !hasExisting {
		reason := interruptedDeferReason(snapshot)
		state = terminalGateState{
			ThreadID:     threadID,
			TurnID:       turnID,
			FirstSeenAt:  model.TimeString(now.Format(time.RFC3339Nano)),
			ExpiresAt:    model.TimeString(now.Add(decision.Grace).Format(time.RFC3339Nano)),
			LastDecision: string(terminalGateDefer),
			LastReason:   reason,
		}
	}
	state.ThreadID = threadID
	state.TurnID = turnID
	state.LastSeenAt = model.TimeString(now.Format(time.RFC3339Nano))
	state.EmptyInterruptedSeenCount++

	firstSeenAt := parseTime(state.FirstSeenAt)
	if firstSeenAt.IsZero() {
		firstSeenAt = now
		state.FirstSeenAt = model.TimeString(now.Format(time.RFC3339Nano))
	}
	expiresAt := parseTime(state.ExpiresAt)
	if expiresAt.IsZero() {
		expiresAt = firstSeenAt.Add(decision.Grace)
		state.ExpiresAt = model.TimeString(expiresAt.Format(time.RFC3339Nano))
	}
	decision.FirstSeenAt = firstSeenAt
	decision.LastSeenAt = now
	decision.ExpiresAt = expiresAt
	if !now.Before(expiresAt) {
		decision.Action = terminalGateAccept
		decision.Reason = "grace_expired"
		state.LastDecision = string(terminalGateAccept)
		state.LastReason = decision.Reason
		if err := s.saveTelegramOriginEmptyInterruptedDefer(ctx, threadID, turnID, state); err != nil {
			return decision, err
		}
		return decision, nil
	}

	decision.Action = terminalGateDefer
	decision.Reason = interruptedDeferReason(snapshot)
	decision.HotPoll = true
	decision.NextPollAfter = terminalGateNextPollAfter(now, s.cfg.ObserverPollInterval)
	state.NextPollAfter = decision.NextPollAfter
	state.HotPollIntervalMillis = terminalGateHotPollInterval(s.cfg.ObserverPollInterval).Milliseconds()
	state.LastDecision = string(decision.Action)
	state.LastReason = decision.Reason
	if err := s.saveTelegramOriginEmptyInterruptedDefer(ctx, threadID, turnID, state); err != nil {
		return decision, err
	}
	return decision, nil
}

func applyTerminalGateHotPolling(snapshot *model.ThreadSnapshotState, decision terminalGateDecision) {
	if snapshot == nil || !decision.HotPoll || strings.TrimSpace(string(decision.NextPollAfter)) == "" {
		return
	}
	snapshot.NextPollAfter = decision.NextPollAfter
}

func (s *Service) applyTelegramOriginTerminalGate(ctx context.Context, operation string, current *appserver.ThreadReadSnapshot, previous *model.ThreadSnapshotState) bool {
	decision, err := s.decideTelegramOriginEmptyInterruptedTerminal(ctx, current, time.Now().UTC())
	if err != nil {
		s.logLifecycle("telegram_origin_terminal_gate_error", lifecycleFields{
			"operation": operation,
			"thread_id": decision.ThreadID,
			"turn_id":   decision.TurnID,
			"error":     err,
		})
		return false
	}
	switch decision.Action {
	case terminalGateDefer:
		if previous != nil {
			next := *previous
			applyTerminalGateHotPolling(&next, decision)
			_ = s.store.UpsertSnapshot(ctx, decision.ThreadID, next)
		}
		fields := snapshotDiagnosticFields(*current)
		fields["operation"] = operation
		fields["reason"] = decision.Reason
		fields["defer_until"] = decision.ExpiresAt
		fields["next_poll_after"] = decision.NextPollAfter
		s.logLifecycle("telegram_origin_terminal_deferred", fields)
		return true
	case terminalGateRecover:
		fields := snapshotDiagnosticFields(*current)
		fields["operation"] = operation
		fields["reason"] = decision.Reason
		fields["first_seen_at"] = decision.FirstSeenAt
		s.logLifecycle("telegram_origin_terminal_recovered", fields)
	case terminalGateAccept:
		if decision.DeferrableInterrupted && decision.Reason == "grace_expired" {
			fields := snapshotDiagnosticFields(*current)
			fields["operation"] = operation
			fields["reason"] = decision.Reason
			fields["first_seen_at"] = decision.FirstSeenAt
			fields["defer_until"] = decision.ExpiresAt
			s.logLifecycle("telegram_origin_terminal_defer_expired", fields)
		}
	}
	return false
}

func (s *Service) threadHasDeferredEmptyInterrupted(ctx context.Context, thread model.Thread, snapshot *model.ThreadSnapshotState) bool {
	turnIDs := make([]string, 0, 2)
	if thread.ActiveTurnID != "" {
		turnIDs = append(turnIDs, strings.TrimSpace(thread.ActiveTurnID))
	}
	if snapshot != nil && snapshot.LastSeenTurnID != "" {
		turnIDs = append(turnIDs, strings.TrimSpace(snapshot.LastSeenTurnID))
	}
	seen := map[string]struct{}{}
	now := time.Now().UTC()
	for _, turnID := range turnIDs {
		if turnID == "" {
			continue
		}
		if _, ok := seen[turnID]; ok {
			continue
		}
		seen[turnID] = struct{}{}
		state, ok, err := s.loadTelegramOriginEmptyInterruptedDefer(ctx, thread.ID, turnID, now)
		if err != nil || !ok {
			continue
		}
		expiresAt := parseTime(state.ExpiresAt)
		if expiresAt.IsZero() || now.Before(expiresAt) {
			return true
		}
	}
	return false
}

func (s *Service) clearTelegramOriginTerminalGate(ctx context.Context, threadID, turnID string) error {
	if err := s.clearTelegramOriginEmptyInterruptedDefer(ctx, threadID, turnID); err != nil {
		return err
	}
	key := terminalGateExplicitInterruptKey(threadID, turnID)
	if key == "" {
		return nil
	}
	return s.store.SetState(ctx, key, "")
}

func (s *Service) clearTelegramOriginEmptyInterruptedDefer(ctx context.Context, threadID, turnID string) error {
	key := terminalGateDeferKey(threadID, turnID)
	if key == "" {
		return nil
	}
	return s.store.SetState(ctx, key, "")
}

func (s *Service) hasTelegramOriginExplicitInterrupt(ctx context.Context, threadID, turnID string) (bool, error) {
	key := terminalGateExplicitInterruptKey(threadID, turnID)
	if key == "" {
		return false, nil
	}
	value, err := s.store.GetState(ctx, key)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(value) != "", nil
}

func (s *Service) loadTelegramOriginEmptyInterruptedDefer(ctx context.Context, threadID, turnID string, now time.Time) (terminalGateState, bool, error) {
	key := terminalGateDeferKey(threadID, turnID)
	if key == "" {
		return terminalGateState{}, false, nil
	}
	value, err := s.store.GetState(ctx, key)
	if err != nil {
		return terminalGateState{}, false, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return terminalGateState{}, false, nil
	}
	var state terminalGateState
	if err := json.Unmarshal([]byte(value), &state); err == nil {
		return state, true, nil
	}
	firstSeenAt := parseTime(model.TimeString(value))
	if firstSeenAt.IsZero() {
		firstSeenAt = now.UTC()
	}
	return terminalGateState{
		ThreadID:     strings.TrimSpace(threadID),
		TurnID:       strings.TrimSpace(turnID),
		FirstSeenAt:  model.TimeString(firstSeenAt.Format(time.RFC3339Nano)),
		ExpiresAt:    model.TimeString(firstSeenAt.Add(terminalGateGraceDuration(s.cfg.ObserverPollInterval)).Format(time.RFC3339Nano)),
		LastDecision: string(terminalGateDefer),
		LastReason:   "legacy_state",
	}, true, nil
}

func (s *Service) saveTelegramOriginEmptyInterruptedDefer(ctx context.Context, threadID, turnID string, state terminalGateState) error {
	key := terminalGateDeferKey(threadID, turnID)
	if key == "" {
		return nil
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.store.SetState(ctx, key, string(payload))
}

func terminalGateSnapshotIDs(snapshot *appserver.ThreadReadSnapshot) (string, string) {
	if snapshot == nil {
		return "", ""
	}
	return strings.TrimSpace(snapshot.Thread.ID), strings.TrimSpace(snapshot.LatestTurnID)
}

func terminalGateDeferKey(threadID, turnID string) string {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return ""
	}
	return terminalGateDeferPrefix + threadID + "." + turnID
}

func terminalGateExplicitInterruptKey(threadID, turnID string) string {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return ""
	}
	return terminalGateExplicitInterruptPrefix + threadID + "." + turnID
}

func terminalGateGraceDuration(observerPollInterval time.Duration) time.Duration {
	return maxDuration(terminalGateMinimumGrace, observerPollInterval*12)
}

func terminalGateHotPollInterval(observerPollInterval time.Duration) time.Duration {
	if observerPollInterval > 0 {
		return observerPollInterval
	}
	return terminalGateDefaultHotPollInterval
}

func terminalGateNextPollAfter(now time.Time, observerPollInterval time.Duration) model.TimeString {
	return model.TimeString(now.UTC().Add(terminalGateHotPollInterval(observerPollInterval)).Format(time.RFC3339Nano))
}

func terminalGateSnapshotNeedsHotPolling(snapshot *appserver.ThreadReadSnapshot) bool {
	if snapshot == nil {
		return false
	}
	return threadLooksActiveForPolling(snapshot.Thread) ||
		strings.EqualFold(strings.TrimSpace(snapshot.LatestTurnStatus), "inProgress") ||
		snapshot.WaitingOnApproval ||
		snapshot.WaitingOnReply
}
