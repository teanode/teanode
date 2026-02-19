// Package gw provides the core Gateway interface that mediates between
// the HTTP/WebSocket layer (v1api) and domain subsystems (agents, browser,
// terminal, scheduler, summarizer, media).
package gw

import (
	"context"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/integrations/browsers/relaybrowser"
	"github.com/teanode/teanode/internal/integrations/terminals"
	"github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/sessions"
	"github.com/teanode/teanode/internal/web"
)

var log = logging.MustGetLogger("gateway")

// SendMessageParameters are the parameters for sending a message through the gateway.
type SendMessageParameters struct {
	AgentID        string
	ConversationID string // empty = auto-create
	Message        string
	Model          string
	OriginID       string                    // opaque client-generated ID echoed in broadcasts so the sender can filter its own messages
	Origin         string                    // source of the message (e.g. "webui", "discord", "telegram"); empty for automated sources like the scheduler
	Attachments    []conversations.Attachment // file attachments
}

// RunHandle is returned by SendMessage and allows the caller to wait for completion.
type RunHandle struct {
	RunID          string
	ConversationID string
	Done           <-chan struct{}
	Outcome        func() *RunOutcome // safe to call after <-Done
}

// RunOutcome holds the final result of a completed run.
type RunOutcome struct {
	Response   string
	Model      string
	StopReason string
	Usage      *conversations.Usage
	Error      error
}

// EventType identifies the kind of broadcast event.
type EventType string

const (
	// EventTypeConversation is emitted for each stage of an agent run: user_message, queued,
	// delta (streaming text), tool_call, tool_result, final, error, and aborted.
	EventTypeConversation EventType = "conversation"

	// EventTypeConversations signals that the conversation list has changed (created, deleted, or summarized).
	EventTypeConversations EventType = "conversations"

	// EventTypeActiveAgent is emitted when the system-wide active agent changes.
	EventTypeActiveAgent EventType = "activeAgent"

	// EventTypeActiveConversation is emitted when the active conversation for an agent changes.
	EventTypeActiveConversation EventType = "activeConversation"

	// EventTypeJobs signals that the scheduled jobs list has changed.
	EventTypeJobs EventType = "jobs"
)

// Subscriber receives broadcast events from the gateway.
type Subscriber interface {
	OnEvent(eventType EventType, payload interface{})
}

// LifecycleAction identifies a gateway lifecycle request.
type LifecycleAction int

const (
	LifecycleShutdown LifecycleAction = iota
	LifecycleRestart
)

// Gateway is the main domain interface for the TeaNode gateway.
type Gateway interface {
	// Configuration access
	Config() *configs.Config
	SetConfig(configuration *configs.Config)
	SecurityConfig() *configs.SecurityConfig
	SetSecurityConfig(securityConfig *configs.SecurityConfig)

	// Subsystem access
	AgentRegistry() *agents.AgentRegistry
	Scheduler() *jobs.Scheduler
	Summarizer() *agents.Summarizer
	MediaStore() *media.Store
	BrowserRelay() *relaybrowser.Relay
	TerminalRelay() *terminals.Relay

	// Domain operations
	ResolveRunner(agentId string) *agents.Runner
	LoadModels(ctx context.Context) (map[string][]providers.ModelInfo, error)
	InvalidateModelsCache()

	// Active agent / conversation
	ActiveAgentID() string
	SetActiveAgent(agentId string) error
	ActiveConversationID(agentId string) string
	SetActiveConversation(agentId, conversationId string)
	SetActiveConversationIfUnset(agentId, conversationId string) bool
	NewConversation(agentId, model string) string

	// Centralized message sending and run management
	SendMessage(ctx context.Context, parameters SendMessageParameters, callerCallbacks *agents.RunCallbacks) *RunHandle
	AbortRun(runId string) bool
	GetActiveRun(conversationId string) string
	DeleteConversation(agentId, conversationId string) error

	// Event broadcasting via subscriber pattern
	Subscribe(subscriber Subscriber)
	Unsubscribe(subscriber Subscriber)
	Broadcast(eventType EventType, payload interface{})

	// Session store access
	SessionStore() *sessions.Store

	// Auth middleware for the HTTP server
	AuthMiddleware() web.Middleware

	// ListenAddress returns the host:port the server should bind to.
	ListenAddress() string

	// Lifecycle controls
	RequestLifecycle(action LifecycleAction)
	ScheduleLifecycle(action LifecycleAction)
	LifecycleChannel() <-chan LifecycleAction
}

// New creates a new Gateway.
func New(
	configuration *configs.Config,
	securityConfig *configs.SecurityConfig,
	agentRegistry *agents.AgentRegistry,
	browserRelay *relaybrowser.Relay,
	terminalRelay *terminals.Relay,
	scheduler *jobs.Scheduler,
	summarizer *agents.Summarizer,
	mediaStore *media.Store,
	sessionStore *sessions.Store,
) Gateway {
	return &gateway{
		config:           configuration,
		securityConfig:   securityConfig,
		agentRegistry:    agentRegistry,
		browserRelay:     browserRelay,
		terminalRelay:    terminalRelay,
		scheduler:        scheduler,
		summarizer:       summarizer,
		mediaStore:       mediaStore,
		sessionStore:     sessionStore,
		subscribers:      make(map[Subscriber]struct{}),
		activeRuns:       make(map[string]*activeRun),
		runIndex:         make(map[string]string),
		lifecycleChannel: make(chan LifecycleAction, 1),
	}
}
