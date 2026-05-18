package control

import "testing"

func TestNotificationSeverityValues(t *testing.T) {
	tests := map[NotificationSeverity]string{
		NotificationUrgent: "urgent",
		NotificationNormal: "normal",
		NotificationSilent: "silent",
		NotificationDigest: "digest",
	}
	for got, want := range tests {
		if string(got) != want {
			t.Fatalf("severity %q = %q, want %q", got, string(got), want)
		}
	}
}
