package tests

import (
	"testing"
	"time"

	"github.com/mideco-tech/codex-tg/internal/config"
)

func TestFromEnvPrefersGoScopedEnvVars(t *testing.T) {
	t.Setenv("CTR_GO_HOME", `C:\tmp\ctr-go`)
	t.Setenv("CTR_GO_CODEX_BIN", `C:\tools\codex.exe`)
	t.Setenv("CTR_GO_APP_SERVER_LISTEN", "stdio://")
	t.Setenv("CTR_GO_TELEGRAM_BOT_TOKEN", "go-token")
	t.Setenv("CTR_TELEGRAM_BOT_TOKEN", "legacy-token")
	t.Setenv("CTR_GO_ALLOWED_USER_IDS", "1,2")
	t.Setenv("CTR_ALLOWED_USER_IDS", "99")
	t.Setenv("CTR_GO_ALLOWED_CHAT_IDS", "10 20")
	t.Setenv("CTR_GO_DEFAULT_CWD", `C:\workspace`)
	t.Setenv("CTR_GO_LOG_ENABLED", "off")
	t.Setenv("CTR_GO_DIAGNOSTIC_LOGS", "no")
	t.Setenv("CTR_GO_NOTIFY_NEW_RUN", "off")
	t.Setenv("CTR_GO_OBSERVER_POLL_SECONDS", "7")
	t.Setenv("CTR_GO_REQUEST_TIMEOUT_SECONDS", "31")
	t.Setenv("CTR_GO_INDEX_REFRESH_SECONDS", "46")
	t.Setenv("CTR_GO_ATTACH_REFRESH_SECONDS", "21")
	t.Setenv("CTR_GO_DELIVERY_RETRY_SECONDS", "6")
	t.Setenv("CTR_GO_DELIVERY_MAX_ATTEMPTS", "8")
	t.Setenv("CTR_GO_PROJECTS_PROJECT_PREVIEW_LIMIT", "11")
	t.Setenv("CTR_GO_PROJECTS_CHAT_PREVIEW_LIMIT", "4")
	t.Setenv("CTR_GO_CHATS_PAGE_SIZE", "9")

	cfg := config.FromEnv()

	if got, want := cfg.Paths.Home, `C:\tmp\ctr-go`; got != want {
		t.Fatalf("Paths.Home = %q, want %q", got, want)
	}
	if got, want := cfg.CodexBin, `C:\tools\codex.exe`; got != want {
		t.Fatalf("CodexBin = %q, want %q", got, want)
	}
	if got, want := cfg.TelegramBotToken, "go-token"; got != want {
		t.Fatalf("TelegramBotToken = %q, want %q", got, want)
	}
	if got := cfg.AllowedUserIDs; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("AllowedUserIDs = %#v, want [1 2]", got)
	}
	if got := cfg.AllowedChatIDs; len(got) != 2 || got[0] != 10 || got[1] != 20 {
		t.Fatalf("AllowedChatIDs = %#v, want [10 20]", got)
	}
	if got, want := cfg.DefaultCWD, `C:\workspace`; got != want {
		t.Fatalf("DefaultCWD = %q, want %q", got, want)
	}
	if cfg.LogEnabled {
		t.Fatal("LogEnabled = true, want false")
	}
	if cfg.DiagnosticLogs {
		t.Fatal("DiagnosticLogs = true, want false")
	}
	if cfg.NotifyNewRun {
		t.Fatal("NotifyNewRun = true, want false")
	}
	if got, want := cfg.ObserverPollInterval, 7*time.Second; got != want {
		t.Fatalf("ObserverPollInterval = %v, want %v", got, want)
	}
	if got, want := cfg.RequestTimeout, 31*time.Second; got != want {
		t.Fatalf("RequestTimeout = %v, want %v", got, want)
	}
	if got, want := cfg.IndexRefreshInterval, 46*time.Second; got != want {
		t.Fatalf("IndexRefreshInterval = %v, want %v", got, want)
	}
	if got, want := cfg.AttachRefreshInterval, 21*time.Second; got != want {
		t.Fatalf("AttachRefreshInterval = %v, want %v", got, want)
	}
	if got, want := cfg.DeliveryRetryBase, 6*time.Second; got != want {
		t.Fatalf("DeliveryRetryBase = %v, want %v", got, want)
	}
	if got, want := cfg.DeliveryMaxAttempts, 8; got != want {
		t.Fatalf("DeliveryMaxAttempts = %d, want %d", got, want)
	}
	if got, want := cfg.ProjectsProjectPreviewLimit, 11; got != want {
		t.Fatalf("ProjectsProjectPreviewLimit = %d, want %d", got, want)
	}
	if got, want := cfg.ProjectsChatPreviewLimit, 4; got != want {
		t.Fatalf("ProjectsChatPreviewLimit = %d, want %d", got, want)
	}
	if got, want := cfg.ChatsPageSize, 9; got != want {
		t.Fatalf("ChatsPageSize = %d, want %d", got, want)
	}
}

func TestFromEnvProjectChatLimitsClampInvalidValues(t *testing.T) {
	t.Setenv("CTR_GO_PROJECTS_PROJECT_PREVIEW_LIMIT", "0")
	t.Setenv("CTR_GO_PROJECTS_CHAT_PREVIEW_LIMIT", "-1")
	t.Setenv("CTR_GO_CHATS_PAGE_SIZE", "wat")

	cfg := config.FromEnv()

	if got, want := cfg.ProjectsProjectPreviewLimit, 7; got != want {
		t.Fatalf("ProjectsProjectPreviewLimit = %d, want default %d", got, want)
	}
	if got, want := cfg.ProjectsChatPreviewLimit, 3; got != want {
		t.Fatalf("ProjectsChatPreviewLimit = %d, want default %d", got, want)
	}
	if got, want := cfg.ChatsPageSize, 8; got != want {
		t.Fatalf("ChatsPageSize = %d, want default %d", got, want)
	}
}

func TestFromEnvDefaultsLoggingOn(t *testing.T) {
	t.Setenv("CTR_GO_LOG_ENABLED", "")
	t.Setenv("CTR_GO_DIAGNOSTIC_LOGS", "")
	t.Setenv("CTR_GO_NOTIFY_NEW_RUN", "")

	cfg := config.FromEnv()

	if !cfg.LogEnabled {
		t.Fatal("LogEnabled = false, want true")
	}
	if !cfg.DiagnosticLogs {
		t.Fatal("DiagnosticLogs = false, want true")
	}
	if !cfg.NotifyNewRun {
		t.Fatal("NotifyNewRun = false, want true")
	}
}

func TestFromEnvInvalidLoggingFlagsFallBackToEnabled(t *testing.T) {
	t.Setenv("CTR_GO_LOG_ENABLED", "wat")
	t.Setenv("CTR_GO_DIAGNOSTIC_LOGS", "maybe")
	t.Setenv("CTR_GO_NOTIFY_NEW_RUN", "perhaps")

	cfg := config.FromEnv()

	if !cfg.LogEnabled {
		t.Fatal("LogEnabled = false, want true fallback")
	}
	if !cfg.DiagnosticLogs {
		t.Fatal("DiagnosticLogs = false, want true fallback")
	}
	if !cfg.NotifyNewRun {
		t.Fatal("NotifyNewRun = false, want true fallback")
	}
}

func TestFromEnvFallsBackToLegacyTelegramVariables(t *testing.T) {
	t.Setenv("CTR_GO_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("CTR_TELEGRAM_BOT_TOKEN", "legacy-token")
	t.Setenv("CTR_GO_ALLOWED_USER_IDS", "")
	t.Setenv("CTR_ALLOWED_USER_IDS", "123456789")
	t.Setenv("CTR_GO_ALLOWED_CHAT_IDS", "")
	t.Setenv("CTR_ALLOWED_CHAT_IDS", "123456789")

	cfg := config.FromEnv()

	if got, want := cfg.TelegramBotToken, "legacy-token"; got != want {
		t.Fatalf("TelegramBotToken = %q, want %q", got, want)
	}
	if got := cfg.AllowedUserIDs; len(got) != 1 || got[0] != 123456789 {
		t.Fatalf("AllowedUserIDs = %#v, want [123456789]", got)
	}
	if got := cfg.AllowedChatIDs; len(got) != 1 || got[0] != 123456789 {
		t.Fatalf("AllowedChatIDs = %#v, want [123456789]", got)
	}
}
