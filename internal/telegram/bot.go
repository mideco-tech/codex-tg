package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/mideco-tech/codex-tg/internal/config"
	"github.com/mideco-tech/codex-tg/internal/daemon"
	"github.com/mideco-tech/codex-tg/internal/model"
)

const telegramMessageLimit = 4096

var telegramBotTokenURLPattern = regexp.MustCompile(`bot[0-9]+:[A-Za-z0-9_-]+`)

type Bot struct {
	cfg     config.Config
	client  *Client
	service *daemon.Service
	logger  *log.Logger
	me      *User
}

type Document struct {
	Name        string
	ContentType string
	Data        []byte
	Caption     string
}

func NewBot(cfg config.Config, service *daemon.Service, logger *log.Logger) (*Bot, error) {
	if strings.TrimSpace(cfg.TelegramBotToken) == "" {
		return nil, errors.New("CTR_GO_TELEGRAM_BOT_TOKEN is not configured")
	}
	if logger == nil {
		logger = log.Default()
	}
	return &Bot{
		cfg:     cfg,
		client:  NewClient(cfg.TelegramBotToken),
		service: service,
		logger:  logger,
	}, nil
}

func (b *Bot) Start(ctx context.Context) error {
	startCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	me, err := b.client.GetMe(startCtx)
	if err != nil {
		return err
	}
	b.me = me
	if err := b.client.SetMyCommands(startCtx, defaultCommands()); err != nil {
		b.logger.Printf("telegram setMyCommands failed: %s", sanitizeTelegramLogError(err))
	}
	b.logger.Printf("telegram bot ready: @%s", me.Username)
	return nil
}

func (b *Bot) Run(ctx context.Context) error {
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		pollCtx, cancel := context.WithTimeout(ctx, 65*time.Second)
		updates, err := b.client.GetUpdates(pollCtx, offset, 30)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			b.logger.Printf("telegram getUpdates failed: %s", sanitizeTelegramLogError(err))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
				continue
			}
		}
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			if err := b.handleUpdate(ctx, update); err != nil {
				b.logger.Printf("telegram update %d failed: %s", update.UpdateID, sanitizeTelegramLogError(err))
			}
		}
	}
}

func sanitizeTelegramLogError(err error) string {
	if err == nil {
		return ""
	}
	return telegramBotTokenURLPattern.ReplaceAllString(err.Error(), "bot<redacted>")
}

func (b *Bot) SendMessage(ctx context.Context, chatID, topicID int64, text string, buttons [][]model.ButtonSpec, options model.SendOptions) (int64, error) {
	chunks := splitText(strings.TrimSpace(text), telegramMessageLimit)
	if len(chunks) == 0 {
		chunks = []string{" "}
	}
	return b.sendTextChunks(ctx, chatID, topicID, chunks, buttons, options)
}

func (b *Bot) SendRenderedMessages(ctx context.Context, chatID, topicID int64, messages []model.RenderedMessage, buttons [][]model.ButtonSpec, options model.SendOptions) ([]int64, error) {
	if len(messages) == 0 {
		messages = []model.RenderedMessage{{Text: " "}}
	}
	ids := make([]int64, 0, len(messages))
	for index, rendered := range messages {
		if strings.TrimSpace(rendered.Text) == "" {
			rendered.Text = " "
			rendered.Entities = nil
		}
		var markup *InlineKeyboardMarkup
		if index == len(messages)-1 {
			markup = toInlineKeyboard(buttons)
		}
		sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		message, err := b.client.SendRenderedMessage(sendCtx, chatID, topicID, rendered, markup, options)
		cancel()
		if err != nil {
			rendered.Entities = nil
			sendCtx, cancel = context.WithTimeout(ctx, 20*time.Second)
			message, err = b.client.SendRenderedMessage(sendCtx, chatID, topicID, rendered, markup, options)
			cancel()
			if err != nil {
				return nil, err
			}
		}
		if message != nil {
			ids = append(ids, message.MessageID)
		}
	}
	return ids, nil
}

func (b *Bot) EditMessage(ctx context.Context, chatID, topicID, messageID int64, text string, buttons [][]model.ButtonSpec) error {
	chunks := splitText(strings.TrimSpace(text), telegramMessageLimit)
	if len(chunks) != 1 {
		return fmt.Errorf("telegram editMessageText requires a single text chunk, got %d", len(chunks))
	}
	editCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	_, err := b.client.EditMessageText(editCtx, chatID, messageID, chunks[0], toInlineKeyboard(buttons))
	cancel()
	return err
}

func (b *Bot) EditRenderedMessage(ctx context.Context, chatID, topicID, messageID int64, rendered model.RenderedMessage, buttons [][]model.ButtonSpec) error {
	if strings.TrimSpace(rendered.Text) == "" {
		rendered.Text = " "
		rendered.Entities = nil
	}
	editCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	_, err := b.client.EditRenderedMessageText(editCtx, chatID, messageID, rendered, toInlineKeyboard(buttons))
	cancel()
	if err == nil {
		return nil
	}
	rendered.Entities = nil
	editCtx, cancel = context.WithTimeout(ctx, 20*time.Second)
	_, fallbackErr := b.client.EditRenderedMessageText(editCtx, chatID, messageID, rendered, toInlineKeyboard(buttons))
	cancel()
	if fallbackErr != nil {
		return errors.Join(err, fallbackErr)
	}
	return nil
}

func (b *Bot) SendDocument(ctx context.Context, chatID, topicID int64, fileName, filePath, caption string, options model.SendOptions) (int64, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}
	return b.SendDocumentData(ctx, chatID, topicID, fileName, data, caption, options)
}

func (b *Bot) SendDocumentData(ctx context.Context, chatID, topicID int64, fileName string, data []byte, caption string, options model.SendOptions) (int64, error) {
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	message, err := b.client.SendDocument(sendCtx, chatID, topicID, DocumentFile{
		Name:        fileName,
		ContentType: "application/octet-stream",
		Data:        data,
	}, strings.TrimSpace(caption), nil, options)
	cancel()
	if err != nil {
		return 0, err
	}
	if message == nil {
		return 0, nil
	}
	return message.MessageID, nil
}

func (b *Bot) DeleteMessage(ctx context.Context, chatID, topicID, messageID int64) error {
	deleteCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return b.client.DeleteMessage(deleteCtx, chatID, messageID)
}

func (b *Bot) handleUpdate(ctx context.Context, update Update) error {
	switch {
	case update.CallbackQuery != nil:
		return b.handleCallback(ctx, *update.CallbackQuery)
	case update.Message != nil:
		return b.handleMessage(ctx, *update.Message)
	case update.EditedMessage != nil:
		return b.handleMessage(ctx, *update.EditedMessage)
	default:
		return nil
	}
}

func (b *Bot) handleMessage(ctx context.Context, message Message) error {
	if message.From == nil || strings.TrimSpace(message.Text) == "" {
		return nil
	}
	replyTo := int64(0)
	if message.ReplyToMessage != nil {
		replyTo = message.ReplyToMessage.MessageID
	}
	response, err := b.service.HandleMessage(ctx, message.Chat.ID, message.MessageThreadID, message.From.ID, message.Text, replyTo)
	if err != nil {
		return b.sendFailureMessage(ctx, message.Chat.ID, message.MessageThreadID, err)
	}
	return b.deliverDirectResponse(ctx, message.Chat.ID, message.MessageThreadID, response)
}

func (b *Bot) handleCallback(ctx context.Context, callback CallbackQuery) error {
	if callback.From == nil {
		return nil
	}
	chatID := int64(0)
	topicID := int64(0)
	if callback.Message != nil {
		chatID = callback.Message.Chat.ID
		topicID = callback.Message.MessageThreadID
	}
	messageID := int64(0)
	if callback.Message != nil {
		messageID = callback.Message.MessageID
	}
	response, err := b.service.HandleCallback(ctx, chatID, topicID, messageID, callback.From.ID, callback.Data)
	answerText := ""
	if err != nil {
		answerText = "Request failed."
	} else if response == nil {
		answerText = "Ignored."
	} else if strings.TrimSpace(response.CallbackText) != "" {
		answerText = response.CallbackText
	}
	answerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	_ = b.client.AnswerCallbackQuery(answerCtx, callback.ID, answerText, false)
	cancel()
	if err != nil {
		return b.sendFailureMessage(ctx, chatID, topicID, err)
	}
	return b.deliverDirectResponse(ctx, chatID, topicID, response)
}

func (b *Bot) deliverDirectResponse(ctx context.Context, chatID, topicID int64, response *daemon.DirectResponse) error {
	if response == nil || strings.TrimSpace(response.Text) == "" {
		return nil
	}
	messageID, err := b.SendMessage(ctx, chatID, topicID, response.Text, response.Buttons, model.SendOptions{Silent: true})
	if err != nil {
		return err
	}
	return b.service.RegisterDirectDelivery(ctx, chatID, topicID, messageID, response)
}

func (b *Bot) sendFailureMessage(ctx context.Context, chatID, topicID int64, cause error) error {
	if chatID == 0 {
		if cause != nil {
			b.logger.Printf("telegram handler error without chat context: %v", cause)
		}
		return nil
	}
	text := "Request failed inside the local Go bridge. Try /repair or /status."
	if cause != nil {
		b.logger.Printf("telegram handler error: %v", cause)
	}
	_, err := b.SendMessage(ctx, chatID, topicID, text, nil, model.SendOptions{Silent: true})
	if err != nil {
		if cause != nil {
			return errors.Join(cause, err)
		}
		return err
	}
	return nil
}

func defaultCommands() []BotCommand {
	return []BotCommand{
		{Command: "start", Description: "Bridge status and quick help"},
		{Command: "help", Description: "Command list"},
		{Command: "status", Description: "Daemon and routing status"},
		{Command: "threads", Description: "List cached Codex threads"},
		{Command: "projects", Description: "List cached projects"},
		{Command: "newchat", Description: "Start a new Codex UI Chat"},
		{Command: "newthread", Description: "Start without project selection"},
		{Command: "show", Description: "Show a thread card"},
		{Command: "bind", Description: "Bind this chat to a thread"},
		{Command: "reply", Description: "Send input to a thread"},
		{Command: "default", Description: "Run in Default Mode"},
		{Command: "plan", Description: "Start Plan Mode in a thread"},
		{Command: "settings", Description: "Show Codex model settings"},
		{Command: "model", Description: "Choose the Codex model"},
		{Command: "effort", Description: "Choose reasoning effort"},
		{Command: "context", Description: "Show current routing context"},
		{Command: "observe", Description: "Enable or disable observer mode"},
		{Command: "panelmode", Description: "Switch trio lifecycle mode"},
		{Command: "repair", Description: "Restart app-server sessions"},
		{Command: "stop", Description: "Interrupt the active turn"},
		{Command: "approve", Description: "Approve a pending request"},
		{Command: "deny", Description: "Decline a pending request"},
	}
}

func toInlineKeyboard(rows [][]model.ButtonSpec) *InlineKeyboardMarkup {
	if len(rows) == 0 {
		return nil
	}
	keyboard := make([][]InlineKeyboardButton, 0, len(rows))
	for _, row := range rows {
		buttonRow := make([]InlineKeyboardButton, 0, len(row))
		for _, button := range row {
			if strings.TrimSpace(button.Text) == "" {
				continue
			}
			buttonRow = append(buttonRow, InlineKeyboardButton{
				Text:         button.Text,
				CallbackData: button.CallbackData,
			})
		}
		if len(buttonRow) > 0 {
			keyboard = append(keyboard, buttonRow)
		}
	}
	if len(keyboard) == 0 {
		return nil
	}
	return &InlineKeyboardMarkup{InlineKeyboard: keyboard}
}

func (b *Bot) sendTextChunks(ctx context.Context, chatID, topicID int64, chunks []string, buttons [][]model.ButtonSpec, options model.SendOptions) (int64, error) {
	var messageID int64
	for index, chunk := range chunks {
		var markup *InlineKeyboardMarkup
		if index == len(chunks)-1 {
			markup = toInlineKeyboard(buttons)
		}
		sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		message, err := b.client.SendMessage(sendCtx, chatID, topicID, chunk, markup, options)
		cancel()
		if err != nil {
			return 0, err
		}
		if message != nil {
			messageID = message.MessageID
		}
	}
	return messageID, nil
}

func splitText(text string, limit int) []string {
	if limit <= 0 {
		return []string{text}
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if len(text) <= limit {
		return []string{text}
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	current := strings.Builder{}
	flush := func() {
		if current.Len() == 0 {
			return
		}
		out = append(out, strings.TrimSpace(current.String()))
		current.Reset()
	}
	for _, line := range lines {
		line = strings.TrimRight(line, " ")
		candidate := line
		if current.Len() > 0 {
			candidate = current.String() + "\n" + line
		}
		if len(candidate) <= limit {
			if current.Len() > 0 {
				current.WriteByte('\n')
			}
			current.WriteString(line)
			continue
		}
		flush()
		for len(line) > limit {
			out = append(out, strings.TrimSpace(line[:limit]))
			line = line[limit:]
		}
		if line != "" {
			current.WriteString(line)
		}
	}
	flush()
	if len(out) == 0 {
		return []string{text}
	}
	return out
}

func (b *Bot) String() string {
	if b.me == nil {
		return "telegram bot"
	}
	return fmt.Sprintf("@%s", b.me.Username)
}
