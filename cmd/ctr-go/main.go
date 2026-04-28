package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/mideco-tech/codex-tg/internal/config"
	"github.com/mideco-tech/codex-tg/internal/daemon"
	"github.com/mideco-tech/codex-tg/internal/telegram"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.SetOutput(os.Stderr)
		log.Printf("ctr-go: %v", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stdout)
		return nil
	}
	cfg := config.FromEnv()
	switch args[0] {
	case "daemon":
		if len(args) < 2 || args[1] != "run" {
			return errors.New("usage: ctr-go daemon run")
		}
		return runDaemon(cfg)
	case "status":
		return runStatus(cfg)
	case "doctor":
		return runDoctor(cfg)
	case "repair":
		return runRepair(cfg)
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown command: %s", strings.Join(args, " "))
	}
}

func runDaemon(cfg config.Config) error {
	if strings.TrimSpace(cfg.TelegramBotToken) == "" {
		return errors.New("CTR_GO_TELEGRAM_BOT_TOKEN or CTR_TELEGRAM_BOT_TOKEN must be set")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := log.New(os.Stdout, "", log.LstdFlags)
	service, err := daemon.New(cfg)
	if err != nil {
		return err
	}
	defer service.Close()

	bot, err := telegram.NewBot(cfg, service, logger)
	if err != nil {
		return err
	}
	service.SetSender(bot)
	if err := service.Start(ctx); err != nil {
		return err
	}
	if err := bot.Start(ctx); err != nil {
		return err
	}
	logger.Printf("ctr-go daemon running with %s", bot.String())
	return bot.Run(ctx)
}

func runStatus(cfg config.Config) error {
	service, err := daemon.New(cfg)
	if err != nil {
		return err
	}
	defer service.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doctor, err := service.Doctor(ctx)
	if err != nil {
		return err
	}
	state := normalizeStringMap(doctor["daemon_state"])
	backlog := normalizeInt64(doctor["delivery_backlog"])

	lines := []string{
		"ctr-go status",
		fmt.Sprintf("Home: %s", cfg.Paths.Home),
		fmt.Sprintf("DB: %s", cfg.Paths.DBPath),
		fmt.Sprintf("Codex bin: %s", cfg.CodexBin),
		fmt.Sprintf("Telegram configured: %t", strings.TrimSpace(cfg.TelegramBotToken) != ""),
		fmt.Sprintf("Allowed users: %s", formatIDs(cfg.AllowedUserIDs)),
		fmt.Sprintf("Allowed chats: %s", formatIDs(cfg.AllowedChatIDs)),
		fmt.Sprintf("Default cwd: %s", cfg.DefaultCWD),
		fmt.Sprintf("Delivery backlog: %d", backlog),
		"",
		"Persisted daemon state:",
	}
	if len(state) == 0 {
		lines = append(lines, "(empty)")
	} else {
		keys := make([]string, 0, len(state))
		for key := range state {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("%s = %s", key, state[key]))
		}
	}
	_, _ = fmt.Fprintln(os.Stdout, strings.Join(lines, "\n"))
	return nil
}

func runDoctor(cfg config.Config) error {
	service, err := daemon.New(cfg)
	if err != nil {
		return err
	}
	defer service.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doctor, err := service.Doctor(ctx)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(doctor, "", "  ")
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(os.Stdout, string(encoded))
	return nil
}

func runRepair(cfg config.Config) error {
	service, err := daemon.New(cfg)
	if err != nil {
		return err
	}
	defer service.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := service.RequestRepair(ctx, "cli"); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(os.Stdout, "Repair requested.")
	return nil
}

func printUsage(out *os.File) {
	_, _ = fmt.Fprintln(out, "Usage:")
	_, _ = fmt.Fprintln(out, "  ctr-go daemon run")
	_, _ = fmt.Fprintln(out, "  ctr-go status")
	_, _ = fmt.Fprintln(out, "  ctr-go doctor")
	_, _ = fmt.Fprintln(out, "  ctr-go repair")
}

func formatIDs(values []int64) string {
	if len(values) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%d", value))
	}
	return strings.Join(parts, ", ")
}

func normalizeStringMap(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		return typed
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, raw := range typed {
			out[key] = fmt.Sprintf("%v", raw)
		}
		return out
	default:
		return nil
	}
}

func normalizeInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}
