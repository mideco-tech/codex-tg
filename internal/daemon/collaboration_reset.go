package daemon

import (
	"context"
	"strings"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

const (
	threadCollaborationOverridePrefix = "thread_collaboration_override."
	threadCollaborationMarkerPrefix   = "thread_collaboration_marker."
)

func threadCollaborationOverrideKey(threadID string) string {
	return threadCollaborationOverridePrefix + strings.TrimSpace(threadID)
}

func threadCollaborationMarkerKey(threadID string) string {
	return threadCollaborationMarkerPrefix + strings.TrimSpace(threadID)
}

func (s *Service) setThreadCollaborationDefaultOverride(ctx context.Context, threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	return s.store.SetState(ctx, threadCollaborationOverrideKey(threadID), collaborationModeDefault)
}

func (s *Service) clearThreadCollaborationOverride(ctx context.Context, threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	return s.store.DeleteState(ctx, threadCollaborationOverrideKey(threadID))
}

func (s *Service) threadCollaborationOverride(ctx context.Context, threadID string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return ""
	}
	value, err := s.store.GetState(ctx, threadCollaborationOverrideKey(threadID))
	if err != nil {
		return ""
	}
	if strings.TrimSpace(value) == collaborationModeDefault {
		return collaborationModeDefault
	}
	return ""
}

func (s *Service) setThreadCollaborationMarker(ctx context.Context, threadID, turnID, mode string) error {
	threadID = strings.TrimSpace(threadID)
	mode = strings.TrimSpace(mode)
	if threadID == "" {
		return nil
	}
	if mode != collaborationModePlan {
		return s.clearThreadCollaborationMarker(ctx, threadID)
	}
	value := collaborationModePlan
	if turnID = strings.TrimSpace(turnID); turnID != "" {
		value += ":" + turnID
	}
	return s.store.SetState(ctx, threadCollaborationMarkerKey(threadID), value)
}

func (s *Service) clearThreadCollaborationMarker(ctx context.Context, threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	return s.store.DeleteState(ctx, threadCollaborationMarkerKey(threadID))
}

func (s *Service) threadCollaborationMarker(ctx context.Context, threadID, turnID string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return ""
	}
	value, err := s.store.GetState(ctx, threadCollaborationMarkerKey(threadID))
	if err != nil {
		return ""
	}
	value = strings.TrimSpace(value)
	if value == collaborationModePlan {
		return collaborationModePlan
	}
	prefix := collaborationModePlan + ":"
	if strings.HasPrefix(value, prefix) {
		markedTurnID := strings.TrimSpace(strings.TrimPrefix(value, prefix))
		if markedTurnID != "" && (strings.TrimSpace(turnID) == "" || markedTurnID == strings.TrimSpace(turnID)) {
			return collaborationModePlan
		}
	}
	return ""
}

func (s *Service) finalCardShouldShowTurnOffPlan(ctx context.Context, threadID string, snapshot *appserver.ThreadReadSnapshot) bool {
	if s.threadCollaborationOverride(ctx, threadID) == collaborationModeDefault {
		return false
	}
	if snapshotLooksPlanLike(snapshot) {
		return true
	}
	if snapshot != nil && s.threadCollaborationMarker(ctx, threadID, snapshot.LatestTurnID) == collaborationModePlan {
		return true
	}
	return false
}

func snapshotLooksPlanLike(snapshot *appserver.ThreadReadSnapshot) bool {
	if snapshot == nil {
		return false
	}
	turnID := strings.TrimSpace(snapshot.LatestTurnID)
	if snapshot.PlanPrompt != nil {
		promptTurnID := strings.TrimSpace(snapshot.PlanPrompt.TurnID)
		if promptTurnID == "" || turnID == "" || promptTurnID == turnID {
			return true
		}
	}
	for _, item := range snapshot.DetailItems {
		if item.Kind == model.DetailItemPlan {
			return true
		}
	}
	return false
}
