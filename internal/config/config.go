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
		home = filepath.Join(userHome, ".codex-telegram-remote-go")
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
	Paths                 Paths
	CodexBin              string
	AppServerListen       string
	TelegramBotToken      string
	AllowedUserIDs        []int64
	AllowedChatIDs        []int64
	DefaultCWD            string
	PanelMode             string
	ObserverPollInterval  time.Duration
	RequestTimeout        time.Duration
	IndexRefreshInterval  time.Duration
	AttachRefreshInterval time.Duration
	DeliveryRetryBase     time.Duration
	DeliveryMaxAttempts   int
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
		Paths:                 paths,
		CodexBin:              codexBin,
		AppServerListen:       listen,
		TelegramBotToken:      token,
		AllowedUserIDs:        parseInt64List(envFirst("CTR_GO_ALLOWED_USER_IDS", "CTR_ALLOWED_USER_IDS")),
		AllowedChatIDs:        parseInt64List(envFirst("CTR_GO_ALLOWED_CHAT_IDS", "CTR_ALLOWED_CHAT_IDS")),
		DefaultCWD:            envString("CTR_GO_DEFAULT_CWD", cwd),
		PanelMode:             normalizePanelMode(envString("CTR_GO_PANEL_MODE", "per_run")),
		ObserverPollInterval:  envDurationSeconds("CTR_GO_OBSERVER_POLL_SECONDS", 5*time.Second),
		RequestTimeout:        envDurationSeconds("CTR_GO_REQUEST_TIMEOUT_SECONDS", 30*time.Second),
		IndexRefreshInterval:  envDurationSeconds("CTR_GO_INDEX_REFRESH_SECONDS", 45*time.Second),
		AttachRefreshInterval: envDurationSeconds("CTR_GO_ATTACH_REFRESH_SECONDS", 20*time.Second),
		DeliveryRetryBase:     envDurationSeconds("CTR_GO_DELIVERY_RETRY_SECONDS", 5*time.Second),
		DeliveryMaxAttempts:   envInt("CTR_GO_DELIVERY_MAX_ATTEMPTS", 5),
	}
}

func (c Config) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Home                  string  `json:"home"`
		DBPath                string  `json:"db_path"`
		CodexBin              string  `json:"codex_bin"`
		AppServerListen       string  `json:"app_server_listen"`
		HasTelegramToken      bool    `json:"telegram_configured"`
		AllowedUserIDs        []int64 `json:"allowed_user_ids"`
		AllowedChatIDs        []int64 `json:"allowed_chat_ids"`
		DefaultCWD            string  `json:"default_cwd"`
		PanelMode             string  `json:"panel_mode"`
		ObserverPollSeconds   float64 `json:"observer_poll_seconds"`
		RequestTimeoutSeconds float64 `json:"request_timeout_seconds"`
		GoOS                  string  `json:"goos"`
		GoArch                string  `json:"goarch"`
	}{
		Home:                  c.Paths.Home,
		DBPath:                c.Paths.DBPath,
		CodexBin:              c.CodexBin,
		AppServerListen:       c.AppServerListen,
		HasTelegramToken:      c.TelegramBotToken != "",
		AllowedUserIDs:        c.AllowedUserIDs,
		AllowedChatIDs:        c.AllowedChatIDs,
		DefaultCWD:            c.DefaultCWD,
		PanelMode:             normalizePanelMode(c.PanelMode),
		ObserverPollSeconds:   c.ObserverPollInterval.Seconds(),
		RequestTimeoutSeconds: c.RequestTimeout.Seconds(),
		GoOS:                  runtime.GOOS,
		GoArch:                runtime.GOARCH,
	})
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
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
