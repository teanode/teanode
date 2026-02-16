// Package gateway provides the core Gateway interface that mediates between
// the HTTP/WebSocket layer (v1api) and domain subsystems (agents, browser,
// terminal, scheduler, summarizer, media).
package gw

import (
	"context"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/integrations/browsers"
	"github.com/teanode/teanode/internal/integrations/terminals"
	"github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/provider"
	"github.com/teanode/teanode/internal/web"
)

var log = logging.MustGetLogger("gateway")

// Gateway is the main domain interface for the TeaNode gateway.
type Gateway interface {
	// Configuration access
	Config() *configs.Config
	SetConfig(configuration *configs.Config)

	// Subsystem access
	AgentRegistry() *agents.AgentRegistry
	Scheduler() *jobs.Scheduler
	Summarizer() *agents.Summarizer
	MediaStore() *media.Store
	BrowserRelay() *browsers.Relay
	TerminalRelay() *terminals.Relay

	// Domain operations
	ResolveRunner(agentId string) *agents.Runner
	LoadModels(ctx context.Context) (map[string][]provider.ModelInfo, error)
	InvalidateModelsCache()

	// Run tracking
	SetActiveRun(conversationId, runId string)
	ClearActiveRun(conversationId, runId string)
	GetActiveRun(conversationId string) string

	// Auth middleware for the HTTP server
	AuthMiddleware() web.Middleware

	// ListenAddress returns the host:port the server should bind to.
	ListenAddress() string
}

// New creates a new Gateway.
func New(
	configuration *configs.Config,
	agentRegistry *agents.AgentRegistry,
	browserRelay *browsers.Relay,
	terminalRelay *terminals.Relay,
	scheduler *jobs.Scheduler,
	summarizer *agents.Summarizer,
	mediaStore *media.Store,
) Gateway {
	return &gateway{
		config:        configuration,
		agentRegistry: agentRegistry,
		browserRelay:  browserRelay,
		terminalRelay: terminalRelay,
		scheduler:     scheduler,
		summarizer:    summarizer,
		mediaStore:    mediaStore,
		activeRuns:    make(map[string]string),
	}
}
