package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/timeutil"
)

const describerRefreshInterval = 24 * time.Hour
const describerCheckInterval = 5 * time.Minute
const describerRequestTimeout = 25 * time.Second
const describerMaxTokens = 120
const describerMaxWorkspaceChars = 4000

// Describer runs a background loop that periodically refreshes agent self-descriptions.
type Describer struct {
	registry *AgentRegistry
	notify   chan struct{}
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewDescriber creates a new Describer for the given agent registry.
func NewDescriber(registry *AgentRegistry) *Describer {
	return &Describer{
		registry: registry,
		notify:   make(chan struct{}, 1),
	}
}

// Notify wakes the describer loop so it runs immediately instead of waiting
// for the next check interval. Non-blocking; extra notifications are coalesced.
func (self *Describer) Notify() {
	select {
	case self.notify <- struct{}{}:
	default:
	}
}

// Start begins the background description loop.
func (self *Describer) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	self.cancel = cancel
	self.done = make(chan struct{})

	go func() {
		defer deferutil.Recover()
		defer close(self.done)
		self.loop(ctx)
	}()
	log.Info("agent describer started")
}

// Stop gracefully stops the describer and waits for it to finish.
func (self *Describer) Stop() {
	if self.cancel != nil {
		self.cancel()
		<-self.done
		log.Info("agent describer stopped")
	}
}

func (self *Describer) loop(ctx context.Context) {
	// Run once at startup to populate missing descriptions immediately.
	self.describeAll(ctx)

	ticker := time.NewTicker(describerCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			self.describeAll(ctx)
		case <-self.notify:
			self.describeAll(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (self *Describer) describeAll(ctx context.Context) {
	self.registry.ForEach(func(agentId string, runner *Runner) {
		if ctx.Err() != nil {
			return
		}
		self.describeAgent(ctx, agentId, runner)
	})
}

func (self *Describer) shouldRefresh(state *configs.AgentState) bool {
	if state == nil || strings.TrimSpace(state.Description) == "" {
		return true
	}
	if state.DescriptionUpdatedAt.IsZero() {
		return true
	}
	return time.Since(state.DescriptionUpdatedAt.Time) >= describerRefreshInterval
}

func (self *Describer) describeAgent(ctx context.Context, agentId string, runner *Runner) {
	state, err := configs.LoadAgentState(agentId)
	if err != nil {
		log.Debugf("describer: failed to load agent state for %s: %v", agentId, err)
		return
	}
	if !self.shouldRefresh(state) {
		return
	}

	configuration, providerRegistry, tools, workspaceDirectory, _ := runner.Snapshot()

	qualifiedModel := strings.TrimSpace(configuration.Models.SummarizerModel)
	if qualifiedModel == "" {
		qualifiedModel = configuration.AgentModel(agentId)
	}
	if qualifiedModel == "" {
		return
	}
	provider, bareModel, err := providerRegistry.Resolve(qualifiedModel)
	if err != nil {
		log.Debugf("describer: failed to resolve model %q for agent %s: %v", qualifiedModel, agentId, err)
		return
	}

	limits := configuration.ResolveModelLimits(qualifiedModel)
	systemPrompt := buildDescriberSystemPrompt(configuration, agentId, workspaceDirectory, limits.MaxWorkspaceFileChars)

	toolNames := []string{}
	if tools != nil {
		toolNames = tools.Names()
	}
	toolList := summarizeToolNames(toolNames, 20)

	request := providers.ChatRequest{
		Model:     bareModel,
		MaxTokens: describerMaxTokens,
		Messages: []providers.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{
				Role: "user",
				Content: "Write a plain-text routing description in 1-2 sentences. " +
					"State your specialty, what tasks should be routed to you, and key tools. " +
					"Tools: " + toolList,
			},
		},
	}

	requestCtx, cancel := context.WithTimeout(ctx, describerRequestTimeout)
	defer cancel()

	response, err := provider.ChatCompletion(requestCtx, request)
	if err != nil || len(response.Choices) == 0 {
		log.Debugf("describer: failed to describe agent %s: %v", agentId, err)
		return
	}
	description := strings.TrimSpace(response.Choices[0].Message.ContentText())
	if description == "" {
		return
	}

	state.Description = description
	state.DescriptionUpdatedAt = timeutil.Now()
	if err := configs.SaveAgentState(agentId, state); err != nil {
		log.Debugf("describer: failed to save agent state for %s: %v", agentId, err)
	}
}

func buildDescriberSystemPrompt(configuration *configs.Config, agentId, workspaceDirectory string, maxWorkspaceFileChars int) string {
	maxChars := maxWorkspaceFileChars
	if maxChars <= 0 || maxChars > describerMaxWorkspaceChars {
		maxChars = describerMaxWorkspaceChars
	}

	agentContent := loadWorkspaceFile(workspaceDirectory, "AGENT.md", maxChars)
	agentMemory := loadWorkspaceFile(workspaceDirectory, "MEMORY.md", maxChars)

	return fmt.Sprintf(
		"%s\n\nGenerate a concise self-description for inter-agent task routing.\nUse only plain text.\n\nAGENT.md:\n%s\n\nMEMORY.md:\n%s",
		resolveIdentityLine(configuration, agentId),
		emptyFallback(agentContent),
		emptyFallback(agentMemory),
	)
}

func summarizeToolNames(toolNames []string, maxTools int) string {
	if len(toolNames) == 0 {
		return "none"
	}
	if maxTools <= 0 || len(toolNames) <= maxTools {
		return strings.Join(toolNames, ", ")
	}
	return strings.Join(toolNames[:maxTools], ", ") + ", ..."
}

func emptyFallback(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "(empty)"
	}
	return trimmed
}
