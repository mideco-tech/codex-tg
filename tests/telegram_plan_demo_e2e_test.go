//go:build demo_e2e

package tests

import (
	"context"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"codex-telegram-remote-go/internal/model"
	"codex-telegram-remote-go/internal/telegram"
	"codex-telegram-remote-go/internal/tgformat"
)

const demoPrompt = "I am preparing for relocation and want to work as an LLM engineer.\n" +
	"Please review this repository as a public portfolio project:\n" +
	"run the Go test suite, inspect the architecture, and tell me whether it is ready for a v0.1 GitHub release.\n\n" +
	"Please check:\n\n" +
	"- `codex app-server` integration\n" +
	"- Telegram Plan Mode UX\n" +
	"- local-first SQLite state\n" +
	"- public README and GitHub docs"

func TestTelegramPlanModeScreenshotDemo(t *testing.T) {
	if os.Getenv("CTR_DEMO_TELEGRAM_E2E") != "1" {
		t.Skip("set CTR_DEMO_TELEGRAM_E2E=1 to send the live Telegram screenshot demo")
	}
	token := strings.TrimSpace(os.Getenv("CTR_GO_TELEGRAM_BOT_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("CTR_TELEGRAM_BOT_TOKEN"))
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("CTR_DEMO_TELEGRAM_CHAT_ID")), 10, 64)
	if err != nil || chatID == 0 || token == "" {
		t.Fatalf("CTR_DEMO_TELEGRAM_CHAT_ID and CTR_GO_TELEGRAM_BOT_TOKEN are required")
	}
	keepMessages := strings.EqualFold(strings.TrimSpace(os.Getenv("CTR_DEMO_KEEP_MESSAGES")), "true")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := telegram.NewClient(token)
	var sent []int64
	cleanup := func() {
		if keepMessages {
			return
		}
		for i := len(sent) - 1; i >= 0; i-- {
			_ = client.DeleteMessage(context.Background(), chatID, sent[i])
		}
	}
	defer cleanup()

	sendRendered := func(messages []model.RenderedMessage, markup *telegram.InlineKeyboardMarkup) {
		for index, message := range messages {
			buttons := markup
			if index != len(messages)-1 {
				buttons = nil
			}
			result, err := client.SendRenderedMessage(ctx, chatID, 0, message, buttons)
			if err != nil {
				t.Fatalf("SendRenderedMessage failed: %v", err)
			}
			sent = append(sent, result.MessageID)
			time.Sleep(350 * time.Millisecond)
		}
	}
	sendText := func(text string) {
		result, err := client.SendMessage(ctx, chatID, 0, text, nil)
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}
		sent = append(sent, result.MessageID)
		time.Sleep(350 * time.Millisecond)
	}

	sendRendered(tgformat.RenderMarkdownWithHeader(demoHeader("User")+"\nStatus: inProgress", demoPrompt), nil)
	sendRendered(tgformat.RenderMarkdownWithHeader(demoHeader("Plan")+"\nStatus: waiting for input", "Choose validation depth before I continue."), &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{{
			{Text: "Fast smoke", CallbackData: "demo_fast_smoke"},
			{Text: "Full test suite", CallbackData: "demo_full_test_suite"},
		}},
	})
	commentary := `I will validate the release with **real commands** and keep the Telegram view readable.

Planned checks:

1. Run ` + "`go test ./...`" + `.
2. Review the docs for a public ` + "`v0.1`" + ` release.
3. Confirm the demo highlights Markdown-to-Telegram formatting.

` + "```bash\n" + `go test ./...
` + "```"
	sendRendered(tgformat.RenderMarkdownWithHeader(demoHeader("commentary")+"\nStatus: inProgress", commentary), nil)
	sendText(demoHeader("Tool") + "\n[Shell:go]\n" + htmlCode("powershell", "go test ./...") + "\nStatus: running")

	output, commandErr := runDemoGoTest(ctx)
	sendText(demoHeader("Output") + "\n" + htmlCode("", trimDemoOutput(output)))
	final := `This repository is ready for a public **v0.1** release as an LLM engineering portfolio project.

Highlights:

- Local agent orchestration over ` + "`codex app-server`" + `.
- Telegram UX with Plan Mode, Details, ` + "`Tools file`" + `, and ` + "`Get full log`" + `.
- Durable local state and live E2E validation.

Useful link: [OpenAI Codex docs](https://developers.openai.com/codex/).`
	if commandErr != nil {
		final = fmt.Sprintf("The public demo ran, but the local test suite needs attention before v0.1.\n\n`go test ./...` failed with: `%v`", commandErr)
	}
	sendRendered(tgformat.RenderMarkdownWithHeader(demoHeader("Final")+"\nStatus: completed", final), &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{{
			{Text: "Details", CallbackData: "demo_details"},
			{Text: "Get full log", CallbackData: "demo_full_log"},
		}},
	})
	t.Logf("sent %d demo messages; keep=%v", len(sent), keepMessages)
}

func demoHeader(kind string) string {
	return fmt.Sprintf("🟦 [codex-tg] [Public v0.1 release review] [T:demo] [R:reloc] [%s]", kind)
}

func htmlCode(language, content string) string {
	if strings.TrimSpace(content) == "" {
		content = "No output."
	}
	if language != "" {
		return fmt.Sprintf(`<pre><code class="language-%s">%s</code></pre>`, html.EscapeString(language), html.EscapeString(content))
	}
	return fmt.Sprintf("<pre><code>%s</code></pre>", html.EscapeString(content))
}

func runDemoGoTest(ctx context.Context) (string, error) {
	root, err := findRepoRoot()
	if err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, "go", "test", "./...")
	command.Dir = root
	output, err := command.CombinedOutput()
	return string(output), err
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

func trimDemoOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "go test ./... completed successfully."
	}
	const max = 1400
	if len(output) <= max {
		return output
	}
	output = output[len(output)-max:]
	if index := strings.Index(output, "\n"); index >= 0 && index+1 < len(output) {
		output = output[index+1:]
	}
	return "[tail]\n" + output
}
