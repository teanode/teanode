package agents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/projects"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/timeutil"
)

const summarizerDescriptionRefreshInterval = 24 * time.Hour
const summarizerRequestTimeout = 25 * time.Second
const summarizerDescriptionMaxTokens = 120
const summarizerDescriptionMaxWorkspaceChars = 4000

type summarizerMode string

const (
	summarizerModeConversationTitleAndSummary summarizerMode = "conversation_title_and_summary"
	summarizerModeAgentDescription            summarizerMode = "agent_description"
	summarizerModeUserDescription             summarizerMode = "user_description"
	summarizerModeProjectDescription          summarizerMode = "project_description"
)

// Summarizer runs a background loop that synthesizes titles/summaries/descriptions
// for conversations, agents, users, and projects.
type Summarizer struct {
	registry             *AgentRegistry
	config               *configs.Config
	configMutex          sync.RWMutex
	IsConversationActive func(conversationId string) bool        // returns true if conversation has an active run
	Broadcast            func(event string, payload interface{}) // broadcasts events to connected clients
	notify               chan struct{}
	cancel               context.CancelFunc
	done                 chan struct{}

	userSourceUpdatedAt    map[string]time.Time
	projectSourceUpdatedAt map[string]time.Time
}

// NewSummarizer creates a new Summarizer for the given agent registry and configs.
func NewSummarizer(registry *AgentRegistry, configuration *configs.Config) *Summarizer {
	return &Summarizer{
		registry:               registry,
		config:                 configuration,
		notify:                 make(chan struct{}, 1),
		userSourceUpdatedAt:    make(map[string]time.Time),
		projectSourceUpdatedAt: make(map[string]time.Time),
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

// resolveConfig returns the current resolved summarizer config.
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
	log.Info("summarizer started")
}

// Stop gracefully stops the summarizer and waits for it to finish.
func (self *Summarizer) Stop() {
	if self.cancel != nil {
		self.cancel()
		<-self.done
		log.Info("summarizer stopped")
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
	if len(userIds) > 0 {
		self.summarizeConversations(ctx, userIds)
		self.summarizeUsers(ctx, userIds)
	}
	self.summarizeAgents(ctx)
	self.summarizeProjects(ctx)
}

func (self *Summarizer) summarizeConversations(ctx context.Context, userIds []string) {
	self.registry.ForEach(func(agentId string, runner *Runner) {
		if ctx.Err() != nil {
			return
		}
		for _, userId := range userIds {
			if ctx.Err() != nil {
				return
			}
			self.summarizeAgentConversations(ctx, userId, agentId, runner)
		}
	})
}

func (self *Summarizer) summarizeAgents(ctx context.Context) {
	self.registry.ForEach(func(agentId string, runner *Runner) {
		if ctx.Err() != nil {
			return
		}
		self.summarizeAgentDescription(ctx, agentId, runner)
	})
}

func (self *Summarizer) summarizeUsers(ctx context.Context, userIds []string) {
	for _, userId := range userIds {
		if ctx.Err() != nil {
			return
		}
		self.summarizeUserDescription(ctx, userId)
	}
}

func (self *Summarizer) summarizeProjects(ctx context.Context) {
	projectConfigs, err := configs.LoadProjectConfigs()
	if err != nil {
		log.Debugf("summarizer: failed to list projects: %v", err)
		return
	}
	for _, project := range projectConfigs {
		if ctx.Err() != nil {
			return
		}
		self.summarizeProjectDescription(ctx, project)
	}
}

func (self *Summarizer) listUserIds() []string {
	usersDirectory := configs.UsersDirectory()
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

func (self *Summarizer) summarizeAgentConversations(ctx context.Context, userId, agentId string, runner *Runner) {
	resolved := self.resolveConfig()

	store := runner.ConversationsForUser(userId)
	if store == nil {
		log.Debugf("summarizer: conversation store unavailable for user %s agent %s", userId, agentId)
		return
	}
	conversationList, err := store.List()
	if err != nil {
		log.Debugf("summarizer: failed to list conversations for user %s agent %s: %v", userId, agentId, err)
		return
	}

	now := time.Now().UnixMilli()
	inactivityThreshold := now - (time.Duration(resolved.InactivityTime) * time.Minute).Milliseconds()

	for _, conversationInfo := range conversationList {
		if ctx.Err() != nil {
			return
		}

		header, err := store.LoadHeader(conversationInfo.ID)
		if err != nil {
			continue
		}
		if header.SummarizedAt >= conversationInfo.LastActive {
			continue
		}

		if header.Title != "" {
			if self.IsConversationActive != nil && self.IsConversationActive(conversationInfo.ID) {
				continue
			}
			if conversationInfo.LastActive > inactivityThreshold {
				continue
			}
		}

		messages, err := store.Load(conversationInfo.ID)
		if err != nil {
			log.Debugf("summarizer: failed to load conversation %s: %v", conversationInfo.ID, err)
			continue
		}
		if len(messages) < resolved.MinMessages {
			continue
		}

		self.summarizeConversationTitleAndSummary(ctx, runner, store, conversationInfo.ID, messages)
	}
}

func (self *Summarizer) summarizeConversationTitleAndSummary(
	ctx context.Context,
	runner *Runner,
	store *conversations.Store,
	conversationId string,
	messages []conversations.Message,
) {
	resolved := self.resolveConfig()

	var previousSummary string
	if idx := findLastSummaryIndex(messages); idx >= 0 {
		previousSummary = messages[idx].ContentText()
		messages = messages[idx+1:]
	}

	conversationText := messagesText(messages, resolved.MaxConversationChars, resolved.MaxMessageChars)
	if previousSummary != "" {
		conversationText = "[Previous summary]: " + previousSummary + "\n" + conversationText
	}

	provider, bareModel, ok := self.resolveSynthesisProvider(runner, "")
	if !ok {
		return
	}

	responseText, ok := self.runSynthesisRequest(ctx, provider, bareModel, summarizerModeConversationTitleAndSummary,
		prompts.SummarizerTitleAndSummarySystemPrompt,
		conversationText,
		0,
	)
	if !ok {
		return
	}

	var result struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		log.Debugf("summarizer: failed to parse conversation title+summary for %s: %v", conversationId, err)
		return
	}
	result.Title = strings.TrimSpace(result.Title)
	result.Summary = strings.TrimSpace(result.Summary)
	if result.Title == "" && result.Summary == "" {
		return
	}
	if result.Title == "" {
		result.Title = time.Now().Format("Jan 2, 2006 3:04 PM")
	}

	if err := store.SetTitleAndSummary(conversationId, result.Title, result.Summary); err != nil {
		log.Debugf("summarizer: failed to save title+summary for conversation %s: %v", conversationId, err)
		return
	}

	if self.Broadcast != nil {
		self.Broadcast("conversations", nil)
	}
}

func (self *Summarizer) summarizeAgentDescription(ctx context.Context, agentId string, runner *Runner) {
	state, err := configs.LoadAgentConfig(agentId)
	if err != nil {
		log.Debugf("summarizer: failed to load agent state for %s: %v", agentId, err)
		return
	}
	if !self.shouldRefreshAgentDescription(state) {
		return
	}

	configuration, _, tools, workspaceDirectory, _ := runner.Snapshot()

	maxChars := configuration.ResolveModelLimits(configuration.AgentModel(agentId)).MaxWorkspaceFileChars
	if maxChars <= 0 || maxChars > summarizerDescriptionMaxWorkspaceChars {
		maxChars = summarizerDescriptionMaxWorkspaceChars
	}
	agentContent := emptyFallback(loadWorkspaceFile(workspaceDirectory, "AGENT.md", maxChars))
	agentMemory := emptyFallback(loadWorkspaceFile(workspaceDirectory, "MEMORY.md", maxChars))

	systemPrompt := resolveIdentityLine(configuration, agentId) +
		"\n\nGenerate a concise self-description for inter-agent task routing.\nUse only plain text.\n\nAGENT.md:\n" +
		agentContent + "\n\nMEMORY.md:\n" + agentMemory

	toolNames := []string{}
	if tools != nil {
		toolNames = tools.Names()
	}
	userPrompt := "Write a plain-text routing description in 1-2 sentences. State your specialty, what tasks should be routed to you, and key tools. Tools: " + summarizeToolNames(toolNames, 20)

	provider, bareModel, ok := self.resolveSynthesisProvider(runner, "")
	if !ok {
		return
	}
	description, ok := self.runSynthesisRequest(ctx, provider, bareModel, summarizerModeAgentDescription, systemPrompt, userPrompt, summarizerDescriptionMaxTokens)
	if !ok {
		return
	}

	state.Description = description
	state.DescriptionUpdatedAt = timeutil.Now()
	if err := configs.SaveAgentConfig(agentId, state); err != nil {
		log.Debugf("summarizer: failed to save agent state for %s: %v", agentId, err)
	}
}

func (self *Summarizer) summarizeUserDescription(ctx context.Context, userId string) {
	profile, err := configs.LoadUserConfig(userId)
	if err != nil {
		log.Debugf("summarizer: failed to load user profile for %s: %v", userId, err)
		return
	}

	sourceUpdatedAt := self.userDescriptionSourceUpdatedAt(userId)
	if strings.TrimSpace(profile.Description) != "" {
		if lastSeen, ok := self.userSourceUpdatedAt[userId]; ok {
			if !sourceUpdatedAt.After(lastSeen) {
				return
			}
		} else {
			self.userSourceUpdatedAt[userId] = sourceUpdatedAt
			return
		}
	}

	runner := self.defaultRunner()
	if runner == nil {
		return
	}

	userWorkspaceDirectory := configs.UserWorkspaceDirectory(userId)
	userContent := emptyFallback(loadWorkspaceFile(userWorkspaceDirectory, "USER.md", summarizerDescriptionMaxWorkspaceChars))
	userMemory := emptyFallback(loadWorkspaceFile(userWorkspaceDirectory, "MEMORY.md", summarizerDescriptionMaxWorkspaceChars))

	systemPrompt := "You generate concise user descriptions for personalization and routing. Use only plain text."
	userPrompt := "Write a plain-text user description in 1-2 sentences. Include preferences, goals, and relevant constraints.\n\nUSER.md:\n" +
		userContent + "\n\nMEMORY.md:\n" + userMemory

	provider, bareModel, ok := self.resolveSynthesisProvider(runner, "")
	if !ok {
		return
	}
	description, ok := self.runSynthesisRequest(ctx, provider, bareModel, summarizerModeUserDescription, systemPrompt, userPrompt, summarizerDescriptionMaxTokens)
	if !ok {
		return
	}

	profile.Description = description
	if err := configs.SaveUserConfig(userId, profile); err != nil {
		log.Debugf("summarizer: failed to save user profile for %s: %v", userId, err)
		return
	}
	self.userSourceUpdatedAt[userId] = sourceUpdatedAt
}

func (self *Summarizer) summarizeProjectDescription(ctx context.Context, project configs.ProjectConfig) {
	projectId := strings.TrimSpace(project.ID)
	if projectId == "" {
		return
	}
	updatedAt := project.UpdatedAt.Time

	if strings.TrimSpace(project.Description) != "" {
		if lastSeen, ok := self.projectSourceUpdatedAt[projectId]; ok {
			if !updatedAt.After(lastSeen) {
				return
			}
		} else {
			self.projectSourceUpdatedAt[projectId] = updatedAt
			return
		}
	}

	runner := self.defaultRunner()
	if runner == nil {
		return
	}

	workspaceDirectory, err := projects.WorkspaceDirectory(projectId)
	if err != nil {
		return
	}
	projectMarkdown := emptyFallback(loadWorkspaceFile(workspaceDirectory, "PROJECT.md", summarizerDescriptionMaxWorkspaceChars))

	systemPrompt := "You generate concise project descriptions for routing and discovery. Use only plain text."
	userPrompt := "Write a plain-text project description in 1-2 sentences. Include what work belongs in this project.\n\nPROJECT.md:\n" + projectMarkdown

	provider, bareModel, ok := self.resolveSynthesisProvider(runner, "")
	if !ok {
		return
	}
	description, ok := self.runSynthesisRequest(ctx, provider, bareModel, summarizerModeProjectDescription, systemPrompt, userPrompt, summarizerDescriptionMaxTokens)
	if !ok {
		return
	}

	project.Description = description
	if err := configs.SaveProjectConfig(project.ID, &project); err != nil {
		log.Debugf("summarizer: failed to save project metadata for %s: %v", projectId, err)
		return
	}
	self.projectSourceUpdatedAt[projectId] = updatedAt
}

func (self *Summarizer) runSynthesisRequest(
	ctx context.Context,
	provider providers.Provider,
	model string,
	mode summarizerMode,
	systemPrompt string,
	userPrompt string,
	maxTokens int,
) (string, bool) {
	request := providers.ChatRequest{
		Model: model,
		Messages: []providers.ChatMessage{
			{Role: "system", Content: strings.TrimSpace(systemPrompt)},
			{Role: "user", Content: strings.TrimSpace(userPrompt)},
		},
	}
	if maxTokens > 0 {
		request.MaxTokens = maxTokens
	}

	requestContext, cancel := context.WithTimeout(ctx, summarizerRequestTimeout)
	defer cancel()
	response, err := provider.ChatCompletion(requestContext, request)
	if err != nil || len(response.Choices) == 0 {
		log.Debugf("summarizer: synthesis %s failed: %v", mode, err)
		return "", false
	}
	content := strings.TrimSpace(response.Choices[0].Message.ContentText())
	if content == "" {
		return "", false
	}
	return content, true
}

func (self *Summarizer) resolveSynthesisProvider(runner *Runner, modelOverride string) (providers.Provider, string, bool) {
	if runner == nil {
		return nil, "", false
	}
	configuration, providerRegistry, _, _, _ := runner.Snapshot()
	qualifiedModel := strings.TrimSpace(modelOverride)
	if qualifiedModel == "" {
		qualifiedModel = strings.TrimSpace(configuration.Models.SummarizerModel)
	}
	if qualifiedModel == "" {
		qualifiedModel = strings.TrimSpace(configuration.Models.Default)
	}
	if qualifiedModel == "" {
		return nil, "", false
	}

	provider, bareModel, err := providerRegistry.Resolve(qualifiedModel)
	if err != nil {
		log.Debugf("summarizer: failed to resolve synthesis model %q: %v", qualifiedModel, err)
		return nil, "", false
	}
	return provider, bareModel, true
}

func (self *Summarizer) defaultRunner() *Runner {
	if self.registry == nil {
		return nil
	}
	var selectedRunner *Runner
	self.registry.ForEach(func(agentId string, runner *Runner) {
		if selectedRunner == nil {
			selectedRunner = runner
		}
	})
	return selectedRunner
}

func (self *Summarizer) userDescriptionSourceUpdatedAt(userId string) time.Time {
	userWorkspaceDirectory := configs.UserWorkspaceDirectory(userId)
	latest := time.Time{}
	for _, path := range []string{
		filepath.Join(userWorkspaceDirectory, "USER.md"),
		filepath.Join(userWorkspaceDirectory, "MEMORY.md"),
	} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			continue
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

func (self *Summarizer) shouldRefreshAgentDescription(state *configs.AgentConfig) bool {
	if state == nil || strings.TrimSpace(state.Description) == "" {
		return true
	}
	if state.DescriptionUpdatedAt.IsZero() {
		return true
	}
	return time.Since(state.DescriptionUpdatedAt.Time) >= summarizerDescriptionRefreshInterval
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
