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
	"github.com/teanode/teanode/internal/voice"
	"github.com/teanode/teanode/internal/web"
)

var log = logging.MustGetLogger("gateway")

// SendMessageParameters are the parameters for sending a message through the gateway.
type SendMessageParameters struct {
	UserContext        *UserContext
	AgentID            string
	ConversationID     string // empty = auto-create
	Message            string
	Model              string
	OriginID           string                     // opaque client-generated ID echoed in broadcasts so the sender can filter its own messages
	Origin             string                     // source of the message (e.g. "webui", "discord", "telegram"); empty for automated sources like the scheduler
	OriginSessionID    string                     // source session identifier (used for disconnect-aware notifications)
	Attachments        []conversations.Attachment // file attachments
	SystemPromptSuffix string                     // optional; appended to system prompt for this run only
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

	// EventTypeDefaultAgent is emitted when the system-wide default agent changes.
	EventTypeDefaultAgent EventType = "defaultAgent"

	// EventTypeDefaultConversation is emitted when the default conversation for an agent changes.
	EventTypeDefaultConversation EventType = "defaultConversation"

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
	ConversationStore(userId, agentId string) *conversations.Store
	ProviderRegistry() *providers.Registry
	LoadModels(ctx context.Context) (map[string][]providers.ModelInfo, error)
	InvalidateModelsCache()

	// Default agent / conversation
	DefaultAgentID() string
	DefaultAgentIDForUser(userId string) string
	SetDefaultAgent(agentId string) error
	SetDefaultAgentForUser(userId, agentId string) error
	DefaultConversationID(userId, agentId string) string
	SetDefaultConversation(userId, agentId, conversationId string)
	SetDefaultConversationIfUnset(userId, agentId, conversationId string) bool
	NewConversation(userId, agentId, model string) string

	// Centralized message sending and run management
	SendMessage(ctx context.Context, parameters SendMessageParameters, callerCallbacks *agents.RunCallbacks) *RunHandle
	AbortRun(runId string) bool
	GetActiveRun(conversationId string) string
	DeleteConversation(userId, agentId, conversationId string) error

	// Event broadcasting via subscriber pattern
	Subscribe(subscriber Subscriber)
	Unsubscribe(subscriber Subscriber)
	Broadcast(eventType EventType, payload interface{})

	// Voice session lifecycle
	StartVoiceSession(
		userId, conversationId, agentId string,
		promptSuffix string,
		audioIn, audioOut voice.AudioFormat,
		features voice.Features,
		sendJson func(interface{}),
		sendBinary func([]byte),
	) (*voice.Session, error)
	// Connection tracking
	MarkSessionConnected(sessionId string)
	MarkSessionDisconnected(sessionId string)
	IsSessionConnected(sessionId string) bool

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
		config:             configuration,
		securityConfig:     securityConfig,
		agentRegistry:      agentRegistry,
		browserRelay:       browserRelay,
		terminalRelay:      terminalRelay,
		scheduler:          scheduler,
		summarizer:         summarizer,
		mediaStore:         mediaStore,
		sessionStore:       sessionStore,
		subscribers:        make(map[Subscriber]struct{}),
		sessionsConnected:  make(map[string]int),
		activeRuns:         make(map[string]*activeRun),
		runIndex:           make(map[string]string),
		lifecycleChannel:   make(chan LifecycleAction, 1),
		conversationStores: make(map[string]*conversations.Store),
	}
}
