package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestBotEditMessageRejectsMultiChunkPayload(t *testing.T) {
	t.Parallel()

	bot := &Bot{client: NewClient("token")}
	err := bot.EditMessage(context.Background(), 42, 0, 77, strings.Repeat("x", telegramMessageLimit+10), nil)
	if err == nil {
		t.Fatal("EditMessage must reject multi-chunk payloads")
	}
}

func TestSanitizeTelegramLogErrorRedactsBotTokenURL(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf(`Post "https://api.telegram.org/bot123456789:AAF_secret-token/getUpdates": context deadline exceeded`)
	got := sanitizeTelegramLogError(err)
	if strings.Contains(got, "123456789:AAF_secret-token") {
		t.Fatalf("sanitizeTelegramLogError leaked token: %q", got)
	}
	if !strings.Contains(got, "bot<redacted>") {
		t.Fatalf("sanitizeTelegramLogError = %q, want redacted marker", got)
	}
}

func TestDefaultCommandsExposeNewChatMenuCommand(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	for _, command := range defaultCommands() {
		if seen[command.Command] {
			t.Fatalf("defaultCommands contains duplicate command %q", command.Command)
		}
		seen[command.Command] = true
	}
	for _, command := range []string{"newchat", "newthread"} {
		if !seen[command] {
			t.Fatalf("defaultCommands must expose /%s in the Telegram command menu", command)
		}
	}
}

func TestBotSendMessageChunksAndReturnsLastMessageID(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"chat":{"id":42,"type":"private"}}}`, 100+calls)
	}))
	defer server.Close()

	client := NewClient("token")
	client.baseURL = server.URL
	bot := &Bot{client: client}

	messageID, err := bot.SendMessage(context.Background(), 42, 0, strings.Repeat("line\n", telegramMessageLimit/4), nil)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if calls < 2 {
		t.Fatalf("calls = %d, want at least 2 chunked requests", calls)
	}
	if got, want := messageID, int64(100+calls); got != want {
		t.Fatalf("messageID = %d, want %d", got, want)
	}
}

func TestBotSendRenderedMessagesFallsBackToPlainEntities(t *testing.T) {
	t.Parallel()

	var calls int
	var second map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if calls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: entities are invalid"}`))
			return
		}
		if err := json.Unmarshal(body, &second); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":202,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer server.Close()

	client := NewClient("token")
	client.baseURL = server.URL
	bot := &Bot{client: client}
	ids, err := bot.SendRenderedMessages(context.Background(), 42, 0, []model.RenderedMessage{{
		Text:     "formatted",
		Entities: []model.MessageEntity{{Type: "code", Offset: 0, Length: 9}},
	}}, nil)
	if err != nil {
		t.Fatalf("SendRenderedMessages failed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(ids) != 1 || ids[0] != 202 {
		t.Fatalf("ids = %#v, want [202]", ids)
	}
	if _, ok := second["entities"]; ok {
		t.Fatalf("fallback entities = %#v, want omitted", second["entities"])
	}
	if _, ok := second["parse_mode"]; ok {
		t.Fatalf("fallback parse_mode = %#v, want omitted", second["parse_mode"])
	}
}

func TestBotSendDocumentReturnsTelegramMessageID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":555,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer server.Close()

	client := NewClient("token")
	client.baseURL = server.URL
	bot := &Bot{client: client}

	dir := t.TempDir()
	path := filepath.Join(dir, "trace.log")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(trace.log) failed: %v", err)
	}

	messageID, err := bot.SendDocument(context.Background(), 42, 0, "trace.log", path, "trace")
	if err != nil {
		t.Fatalf("SendDocument failed: %v", err)
	}
	if got, want := messageID, int64(555); got != want {
		t.Fatalf("messageID = %d, want %d", got, want)
	}
}
