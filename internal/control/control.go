package control

import "context"

type Event struct {
	Channel string         `json:"channel"`
	Method  string         `json:"method,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
	ID      any            `json:"id,omitempty"`
}

type TurnStartOptions struct {
	CollaborationMode string
	Model             string
	ReasoningEffort   string
}

type ModelOption struct {
	ID                       string
	DisplayName              string
	Description              string
	DefaultReasoningEffort   string
	SupportedReasoningEffort []string
	IsDefault                bool
	Hidden                   bool
}

type CollaborationModeOption struct {
	Name            string
	Mode            string
	Model           string
	ReasoningEffort string
}

type NotificationSeverity string

const (
	NotificationUrgent NotificationSeverity = "urgent"
	NotificationNormal NotificationSeverity = "normal"
	NotificationSilent NotificationSeverity = "silent"
	NotificationDigest NotificationSeverity = "digest"
)

type Lifecycle interface {
	Start(ctx context.Context) error
	Close() error
}

type EventSource interface {
	Subscribe() <-chan Event
}

type Threads interface {
	ThreadList(ctx context.Context, limit int, cursor string) (map[string]any, error)
	ThreadRead(ctx context.Context, threadID string, includeTurns bool) (map[string]any, error)
	ThreadResume(ctx context.Context, threadID, cwd string) (map[string]any, error)
	ThreadStart(ctx context.Context, cwd string) (map[string]any, error)
}

type ThreadAdmin interface {
	ThreadFork(ctx context.Context, threadID, cwd string) (map[string]any, error)
	ThreadSetName(ctx context.Context, threadID, name string) (map[string]any, error)
	ThreadArchive(ctx context.Context, threadID string) (map[string]any, error)
	ThreadUnarchive(ctx context.Context, threadID string) (map[string]any, error)
	ThreadCompactStart(ctx context.Context, threadID string) (map[string]any, error)
	ThreadRollback(ctx context.Context, threadID string, numTurns int) (map[string]any, error)
}

type Turns interface {
	TurnStart(ctx context.Context, threadID, message, cwd string, options TurnStartOptions) (map[string]any, error)
	TurnSteer(ctx context.Context, threadID, turnID, message string) (map[string]any, error)
	TurnInterrupt(ctx context.Context, threadID, turnID string) error
}

type ServerRequests interface {
	RespondServerRequest(ctx context.Context, requestID string, result map[string]any) error
}

type Models interface {
	ModelList(ctx context.Context, includeHidden bool) ([]ModelOption, error)
	CollaborationModeList(ctx context.Context) ([]CollaborationModeOption, error)
}

type Ecosystem interface {
	SkillsList(ctx context.Context, cwds []string, forceReload bool) (map[string]any, error)
	PluginSkillRead(ctx context.Context, remoteMarketplaceName, remotePluginID, skillName string) (map[string]any, error)
	HooksList(ctx context.Context, cwds []string) (map[string]any, error)
	MCPServerStatusList(ctx context.Context, limit int, cursor string, detail bool) (map[string]any, error)
	AppList(ctx context.Context, limit int, cursor, threadID string, forceRefetch bool) (map[string]any, error)
	ConfigRead(ctx context.Context, cwd string, includeLayers bool) (map[string]any, error)
}

type Diagnostics interface {
	StderrTail() []string
}

type RuntimeSession interface {
	Lifecycle
	EventSource
	Threads
	Turns
	ServerRequests
	Models
	Diagnostics
}

type ControlPlane interface {
	RuntimeSession
	ThreadAdmin
	Ecosystem
}
