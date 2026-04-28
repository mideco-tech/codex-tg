package model

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	BindingModeBound    = "bound"
	BindingModeObserver = "observer"

	PanelModePerRun = "per_run"
	PanelModeStable = "stable"

	PanelSourceExplicit       = "explicit"
	PanelSourceGlobalObserver = "global_observer"
	PanelSourceTelegramInput  = "telegram_input"

	PromptSourceServerRequest = "server_request"
	PromptSourceSyntheticPoll = "synthetic_poll"

	DetailItemUser       = "user"
	DetailItemCommentary = "commentary"
	DetailItemPlan       = "plan"
	DetailItemTool       = "tool"
	DetailItemOutput     = "output"
	DetailItemFinal      = "final"

	DeliveryStatusPending    = "pending"
	DeliveryStatusProcessing = "processing"
	DeliveryStatusDelivered  = "delivered"
	DeliveryStatusRetry      = "retry"
	DeliveryStatusDead       = "dead"

	CallbackStatusActive  = "active"
	CallbackStatusExpired = "expired"

	DeliveryModeSendMessage  = "send_message"
	DeliveryModeEditMessage  = "edit_message"
	DeliveryModeSendDocument = "send_document"
)

type TimeString string

func NowString() TimeString {
	return TimeString(time.Now().UTC().Format(time.RFC3339Nano))
}

type Thread struct {
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	CWD             string          `json:"cwd,omitempty"`
	ProjectName     string          `json:"project_name"`
	DirectoryName   string          `json:"directory_name,omitempty"`
	UpdatedAt       int64           `json:"updated_at"`
	Status          string          `json:"status,omitempty"`
	LastPreview     string          `json:"last_preview,omitempty"`
	ActiveTurnID    string          `json:"active_turn_id,omitempty"`
	PreferredModel  string          `json:"preferred_model,omitempty"`
	PermissionsMode string          `json:"permissions_mode,omitempty"`
	Archived        bool            `json:"archived"`
	Raw             json.RawMessage `json:"raw"`
}

func (t Thread) ShortID() string {
	if len(t.ID) <= 8 {
		return t.ID
	}
	return t.ID[:8]
}

func (t Thread) Label() string {
	title := strings.TrimSpace(t.Title)
	if title == "" {
		title = t.ShortID()
	}
	project := strings.TrimSpace(t.ProjectName)
	if project == "" {
		return title
	}
	return fmt.Sprintf("[%s] %s", project, title)
}

type ThreadSnapshotState struct {
	ThreadUpdatedAt      int64           `json:"thread_updated_at"`
	LastSeenThreadStatus string          `json:"last_seen_thread_status,omitempty"`
	LastSeenTurnID       string          `json:"last_seen_turn_id,omitempty"`
	LastSeenTurnStatus   string          `json:"last_seen_turn_status,omitempty"`
	LastProgressFP       string          `json:"last_progress_fp,omitempty"`
	LastProgressSentAt   TimeString      `json:"last_progress_sent_at,omitempty"`
	LastFinalFP          string          `json:"last_final_fp,omitempty"`
	LastCompletionFP     string          `json:"last_completion_fp,omitempty"`
	LastApprovalFP       string          `json:"last_approval_fp,omitempty"`
	LastReplyFP          string          `json:"last_reply_fp,omitempty"`
	LastFinalNoticeFP    string          `json:"last_final_notice_fp,omitempty"`
	LastToolDocumentFP   string          `json:"last_tool_document_fp,omitempty"`
	LastRichLiveEventAt  TimeString      `json:"last_rich_live_event_at,omitempty"`
	LastPollAt           TimeString      `json:"last_poll_at,omitempty"`
	NextPollAfter        TimeString      `json:"next_poll_after,omitempty"`
	CompactJSON          json.RawMessage `json:"compact_json,omitempty"`
}

type ThreadBinding struct {
	ChatKey   string
	ChatID    int64
	TopicID   int64
	ThreadID  string
	Mode      string
	CreatedAt TimeString
	UpdatedAt TimeString
}

type ObserverTarget struct {
	ChatKey   string
	ChatID    int64
	TopicID   int64
	Enabled   bool
	CreatedAt TimeString
	UpdatedAt TimeString
}

type CallbackRoute struct {
	Token             string
	Action            string
	ThreadID          string
	TurnID            string
	RequestID         string
	TelegramMessageID int64
	Status            string
	ExpiresAt         string
	PayloadJSON       string
	CreatedAt         TimeString
}

type PendingApproval struct {
	RequestID         string
	ThreadID          string
	TurnID            string
	ItemID            string
	PromptKind        string
	Question          string
	Status            string
	TelegramMessageID int64
	PayloadJSON       string
	UpdatedAt         TimeString
}

type PlanPrompt struct {
	PromptID    string   `json:"prompt_id"`
	Source      string   `json:"source"`
	ThreadID    string   `json:"thread_id"`
	TurnID      string   `json:"turn_id,omitempty"`
	ItemID      string   `json:"item_id,omitempty"`
	RequestID   string   `json:"request_id,omitempty"`
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty"`
	Fingerprint string   `json:"fingerprint"`
	Status      string   `json:"status"`
}

type MessageRoute struct {
	ChatID    int64
	TopicID   int64
	MessageID int64
	ThreadID  string
	TurnID    string
	ItemID    string
	EventID   string
	CreatedAt TimeString
}

type DeliveryPayload struct {
	Mode      string         `json:"mode,omitempty"`
	Text      string         `json:"text,omitempty"`
	ThreadID  string         `json:"thread_id,omitempty"`
	TurnID    string         `json:"turn_id,omitempty"`
	ItemID    string         `json:"item_id,omitempty"`
	EventID   string         `json:"event_id,omitempty"`
	MessageID int64          `json:"message_id,omitempty"`
	FileName  string         `json:"file_name,omitempty"`
	FilePath  string         `json:"file_path,omitempty"`
	Caption   string         `json:"caption,omitempty"`
	PanelID   int64          `json:"panel_id,omitempty"`
	PanelRole string         `json:"panel_role,omitempty"`
	Buttons   [][]ButtonSpec `json:"buttons,omitempty"`
}

type DeliveryQueueItem struct {
	ID          int64
	EventID     string
	ChatKey     string
	ChatID      int64
	TopicID     int64
	ThreadID    string
	Kind        string
	Status      string
	RetryCount  int
	AvailableAt TimeString
	LastError   string
	PayloadJSON string
	CreatedAt   TimeString
	UpdatedAt   TimeString
}

type DeliveryAttempt struct {
	ID        int64
	QueueID   int64
	AttemptNo int
	Status    string
	ErrorText string
	CreatedAt TimeString
}

type ButtonSpec struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

type MessageEntity struct {
	Type     string `json:"type"`
	Offset   int    `json:"offset"`
	Length   int    `json:"length"`
	URL      string `json:"url,omitempty"`
	Language string `json:"language,omitempty"`
}

type RenderedMessage struct {
	Text     string          `json:"text"`
	Entities []MessageEntity `json:"entities,omitempty"`
}

type DetailItem struct {
	ID              string `json:"id,omitempty"`
	Kind            string `json:"kind"`
	Phase           string `json:"phase,omitempty"`
	Text            string `json:"text,omitempty"`
	Label           string `json:"label,omitempty"`
	Status          string `json:"status,omitempty"`
	Output          string `json:"output,omitempty"`
	FP              string `json:"fp,omitempty"`
	CommentaryIndex int    `json:"commentary_index,omitempty"`
}

type DetailsViewState struct {
	Page            int  `json:"page"`
	ToolMode        bool `json:"tool_mode"`
	CommentaryIndex int  `json:"commentary_index,omitempty"`
}

type ObserverEvent struct {
	EventID       string `json:"event_id"`
	Kind          string `json:"kind"`
	ThreadID      string `json:"thread_id"`
	ProjectName   string `json:"project_name"`
	ThreadTitle   string `json:"thread_title"`
	Text          string `json:"text"`
	Status        string `json:"status,omitempty"`
	TurnID        string `json:"turn_id,omitempty"`
	ItemID        string `json:"item_id,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	NeedsReply    bool   `json:"needs_reply,omitempty"`
	NeedsApproval bool   `json:"needs_approval,omitempty"`
}

type ChatContext struct {
	Mode            string
	Binding         *ThreadBinding
	ObserverEnabled bool
	ObserverTarget  *ObserverTarget
	Thread          *Thread
}

type ThreadPanel struct {
	ID                  int64
	ChatID              int64
	TopicID             int64
	ProjectName         string
	ThreadID            string
	SourceMode          string
	SummaryMessageID    int64
	ToolMessageID       int64
	OutputMessageID     int64
	CurrentTurnID       string
	Status              string
	ArchiveEnabled      bool
	LastSummaryHash     string
	LastToolHash        string
	LastOutputHash      string
	LastFinalNoticeFP   string
	RunNoticeMessageID  int64
	LastRunNoticeFP     string
	UserMessageID       int64
	LastUserNoticeFP    string
	PlanPromptMessageID int64
	LastPlanPromptFP    string
	DetailsViewJSON     string
	LastFinalCardHash   string
	IsCurrent           bool
	CreatedAt           TimeString
	UpdatedAt           TimeString
}

type SteerState struct {
	ChatKey   string
	ChatID    int64
	TopicID   int64
	ThreadID  string
	TurnID    string
	PanelID   int64
	ExpiresAt TimeString
	CreatedAt TimeString
	UpdatedAt TimeString
}

type RouteSource string

const (
	RouteSourceExplicit RouteSource = "explicit"
	RouteSourceReply    RouteSource = "reply"
	RouteSourceSteer    RouteSource = "steer"
	RouteSourceBinding  RouteSource = "binding"
	RouteSourceNone     RouteSource = "none"
)

type RouteDecision struct {
	ThreadID  string
	TurnID    string
	RequestID string
	Source    RouteSource
}

func ChatKey(chatID, topicID int64) string {
	return fmt.Sprintf("%d:%d", chatID, topicID)
}

func NormalizePath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}

func ProjectNameFromCWD(cwd string) (projectName string, directoryName string) {
	normalized := NormalizePath(cwd)
	if normalized == "" {
		return "Shared/General", ""
	}
	slashed := strings.ToLower(filepath.ToSlash(normalized))
	if slashed == "c:/users/you/documents/codex" {
		return "Shared/General", "General"
	}
	dir := filepath.Base(normalized)
	if dir == "." || dir == string(filepath.Separator) || dir == "" {
		return "Shared/General", ""
	}
	return dir, dir
}

func MustJSON(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(payload)
}
