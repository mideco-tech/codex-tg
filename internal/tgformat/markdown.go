package tgformat

import (
	"fmt"
	"strings"

	tgmd "github.com/eekstunt/telegramify-markdown-go"

	"github.com/mideco-tech/codex-tg/internal/model"
)

const TelegramMessageLimit = 4096

type Segment struct {
	Plain    string
	Markdown string
}

func Plain(text string) Segment {
	return Segment{Plain: text}
}

func Markdown(text string) Segment {
	return Segment{Markdown: text}
}

func RenderMarkdownWithHeader(header, markdown string) []model.RenderedMessage {
	return RenderSegments([]Segment{
		Plain(strings.TrimRight(header, "\n")),
		Plain("\n\n"),
		Markdown(strings.TrimSpace(markdown)),
	}, TelegramMessageLimit)
}

func RenderSegments(segments []Segment, maxLen int) []model.RenderedMessage {
	if maxLen <= 0 {
		maxLen = TelegramMessageLimit
	}
	text := strings.Builder{}
	entities := []tgmd.Entity{}
	for _, segment := range segments {
		if segment.Plain != "" {
			text.WriteString(segment.Plain)
		}
		if strings.TrimSpace(segment.Markdown) == "" {
			continue
		}
		offset := tgmd.UTF16Len(text.String())
		converted := tgmd.Convert(segment.Markdown)
		text.WriteString(converted.Text)
		for _, entity := range converted.Entities {
			entity.Offset += offset
			entities = append(entities, entity)
		}
	}
	message := tgmd.Message{
		Text:     text.String(),
		Entities: entities,
	}
	chunks := tgmd.Split(message, maxLen)
	out := make([]model.RenderedMessage, 0, len(chunks))
	for _, chunk := range chunks {
		out = append(out, model.RenderedMessage{
			Text:     chunk.Text,
			Entities: convertEntities(chunk.Entities),
		})
	}
	if len(out) == 0 {
		return []model.RenderedMessage{{Text: " "}}
	}
	return out
}

func HashRendered(message model.RenderedMessage) string {
	parts := []string{message.Text}
	for _, entity := range message.Entities {
		parts = append(parts, fmt.Sprintf("%s:%d:%d:%s:%s", entity.Type, entity.Offset, entity.Length, entity.URL, entity.Language))
	}
	return strings.Join(parts, "\x00")
}

func convertEntities(entities []tgmd.Entity) []model.MessageEntity {
	out := make([]model.MessageEntity, 0, len(entities))
	for _, entity := range entities {
		out = append(out, model.MessageEntity{
			Type:     string(entity.Type),
			Offset:   entity.Offset,
			Length:   entity.Length,
			URL:      entity.URL,
			Language: entity.Language,
		})
	}
	return out
}
