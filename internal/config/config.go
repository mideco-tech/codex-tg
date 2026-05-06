package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Paths struct {
	Home    string
	DataDir string
	LogDir  string
	DBPath  string
}

func DefaultPaths() Paths {
	home := os.Getenv("CTR_GO_HOME")
	if strings.TrimSpace(home) == "" {
		userHome, _ := os.UserHomeDir()
		home = filepath.Join(userHome, ".codex-tg")
	}
	return Paths{
		Home:    home,
		DataDir: filepath.Join(home, "data"),
		LogDir:  filepath.Join(home, "logs"),
		DBPath:  filepath.Join(home, "data", "state.sqlite"),
	}
}

func (p Paths) Ensure() error {
	for _, dir := range []string{p.Home, p.DataDir, p.LogDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

type Config struct {
	Paths                       Paths
	CodexBin                    string
	AppServerListen             string
	TelegramBotToken            string
	AllowedUserIDs              []int64
	AllowedChatIDs              []int64
	DefaultCWD                  string
	CodexChatsRoot              string
	PanelMode                   string
	LogEnabled                  bool
	DiagnosticLogs              bool
	NotifyNewRun                bool
	ObserverPollInterval        time.Duration
	RequestTimeout              time.Duration
	IndexRefreshInterval        time.Duration
	AttachRefreshInterval       time.Duration
	DeliveryRetryBase           time.Duration
	DeliveryMaxAttempts         int
	ProjectsProjectPreviewLimit int
	ProjectsChatPreviewLimit    int
	ChatsPageSize               int
}

func FromEnv() Config {
	paths := DefaultPaths()
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	codexBin := strings.TrimSpace(os.Getenv("CTR_GO_CODEX_BIN"))
	if codexBin == "" {
		codexBin = "codex"
	}
	listen := strings.TrimSpace(os.Getenv("CTR_GO_APP_SERVER_LISTEN"))
	if listen == "" {
		listen = "stdio://"
	}
	token := strings.TrimSpace(os.Getenv("CTR_GO_TELEGRAM_BOT_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("CTR_TELEGRAM_BOT_TOKEN"))
	}
	return Config{
		Paths:                       paths,
		CodexBin:                    codexBin,
		AppServerListen:             listen,
		TelegramBotToken:            token,
		AllowedUserIDs:              parseInt64List(envFirst("CTR_GO_ALLOWED_USER_IDS", "CTR_ALLOWED_USER_IDS")),
		AllowedChatIDs:              parseInt64List(envFirst("CTR_GO_ALLOWED_CHAT_IDS", "CTR_ALLOWED_CHAT_IDS")),
		DefaultCWD:                  envString("CTR_GO_DEFAULT_CWD", cwd),
		CodexChatsRoot:              envPath("CTR_GO_CODEX_CHATS_ROOT", DefaultCodexChatsRoot()),
		PanelMode:                   normalizePanelMode(envString("CTR_GO_PANEL_MODE", "per_run")),
		LogEnabled:                  envBool("CTR_GO_LOG_ENABLED", true),
		DiagnosticLogs:              envBool("CTR_GO_DIAGNOSTIC_LOGS", true),
		NotifyNewRun:                envBool("CTR_GO_NOTIFY_NEW_RUN", true),
		ObserverPollInterval:        envDurationSeconds("CTR_GO_OBSERVER_POLL_SECONDS", 5*time.Second),
		RequestTimeout:              envDurationSeconds("CTR_GO_REQUEST_TIMEOUT_SECONDS", 30*time.Second),
		IndexRefreshInterval:        envDurationSeconds("CTR_GO_INDEX_REFRESH_SECONDS", 45*time.Second),
		AttachRefreshInterval:       envDurationSeconds("CTR_GO_ATTACH_REFRESH_SECONDS", 20*time.Second),
		DeliveryRetryBase:           envDurationSeconds("CTR_GO_DELIVERY_RETRY_SECONDS", 5*time.Second),
		DeliveryMaxAttempts:         envInt("CTR_GO_DELIVERY_MAX_ATTEMPTS", 5),
		ProjectsProjectPreviewLimit: envPositiveInt("CTR_GO_PROJECTS_PROJECT_PREVIEW_LIMIT", 7),
		ProjectsChatPreviewLimit:    envPositiveInt("CTR_GO_PROJECTS_CHAT_PREVIEW_LIMIT", 3),
		ChatsPageSize:               envPositiveInt("CTR_GO_CHATS_PAGE_SIZE", 8),
	}
}

func (c Config) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Home                        string  `json:"home"`
		DBPath                      string  `json:"db_path"`
		CodexBin                    string  `json:"codex_bin"`
		AppServerListen             string  `json:"app_server_listen"`
		HasTelegramToken            bool    `json:"telegram_configured"`
		AllowedUserIDs              []int64 `json:"allowed_user_ids"`
		AllowedChatIDs              []int64 `json:"allowed_chat_ids"`
		DefaultCWD                  string  `json:"default_cwd"`
		CodexChatsRoot              string  `json:"codex_chats_root"`
		PanelMode                   string  `json:"panel_mode"`
		LogEnabled                  bool    `json:"log_enabled"`
		DiagnosticLogs              bool    `json:"diagnostic_logs"`
		NotifyNewRun                bool    `json:"notify_new_run"`
		ObserverPollSeconds         float64 `json:"observer_poll_seconds"`
		RequestTimeoutSeconds       float64 `json:"request_timeout_seconds"`
		ProjectsProjectPreviewLimit int     `json:"projects_project_preview_limit"`
		ProjectsChatPreviewLimit    int     `json:"projects_chat_preview_limit"`
		ChatsPageSize               int     `json:"chats_page_size"`
		GoOS                        string  `json:"goos"`
		GoArch                      string  `json:"goarch"`
	}{
		Home:                        c.Paths.Home,
		DBPath:                      c.Paths.DBPath,
		CodexBin:                    c.CodexBin,
		AppServerListen:             c.AppServerListen,
		HasTelegramToken:            c.TelegramBotToken != "",
		AllowedUserIDs:              c.AllowedUserIDs,
		AllowedChatIDs:              c.AllowedChatIDs,
		DefaultCWD:                  c.DefaultCWD,
		CodexChatsRoot:              c.CodexChatsRoot,
		PanelMode:                   normalizePanelMode(c.PanelMode),
		LogEnabled:                  c.LogEnabled,
		DiagnosticLogs:              c.DiagnosticLogs,
		NotifyNewRun:                c.NotifyNewRun,
		ObserverPollSeconds:         c.ObserverPollInterval.Seconds(),
		RequestTimeoutSeconds:       c.RequestTimeout.Seconds(),
		ProjectsProjectPreviewLimit: positiveOrDefault(c.ProjectsProjectPreviewLimit, 7),
		ProjectsChatPreviewLimit:    positiveOrDefault(c.ProjectsChatPreviewLimit, 3),
		ChatsPageSize:               positiveOrDefault(c.ChatsPageSize, 8),
		GoOS:                        runtime.GOOS,
		GoArch:                      runtime.GOARCH,
	})
}

func DefaultCodexChatsRoot() string {
	userHome, _ := os.UserHomeDir()
	if strings.TrimSpace(userHome) == "" {
		return filepath.Join("Documents", "Codex")
	}
	return filepath.Join(userHome, "Documents", "Codex")
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envPath(key, fallback string) string {
	value := envString(key, fallback)
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return filepath.Clean(value)
}

func envFirst(keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}
	return ""
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envPositiveInt(key string, fallback int) int {
	return positiveOrDefault(envInt(key, fallback), fallback)
}

func positiveOrDefault(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "t", "yes", "y", "on", "enabled":
		return true
	case "0", "false", "f", "no", "n", "off", "disabled":
		return false
	default:
		return fallback
	}
}

func envDurationSeconds(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return time.Duration(parsed * float64(time.Second))
}

func parseInt64List(raw string) []int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	out := make([]int64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		value, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			continue
		}
		out = append(out, value)
	}
	return out
}

func normalizePanelMode(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "stable":
		return "stable"
	default:
		return "per_run"
	}
}
