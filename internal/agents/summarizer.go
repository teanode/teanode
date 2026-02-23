package agents

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/deferutil"
)

// Summarizer runs a background loop that generates summaries for inactive conversations.
type Summarizer struct {
	registry             *AgentRegistry
	config               *configs.Config
	configMutex          sync.RWMutex
	IsConversationActive func(conversationId string) bool        // returns true if conversation has an active run
	Broadcast            func(event string, payload interface{}) // broadcasts events to connected clients
	notify               chan struct{}
	cancel               context.CancelFunc
	done                 chan struct{}
}

// NewSummarizer creates a new Summarizer for the given agent registry and configs.
func NewSummarizer(registry *AgentRegistry, configuration *configs.Config) *Summarizer {
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
func (self *Summarizer) SetConfig(configuration *configs.Config) {
	self.configMutex.Lock()
	defer self.configMutex.Unlock()
	self.config = configuration
}

// resolveConfig returns the current resolved summarizer configs.
func (self *Summarizer) resolveConfig() configs.SummarizerConfig {
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
	log.Info("conversation summarizer started")
}

// Stop gracefully stops the summarizer and waits for it to finish.
func (self *Summarizer) Stop() {
	if self.cancel != nil {
		self.cancel()
		<-self.done
		log.Info("conversation summarizer stopped")
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
	userIds := self.listUserIds()
	if len(userIds) == 0 {
		return
	}
	self.registry.ForEach(func(agentId string, runner *Runner) {
		if ctx.Err() != nil {
			return
		}
		for _, userId := range userIds {
			if ctx.Err() != nil {
				return
			}
			self.summarizeAgent(ctx, userId, agentId, runner)
		}
	})
}

func (self *Summarizer) listUserIds() []string {
	usersDirectory, err := configs.UsersDirectory()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(usersDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return nil
	}
	userIds := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		userId := strings.TrimSpace(entry.Name())
		if userId == "" {
			continue
		}
		userIds = append(userIds, userId)
	}
	sort.Strings(userIds)
	return userIds
}

func (self *Summarizer) summarizeAgent(ctx context.Context, userId, agentId string, runner *Runner) {
	resolved := self.resolveConfig()

	store := runner.ConversationsForUser(userId)
	if store == nil {
		log.Debugf("summarizer: conversation store unavailable for user %s agent %s", userId, agentId)
		return
	}
	conversations, err := store.List()
	if err != nil {
		log.Debugf("summarizer: failed to list conversations for user %s agent %s: %v", userId, agentId, err)
		return
	}

	now := time.Now().UnixMilli()
	inactivityThreshold := now - (time.Duration(resolved.InactivityTime) * time.Minute).Milliseconds()

	for _, conversationInfo := range conversations {
		if ctx.Err() != nil {
			return
		}

		// Check if summary is already up-to-date.
		header, err := store.LoadHeader(conversationInfo.ID)
		if err != nil {
			continue
		}
		if header.SummarizedAt >= conversationInfo.LastActive {
			continue
		}

		// Untitled conversations are summarized immediately (no inactivity wait)
		// and are allowed through even during active runs so they get a title ASAP.
		// Titled conversations skip re-summarization while a run is active or
		// if the conversation hasn't been inactive long enough.
		if header.Title != "" {
			if self.IsConversationActive != nil && self.IsConversationActive(conversationInfo.ID) {
				continue
			}
			if conversationInfo.LastActive > inactivityThreshold {
				continue
			}
		}

		// Load messages to check minimum count and generate summary.
		messages, err := store.Load(conversationInfo.ID)
		if err != nil {
			log.Debugf("summarizer: failed to load conversation %s: %v", conversationInfo.ID, err)
			continue
		}
		if len(messages) < resolved.MinMessages {
			continue
		}

		self.summarizeConversation(ctx, runner, store, conversationInfo.ID, header, messages)
	}
}

func (self *Summarizer) summarizeConversation(
	ctx context.Context,
	runner *Runner,
	store *conversations.Store,
	conversationId string,
	header *conversations.Header,
	messages []conversations.Message,
) {
	configuration, providerRegistry, _, _, _ := runner.Snapshot()
	resolved := self.resolveConfig()

	// If conversation has been compacted, only consider messages after the
	// last summary and provide the existing summary as context.
	var previousSummary string
	if idx := findLastSummaryIndex(messages); idx >= 0 {
		previousSummary = messages[idx].ContentText()
		messages = messages[idx+1:]
	}

	// Build conversation text prioritizing recent messages.
	conversationText := messagesText(messages, resolved.MaxConversationChars, resolved.MaxMessageChars)
	if previousSummary != "" {
		conversationText = "[Previous summary]: " + previousSummary + "\n" + conversationText
	}

	// Resolve the model: SummarizerModel > Default.
	qualifiedModel := configuration.Models.Default
	if configuration.Models.SummarizerModel != "" {
		qualifiedModel = configuration.Models.SummarizerModel
	}

	provider, bareModel, err := providerRegistry.Resolve(qualifiedModel)
	if err != nil {
		log.Debugf("summarizer: failed to resolve model %q: %v", qualifiedModel, err)
		return
	}

	// Always generate both title and summary together.
	self.generateTitleAndSummary(ctx, provider, bareModel, store, conversationId, conversationText)
}

func (self *Summarizer) generateTitleAndSummary(
	ctx context.Context,
	provider providers.Provider,
	bareModel string,
	store *conversations.Store,
	conversationId string,
	conversationText string,
) {
	request := providers.ChatRequest{
		Model: bareModel,
		Messages: []providers.ChatMessage{
			{
				Role:    "system",
				Content: "Analyze the following conversation. Output a JSON object with two fields:\n- \"title\": a short title (max 8 words)\n- \"summary\": a 2-4 sentence summary of the main topic, key decisions, and outcomes\n\nOutput only valid JSON, nothing else.",
			},
			{Role: "user", Content: conversationText},
		},
	}

	response, err := provider.ChatCompletion(ctx, request)
	if err != nil || len(response.Choices) == 0 {
		log.Debugf("summarizer: LLM call failed for conversation %s: %v", conversationId, err)
		return
	}

	responseText := strings.TrimSpace(response.Choices[0].Message.ContentText())
	if responseText == "" {
		return
	}

	var result struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		log.Debugf("summarizer: failed to parse JSON response for conversation %s: %v", conversationId, err)
		return
	}

	if result.Title == "" && result.Summary == "" {
		return
	}

	// Fallback: if only one was generated, still save what we have.
	if result.Title == "" {
		result.Title = time.Now().Format("Jan 2, 2006 3:04 PM")
	}

	if err := store.SetTitleAndSummary(conversationId, result.Title, result.Summary); err != nil {
		log.Debugf("summarizer: failed to save title+summary for conversation %s: %v", conversationId, err)
		return
	}
	log.Debugf("summarizer: titled+summarized conversation %s", conversationId)

	if self.Broadcast != nil {
		self.Broadcast("conversations", nil)
	}
}
