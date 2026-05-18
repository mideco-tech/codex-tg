package control

func DefaultNotificationSeverity(event NormalizedEvent) NotificationSeverity {
	switch event.Kind {
	case EventApprovalRequest, EventInputRequest:
		return NotificationUrgent
	case EventTurnCompleted, EventLegacyTaskComplete:
		return NotificationNormal
	case EventAgentMessage:
		if event.Phase == "final_answer" {
			return NotificationNormal
		}
		return NotificationSilent
	case EventTurnStarted, EventThreadStatus, EventToolStarted, EventToolUpdated, EventToolCompleted, EventLegacyTaskStarted:
		return NotificationSilent
	default:
		return NotificationSilent
	}
}
