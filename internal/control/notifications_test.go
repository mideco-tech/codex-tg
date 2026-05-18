package control

import "testing"

func TestDefaultNotificationSeverity(t *testing.T) {
	tests := []struct {
		name  string
		event NormalizedEvent
		want  NotificationSeverity
	}{
		{
			name:  "approval is urgent",
			event: NormalizedEvent{Kind: EventApprovalRequest},
			want:  NotificationUrgent,
		},
		{
			name:  "input is urgent",
			event: NormalizedEvent{Kind: EventInputRequest},
			want:  NotificationUrgent,
		},
		{
			name:  "turn completed is normal",
			event: NormalizedEvent{Kind: EventTurnCompleted},
			want:  NotificationNormal,
		},
		{
			name:  "final answer is normal",
			event: NormalizedEvent{Kind: EventAgentMessage, Phase: "final_answer"},
			want:  NotificationNormal,
		},
		{
			name:  "commentary is silent",
			event: NormalizedEvent{Kind: EventAgentMessage, Phase: "commentary"},
			want:  NotificationSilent,
		},
		{
			name:  "tool update is silent",
			event: NormalizedEvent{Kind: EventToolUpdated},
			want:  NotificationSilent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultNotificationSeverity(tt.event); got != tt.want {
				t.Fatalf("DefaultNotificationSeverity = %q, want %q", got, tt.want)
			}
		})
	}
}
