// Package gw provides the core Gateway interface that mediates between
// the HTTP/WebSocket layer (v1api) and domain subsystems (agents, browser,
// terminal, scheduler, summarizer, media).
package gw

import (
	"context"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/integrations/browsers/relaybrowser"
	"github.com/teanode/teanode/internal/integrations/terminals"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/summarizer"
	"github.com/teanode/teanode/internal/voice"
)

var log = logging.MustGetLogger("gateway")

// SendMessageParameters are the parameters for sending a message through the gateway.
type SendMessageParameters struct {
	AgentID            string
	ConversationID     string // empty = auto-create
	Message            string
	Model              string
	OriginID           string              // opaque client-generated ID echoed in broadcasts so the sender can filter its own messages
	Origin             string              // source of the message (e.g. "webui", "discord", "telegram"); empty for automated sources like the scheduler
	OriginSessionID    string              // source session identifier (used for disconnect-aware notifications)
	Attachments        []map[string]string // file attachments
	SystemPromptSuffix string              // optional; appended to system prompt for this run only
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
	Usage      map[string]int
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

// Gateway is the main domain interface for the TeaNode gateway.
type Gateway interface {
	// Subsystem access
	Coordinator() *coordinators.Coordinator
	BrowserRelay() *relaybrowser.Relay
	TerminalRelay() *terminals.Relay

	// Domain operations
	ProviderRegistry() *providers.Registry

	// Default agent / conversation
	EnsureDefaultConversation(userId, agentId string) string
	SetDefaultConversation(userId, agentId, conversationId string)
	SetDefaultConversationIfUnset(userId, agentId, conversationId string) bool
	NewDefaultConversation(userId, agentId, model string) string

	// Centralized message sending and run management
	SendMessage(ctx context.Context, parameters SendMessageParameters, callerCallbacks *runners.RunCallbacks) *RunHandle
	AbortRun(runId string) bool
	GetActiveRun(conversationId string) string
	DeleteConversation(userId, agentId, conversationId string) error

	// Event broadcasting via subscriber pattern
	Subscribe(subscriber Subscriber)
	Unsubscribe(subscriber Subscriber)
	Broadcast(eventType EventType, payload interface{})

	// Voice session lifecycle
	StartVoiceSession(
		ctx context.Context,
		conversationId, agentId string,
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
}

// New creates a new Gateway. The ctx must contain a lifecycle.Lifecycle
// (via lifecycle.ContextWithLifecycle) for lifecycle controls to work.
func New(
	ctx context.Context,
	configuration *models.Configuration,
	coordinator *coordinators.Coordinator,
	defaults *runners.DefaultConversationManager,
	browserRelay *relaybrowser.Relay,
	terminalRelay *terminals.Relay,
	summarizer *summarizer.Summarizer,
) Gateway {
	return &gateway{
		ctx:               ctx,
		config:            configuration,
		coordinator:       coordinator,
		defaults:          defaults,
		browserRelay:      browserRelay,
		terminalRelay:     terminalRelay,
		summarizer:        summarizer,
		subscribers:       make(map[Subscriber]struct{}),
		sessionsConnected: make(map[string]int),
		activeRuns:        make(map[string]*activeRun),
		runIndex:          make(map[string]string),
	}
}
