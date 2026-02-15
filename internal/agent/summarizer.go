package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/config"
	"github.com/teanode/teanode/internal/provider"
	"github.com/teanode/teanode/internal/session"
	"github.com/teanode/teanode/internal/util/deferutil"
)

// Summarizer runs a background loop that generates summaries for inactive sessions.
type Summarizer struct {
	registry        *AgentRegistry
	config          *config.Config
	configMutex     sync.RWMutex
	IsSessionActive func(sessionKey string) bool               // returns true if session has an active run
	Broadcast       func(event string, payload interface{})    // broadcasts events to connected clients
	notify          chan struct{}
	cancel          context.CancelFunc
	done            chan struct{}
}

// NewSummarizer creates a new Summarizer for the given agent registry and config.
func NewSummarizer(registry *AgentRegistry, configuration *config.Config) *Summarizer {
	return &Summarizer{
		registry: registry,
		config:   configuration,
		notify:   make(chan struct{}, 1),
	}
}

// Notify wakes the summarizer loop so it runs immediately instead of waiting
// for the next tick. Non-blocking; extra notifications are coalesced.
func (self *Summarizer) Notify() {
	select {
	case self.notify <- struct{}{}:
	default:
	}
}

// SetConfig updates the summarizer's config (safe for concurrent use).
func (self *Summarizer) SetConfig(configuration *config.Config) {
	self.configMutex.Lock()
	defer self.configMutex.Unlock()
	self.config = configuration
}

// resolveConfig returns the current resolved summarizer config.
func (self *Summarizer) resolveConfig() config.SummarizerConfig {
	self.configMutex.RLock()
	defer self.configMutex.RUnlock()
	return self.config.ResolveSummarizerConfig()
}

// Start begins the background summarization loop.
func (self *Summarizer) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	self.cancel = cancel
	self.done = make(chan struct{})

	go func() {
		defer deferutil.Recover()
		defer close(self.done)
		self.loop(ctx)
	}()
	log.Info("session summarizer started")
}

// Stop gracefully stops the summarizer and waits for it to finish.
func (self *Summarizer) Stop() {
	if self.cancel != nil {
		self.cancel()
		<-self.done
		log.Info("session summarizer stopped")
	}
}

func (self *Summarizer) loop(ctx context.Context) {
	resolved := self.resolveConfig()

	// Startup delay to let the system settle. A notification cuts this short.
	select {
	case <-time.After(time.Duration(resolved.StartupDelay) * time.Minute):
	case <-self.notify:
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(time.Duration(resolved.TickInterval) * time.Minute)
	defer ticker.Stop()

	for {
		self.summarizeAll(ctx)

		select {
		case <-ticker.C:
		case <-self.notify:
		case <-ctx.Done():
			return
		}
	}
}

func (self *Summarizer) summarizeAll(ctx context.Context) {
	self.registry.ForEach(func(agentId string, runner *Runner) {
		if ctx.Err() != nil {
			return
		}
		self.summarizeAgent(ctx, agentId, runner)
	})
}

func (self *Summarizer) summarizeAgent(ctx context.Context, agentId string, runner *Runner) {
	resolved := self.resolveConfig()

	sessions, err := runner.Sessions.List()
	if err != nil {
		log.Debugf("summarizer: failed to list sessions for agent %s: %v", agentId, err)
		return
	}

	now := time.Now().UnixMilli()
	inactivityThreshold := now - (time.Duration(resolved.InactivityTime) * time.Minute).Milliseconds()

	for _, sessionInfo := range sessions {
		if ctx.Err() != nil {
			return
		}

		// Skip sessions with an active run.
		if self.IsSessionActive != nil && self.IsSessionActive(sessionInfo.Key) {
			continue
		}

		// Check if summary is already up-to-date.
		header, err := runner.Sessions.LoadHeader(sessionInfo.Key)
		if err != nil {
			continue
		}
		if header.SummarizedAt >= sessionInfo.LastActive {
			continue
		}

		// Untitled sessions are summarized immediately (no inactivity wait).
		// Titled sessions require inactivity before re-summarizing.
		if header.Title != "" && sessionInfo.LastActive > inactivityThreshold {
			continue
		}

		// Load messages to check minimum count and generate summary.
		messages, err := runner.Sessions.Load(sessionInfo.Key)
		if err != nil {
			log.Debugf("summarizer: failed to load session %s: %v", sessionInfo.Key, err)
			continue
		}
		if len(messages) < resolved.MinMessages {
			continue
		}

		self.summarizeSession(ctx, runner, sessionInfo.Key, header, messages)
	}
}

func (self *Summarizer) summarizeSession(
	ctx context.Context,
	runner *Runner,
	sessionKey string,
	header *session.SessionHeader,
	messages []session.Message,
) {
	configuration, providers, _, _, _ := runner.Snapshot()
	resolved := self.resolveConfig()

	// Build conversation text for the LLM.
	conversationText := buildConversationText(messages, resolved.MaxConversationChars, resolved.MaxMessageChars)

	// Resolve the model: SummarizerModel > Default.
	qualifiedModel := configuration.Models.Default
	if configuration.Models.SummarizerModel != "" {
		qualifiedModel = configuration.Models.SummarizerModel
	}

	client, bareModel, err := providers.Resolve(qualifiedModel)
	if err != nil {
		log.Debugf("summarizer: failed to resolve model %q: %v", qualifiedModel, err)
		return
	}

	// Always generate both title and summary together.
	self.generateTitleAndSummary(ctx, client, bareModel, runner, sessionKey, conversationText)
}

func (self *Summarizer) generateTitleAndSummary(
	ctx context.Context,
	client *provider.Client,
	bareModel string,
	runner *Runner,
	sessionKey string,
	conversationText string,
) {
	request := provider.ChatRequest{
		Model: bareModel,
		Messages: []provider.ChatMessage{
			{
				Role:    "system",
				Content: "Analyze the following conversation. Output a JSON object with two fields:\n- \"title\": a short title (max 8 words)\n- \"summary\": a 2-4 sentence summary of the main topic, key decisions, and outcomes\n\nOutput only valid JSON, nothing else.",
			},
			{Role: "user", Content: conversationText},
		},
	}

	response, err := client.ChatCompletion(ctx, request)
	if err != nil || len(response.Choices) == 0 {
		log.Debugf("summarizer: LLM call failed for session %s: %v", sessionKey, err)
		return
	}

	responseText := strings.TrimSpace(response.Choices[0].Message.Content)
	if responseText == "" {
		return
	}

	var result struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		log.Debugf("summarizer: failed to parse JSON response for session %s: %v", sessionKey, err)
		return
	}

	if result.Title == "" && result.Summary == "" {
		return
	}

	// Fallback: if only one was generated, still save what we have.
	if result.Title == "" {
		result.Title = time.Now().Format("Jan 2, 2006 3:04 PM")
	}

	if err := runner.Sessions.SetTitleAndSummary(sessionKey, result.Title, result.Summary); err != nil {
		log.Debugf("summarizer: failed to save title+summary for session %s: %v", sessionKey, err)
		return
	}
	log.Debugf("summarizer: titled+summarized session %s", sessionKey)

	if self.Broadcast != nil {
		self.Broadcast("sessions", nil)
	}
}

// buildConversationText constructs a truncated text representation of messages.
func buildConversationText(messages []session.Message, maxConversationChars int, maxMessageChars int) string {
	var builder strings.Builder
	totalChars := 0

	for _, message := range messages {
		if totalChars >= maxConversationChars {
			break
		}

		role := message.Role
		if role == "tool" && message.ToolName != "" {
			role = fmt.Sprintf("tool(%s)", message.ToolName)
		}

		content := message.ContentText()
		if len(content) > maxMessageChars {
			content = content[:maxMessageChars] + "..."
		}

		line := fmt.Sprintf("[%s]: %s\n", role, content)
		if totalChars+len(line) > maxConversationChars {
			remaining := maxConversationChars - totalChars
			if remaining > 0 {
				builder.WriteString(line[:remaining])
			}
			break
		}
		builder.WriteString(line)
		totalChars += len(line)
	}

	return builder.String()
}
