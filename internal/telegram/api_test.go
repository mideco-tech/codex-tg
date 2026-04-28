package telegram

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestClientEditMessageTextSendsExpectedJSON(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/editMessageText" {
			t.Fatalf("path = %q, want /editMessageText", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":77,"chat":{"id":42,"type":"private"},"text":"updated"}}`))
	}))
	defer server.Close()

	client := NewClient("token")
	client.baseURL = server.URL
	message, err := client.EditMessageText(context.Background(), 42, 77, "updated", &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Open", CallbackData: "cb-1"}}},
	})
	if err != nil {
		t.Fatalf("EditMessageText failed: %v", err)
	}
	if message == nil || message.MessageID != 77 {
		t.Fatalf("message = %#v, want message_id=77", message)
	}
	if got, want := captured["chat_id"], float64(42); got != want {
		t.Fatalf("chat_id = %#v, want %#v", got, want)
	}
	if got, want := captured["message_id"], float64(77); got != want {
		t.Fatalf("message_id = %#v, want %#v", got, want)
	}
	if got, want := captured["text"], "updated"; got != want {
		t.Fatalf("text = %#v, want %q", got, want)
	}
	if got, ok := captured["disable_web_page_preview"].(bool); !ok || !got {
		t.Fatalf("disable_web_page_preview = %#v, want true", captured["disable_web_page_preview"])
	}
	if _, ok := captured["parse_mode"]; ok {
		t.Fatalf("parse_mode = %#v, want omitted for plain text", captured["parse_mode"])
	}
}

func TestClientSendMessageUsesHTMLParseModeForMixedCodeBlock(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendMessage" {
			t.Fatalf("path = %q, want /sendMessage", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":78,"chat":{"id":42,"type":"private"},"text":"[Tool]"}}`))
	}))
	defer server.Close()

	client := NewClient("token")
	client.baseURL = server.URL
	message, err := client.SendMessage(context.Background(), 42, 0, `[Tool]
<pre><code class="language-powershell">Status: completed</code></pre>`, nil)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if message == nil || message.MessageID != 78 {
		t.Fatalf("message = %#v, want message_id=78", message)
	}
	if got, want := captured["parse_mode"], "HTML"; got != want {
		t.Fatalf("parse_mode = %#v, want %q", got, want)
	}
}

func TestClientDeleteMessageSendsExpectedJSON(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/deleteMessage" {
			t.Fatalf("path = %q, want /deleteMessage", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	client := NewClient("token")
	client.baseURL = server.URL
	if err := client.DeleteMessage(context.Background(), 42, 77); err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}
	if got, want := captured["chat_id"], float64(42); got != want {
		t.Fatalf("chat_id = %#v, want %#v", got, want)
	}
	if got, want := captured["message_id"], float64(77); got != want {
		t.Fatalf("message_id = %#v, want %#v", got, want)
	}
}

func TestClientSendRenderedMessageUsesEntitiesWithoutParseMode(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendMessage" {
			t.Fatalf("path = %q, want /sendMessage", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":80,"chat":{"id":42,"type":"private"},"text":"formatted"}}`))
	}))
	defer server.Close()

	client := NewClient("token")
	client.baseURL = server.URL
	message, err := client.SendRenderedMessage(context.Background(), 42, 0, model.RenderedMessage{
		Text: "formatted",
		Entities: []model.MessageEntity{{
			Type:     "pre",
			Offset:   0,
			Length:   9,
			Language: "bash",
		}},
	}, nil)
	if err != nil {
		t.Fatalf("SendRenderedMessage failed: %v", err)
	}
	if message == nil || message.MessageID != 80 {
		t.Fatalf("message = %#v, want message_id=80", message)
	}
	if _, ok := captured["parse_mode"]; ok {
		t.Fatalf("parse_mode = %#v, want omitted when entities are supplied", captured["parse_mode"])
	}
	entities, ok := captured["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one entity", captured["entities"])
	}
	entity, ok := entities[0].(map[string]any)
	if !ok {
		t.Fatalf("entity = %#v, want object", entities[0])
	}
	if got, want := entity["type"], "pre"; got != want {
		t.Fatalf("entity.type = %#v, want %q", got, want)
	}
	if got, want := entity["language"], "bash"; got != want {
		t.Fatalf("entity.language = %#v, want %q", got, want)
	}
}

func TestClientEditMessageTextUsesHTMLParseModeForMixedCodeBlock(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/editMessageText" {
			t.Fatalf("path = %q, want /editMessageText", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":79,"chat":{"id":42,"type":"private"},"text":"[Output]"}}`))
	}))
	defer server.Close()

	client := NewClient("token")
	client.baseURL = server.URL
	message, err := client.EditMessageText(context.Background(), 42, 79, `[Output]
<pre><code class="language-bash">hello</code></pre>`, nil)
	if err != nil {
		t.Fatalf("EditMessageText failed: %v", err)
	}
	if message == nil || message.MessageID != 79 {
		t.Fatalf("message = %#v, want message_id=79", message)
	}
	if got, want := captured["parse_mode"], "HTML"; got != want {
		t.Fatalf("parse_mode = %#v, want %q", got, want)
	}
}

func TestClientEditRenderedMessageTextUsesEntitiesWithoutParseMode(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/editMessageText" {
			t.Fatalf("path = %q, want /editMessageText", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":81,"chat":{"id":42,"type":"private"},"text":"updated"}}`))
	}))
	defer server.Close()

	client := NewClient("token")
	client.baseURL = server.URL
	message, err := client.EditRenderedMessageText(context.Background(), 42, 81, model.RenderedMessage{
		Text:     "updated",
		Entities: []model.MessageEntity{{Type: "code", Offset: 0, Length: 7}},
	}, nil)
	if err != nil {
		t.Fatalf("EditRenderedMessageText failed: %v", err)
	}
	if message == nil || message.MessageID != 81 {
		t.Fatalf("message = %#v, want message_id=81", message)
	}
	if _, ok := captured["parse_mode"]; ok {
		t.Fatalf("parse_mode = %#v, want omitted when entities are supplied", captured["parse_mode"])
	}
	if entities, ok := captured["entities"].([]any); !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one entity", captured["entities"])
	}
}

func TestClientSendDocumentUsesMultipartForm(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendDocument" {
			t.Fatalf("path = %q, want /sendDocument", r.URL.Path)
		}
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("ParseMediaType failed: %v", err)
		}
		if mediaType != "multipart/form-data" {
			t.Fatalf("mediaType = %q, want multipart/form-data", mediaType)
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		fields := map[string]string{}
		var documentName, documentBody, documentType string
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("NextPart failed: %v", err)
			}
			data, err := io.ReadAll(part)
			if err != nil {
				t.Fatalf("ReadAll(part) failed: %v", err)
			}
			if part.FormName() == "document" {
				documentName = part.FileName()
				documentBody = string(data)
				documentType = part.Header.Get("Content-Type")
				continue
			}
			fields[part.FormName()] = string(data)
		}
		if got, want := fields["chat_id"], "42"; got != want {
			t.Fatalf("chat_id = %q, want %q", got, want)
		}
		if got, want := fields["message_thread_id"], "9"; got != want {
			t.Fatalf("message_thread_id = %q, want %q", got, want)
		}
		if got, want := fields["caption"], "observer dump"; got != want {
			t.Fatalf("caption = %q, want %q", got, want)
		}
		if !strings.Contains(fields["reply_markup"], `"callback_data":"cb-1"`) {
			t.Fatalf("reply_markup = %q, want callback data", fields["reply_markup"])
		}
		if got, want := documentName, "observer.txt"; got != want {
			t.Fatalf("filename = %q, want %q", got, want)
		}
		if got, want := documentType, "text/plain"; got != want {
			t.Fatalf("content-type = %q, want %q", got, want)
		}
		if got, want := documentBody, "payload"; got != want {
			t.Fatalf("document body = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":501,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer server.Close()

	client := NewClient("token")
	client.baseURL = server.URL
	message, err := client.SendDocument(context.Background(), 42, 9, DocumentFile{
		Name:        "observer.txt",
		ContentType: "text/plain",
		Data:        []byte("payload"),
	}, "observer dump", &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Open", CallbackData: "cb-1"}}},
	})
	if err != nil {
		t.Fatalf("SendDocument failed: %v", err)
	}
	if message == nil || message.MessageID != 501 {
		t.Fatalf("message = %#v, want message_id=501", message)
	}
}
