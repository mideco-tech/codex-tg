package tgformat

import (
	"strings"
	"testing"

	"codex-telegram-remote-go/internal/model"
)

func TestRenderMarkdownWithHeaderKeepsHeaderPlainAndConvertsCodeFence(t *testing.T) {
	t.Parallel()

	messages := RenderMarkdownWithHeader("[Final] [Project: Codex] [Thread: Найти *Swagger* [Stellar]]\nStatus: completed", "Run `rg`:\n\n```bash\nrg -n 'Authorization' stellar_ws.txt\n```\n\n- done")
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	message := messages[0]
	if !strings.Contains(message.Text, "[Thread: Найти *Swagger* [Stellar]]") {
		t.Fatalf("header was not preserved as plain text: %q", message.Text)
	}
	if strings.Contains(message.Text, "```") {
		t.Fatalf("rendered text still contains raw markdown fence: %q", message.Text)
	}
	if !hasEntity(message.Entities, "code", "") {
		t.Fatalf("entities = %#v, want inline code entity", message.Entities)
	}
	if !hasEntity(message.Entities, "pre", "bash") {
		t.Fatalf("entities = %#v, want bash pre entity", message.Entities)
	}
}

func TestRenderSegmentsSplitsLongMarkdown(t *testing.T) {
	t.Parallel()

	messages := RenderSegments([]Segment{
		Plain("[Final]\n\n"),
		Markdown(strings.Repeat("line with `code`\n", 500)),
	}, 512)
	if len(messages) < 2 {
		t.Fatalf("len(messages) = %d, want split messages", len(messages))
	}
	for _, message := range messages {
		if len(message.Text) == 0 {
			t.Fatal("split message text must not be empty")
		}
	}
}

func hasEntity(entities []model.MessageEntity, entityType, language string) bool {
	for _, entity := range entities {
		if entity.Type != entityType {
			continue
		}
		if language == "" || entity.Language == language {
			return true
		}
	}
	return false
}
