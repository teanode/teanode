package summarizers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/ptrto"
)

var log = logging.MustGetLogger("summarizer")

const summarizerRequestTimeout = 25 * time.Second
const summarizerDescriptionMaxTokens = 120
const summarizerDescriptionMaxWorkspaceChars = 4000

const summarizerTickInterval = 1   // minutes
const summarizerStartupDelay = 1   // minutes
const summarizerInactivityTime = 3 // minutes
const summarizerMinMessages = 1    // minimum messages to summarize
const summarizerMaxConversationChars = 8000
const summarizerMaxMessageChars = 2000

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
	ctx                  context.Context
	providerRegistry     *providers.ProviderRegistry
	IsConversationActive func(conversationId string) bool        // returns true if conversation has an active run
	Broadcast            func(event string, payload interface{}) // broadcasts events to connected clients
	notify               chan struct{}
	cancel               context.CancelFunc
	done                 chan struct{}
}

// New creates a new Summarizer backed by the given provider registry.
func New(ctx context.Context, providerRegistry *providers.ProviderRegistry) *Summarizer {
	return &Summarizer{
		ctx:              ctx,
		providerRegistry: providerRegistry,
		notify:           make(chan struct{}, 1),
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

// Start begins the background summarization loop.
func (self *Summarizer) Start() {
	ctx, cancel := context.WithCancel(self.ctx)
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
	// Startup delay to let the system settle. A notification cuts this short.
	select {
	case <-time.After(time.Duration(summarizerStartupDelay) * time.Minute):
	case <-self.notify:
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(time.Duration(summarizerTickInterval) * time.Minute)
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
	for _, agentId := range self.listAgentIds() {
		if ctx.Err() != nil {
			return
		}
		for _, userId := range userIds {
			if ctx.Err() != nil {
				return
			}
			self.summarizeAgentConversations(ctx, userId, agentId)
		}
	}
}

func (self *Summarizer) summarizeAgents(ctx context.Context) {
	for _, agentId := range self.listAgentIds() {
		if ctx.Err() != nil {
			return
		}
		self.summarizeAgentDescription(ctx, agentId)
	}
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
	projectList := make([]*models.Project, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		projects, err := transaction.ListProjects(ctx, nil)
		if err != nil {
			return err
		}
		projectList = projects
		return nil
	}); err != nil {
		log.Debugf("summarizer: failed to list projects from store: %v", err)
		return
	}
	for _, project := range projectList {
		if ctx.Err() != nil {
			return
		}
		self.summarizeProjectDescriptionModel(ctx, project)
	}
}

func (self *Summarizer) listUserIds() []string {
	userIds := make([]string, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		users, err := transaction.ListUsers(ctx, nil)
		if err != nil {
			return err
		}
		for _, user := range users {
			userId := user.ID
			if userId != "" {
				userIds = append(userIds, userId)
			}
		}
		return nil
	}); err != nil {
		return nil
	}
	sort.Strings(userIds)
	return userIds
}

func (self *Summarizer) listAgentIds() []string {
	agentIds := make([]string, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		listedAgents, err := transaction.ListAgents(ctx, nil)
		if err != nil {
			return err
		}
		for _, agent := range listedAgents {
			if agent.ID != "" {
				agentIds = append(agentIds, agent.ID)
			}
		}
		return nil
	}); err != nil {
		return nil
	}
	sort.Strings(agentIds)
	return agentIds
}

func (self *Summarizer) summarizeAgentConversations(ctx context.Context, userId, agentId string) {
	conversationList := make([]*models.Conversation, 0)
	err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		items, listError := transaction.ListConversations(ctx, store.ConversationListOptions{
			UserID:  ptrto.Value(userId),
			AgentID: ptrto.Value(agentId),
		}, nil)
		if listError != nil {
			return listError
		}
		conversationList = append(conversationList, items...)
		return nil
	})
	if err != nil {
		log.Debugf("summarizer: failed to list conversations for user %s agent %s: %v", userId, agentId, err)
		return
	}

	now := time.Now().UnixMilli()
	inactivityThreshold := now - (time.Duration(summarizerInactivityTime) * time.Minute).Milliseconds()

	for _, conversationInfo := range conversationList {
		if ctx.Err() != nil {
			return
		}

		lastActive := int64(0)
		if conversationInfo.ModifiedAt != nil {
			lastActive = conversationInfo.ModifiedAt.UnixMilli()
		} else if conversationInfo.CreatedAt != nil {
			lastActive = conversationInfo.CreatedAt.UnixMilli()
		}
		summarizedAt := int64(0)
		if conversationInfo.SummarizedAt != nil {
			summarizedAt = conversationInfo.SummarizedAt.UnixMilli()
		}
		if summarizedAt >= lastActive {
			continue
		}

		if title := conversationInfo.GetTitle(); title != "" {
			if self.IsConversationActive != nil && self.IsConversationActive(conversationInfo.ID) {
				continue
			}
			if lastActive > inactivityThreshold {
				continue
			}
		}

		messages, err := listConversationMessages(ctx, conversationInfo.ID)
		if err != nil {
			log.Debugf("summarizer: failed to load conversation %s: %v", conversationInfo.ID, err)
			continue
		}
		if len(messages) < summarizerMinMessages {
			continue
		}

		self.summarizeConversationTitleAndSummary(ctx, conversationInfo.ID, messages)
	}
}

func (self *Summarizer) summarizeConversationTitleAndSummary(
	ctx context.Context,
	conversationId string,
	messages []*models.ConversationMessage,
) {
	var previousSummary string
	if index := findLastSummaryIndex(messages); index >= 0 {
		previousSummary = conversationMessageContentText(*messages[index])
		messages = messages[index+1:]
	}

	conversationText := messagesText(messages, summarizerMaxConversationChars, summarizerMaxMessageChars)
	if previousSummary != "" {
		conversationText = "[Previous summary]: " + previousSummary + "\n" + conversationText
	}

	provider, modelName, ok := self.resolveSynthesisProvider()
	if !ok {
		return
	}

	responseText, ok := self.runSynthesisRequest(ctx, provider, modelName, summarizerModeConversationTitleAndSummary,
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
	if result.Title == "" && result.Summary == "" {
		return
	}
	if result.Title == "" {
		result.Title = time.Now().Format("Jan 2, 2006 3:04 PM")
	}

	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyConversation(ctx, conversationId, func(conversation *models.Conversation) error {
			conversation.Title = ptrto.Value(result.Title)
			conversation.Summary = ptrto.Value(result.Summary)
			conversation.SummarizedAt = ptrto.TimeNowInLocal()
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		log.Debugf("summarizer: failed to save title+summary for conversation %s: %v", conversationId, err)
		return
	}

	if self.Broadcast != nil {
		self.Broadcast("conversations", nil)
	}
}

func (self *Summarizer) summarizeAgentDescription(ctx context.Context, agentId string) {
	self.summarizeAgentDescriptionModel(ctx, agentId)
}

func (self *Summarizer) summarizeAgentDescriptionModel(ctx context.Context, agentId string) {
	var agent *models.Agent
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		var err error
		agent, err = transaction.GetAgent(ctx, agentId, nil)
		return err
	}); err != nil {
		log.Debugf("summarizer: failed to load agent from store for %s: %v", agentId, err)
		return
	}
	if agent.GetDescription() != "" && agent.SummarizedAt != nil {
		sourceUpdatedAt := self.agentDescriptionSourceUpdatedAt(agentId)
		if !sourceUpdatedAt.After(*agent.SummarizedAt) {
			return
		}
	}

	agentName := agent.GetName()
	agentContent := emptyFallback(self.loadWorkspaceFileFromStore(models.ScopeAgent, agentId, "AGENT.md", summarizerDescriptionMaxWorkspaceChars))
	agentMemory := emptyFallback(self.loadWorkspaceFileFromStore(models.ScopeAgent, agentId, "MEMORY.md", summarizerDescriptionMaxWorkspaceChars))
	systemPrompt := resolveIdentityLine(agentId, agentName) +
		"\n\nGenerate a concise self-description for inter-agent task routing.\nUse only plain text.\n\nAGENT.md:\n" +
		agentContent + "\n\nMEMORY.md:\n" + agentMemory

	// TODO: load tool names from agent configuration
	toolNames := []string{}
	userPrompt := "Write a plain-text routing description in 1-2 sentences. State your specialty, what tasks should be routed to you, and key tools. Tools: " + summarizeToolNames(toolNames, 20)
	provider, modelName, ok := self.resolveSynthesisProvider()
	if !ok {
		return
	}
	description, ok := self.runSynthesisRequest(ctx, provider, modelName, summarizerModeAgentDescription, systemPrompt, userPrompt, summarizerDescriptionMaxTokens)
	if !ok {
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyAgent(ctx, agentId, func(agent *models.Agent) error {
			agent.Description = ptrto.Value(description)
			agent.SummarizedAt = ptrto.TimeNowInLocal()
			return nil
		}, nil)
		return err
	}); err != nil {
		log.Debugf("summarizer: failed to save agent description for %s: %v", agentId, err)
	}
}

func (self *Summarizer) summarizeUserDescription(ctx context.Context, userId string) {
	self.summarizeUserDescriptionModel(ctx, userId)
}

func (self *Summarizer) summarizeUserDescriptionModel(ctx context.Context, userId string) {
	var user *models.User
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		var err error
		user, err = transaction.GetUser(ctx, userId, nil)
		return err
	}); err != nil {
		log.Debugf("summarizer: failed to load user profile from store for %s: %v", userId, err)
		return
	}
	if user.GetDescription() != "" && user.SummarizedAt != nil {
		sourceUpdatedAt := self.userDescriptionSourceUpdatedAt(userId)
		if !sourceUpdatedAt.After(*user.SummarizedAt) {
			return
		}
	}

	userContent := emptyFallback(self.loadWorkspaceFileFromStore(models.ScopeUser, userId, "USER.md", summarizerDescriptionMaxWorkspaceChars))
	userMemory := emptyFallback(self.loadWorkspaceFileFromStore(models.ScopeUser, userId, "MEMORY.md", summarizerDescriptionMaxWorkspaceChars))
	systemPrompt := "You generate concise user descriptions for personalization and routing. Use only plain text."
	userPrompt := "Write a plain-text user description in 1-2 sentences. Include preferences, goals, and relevant constraints.\n\nUSER.md:\n" +
		userContent + "\n\nMEMORY.md:\n" + userMemory
	provider, modelName, ok := self.resolveSynthesisProvider()
	if !ok {
		return
	}
	description, ok := self.runSynthesisRequest(ctx, provider, modelName, summarizerModeUserDescription, systemPrompt, userPrompt, summarizerDescriptionMaxTokens)
	if !ok {
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyUser(ctx, userId, func(user *models.User) error {
			user.Description = ptrto.Value(description)
			user.SummarizedAt = ptrto.TimeNowInLocal()
			return nil
		}, nil)
		return err
	}); err != nil {
		log.Debugf("summarizer: failed to save user profile in store for %s: %v", userId, err)
		return
	}
}

func (self *Summarizer) summarizeProjectDescriptionModel(ctx context.Context, project *models.Project) {
	projectId := project.ID
	if projectId == "" {
		return
	}
	if project.GetDescription() != "" && project.SummarizedAt != nil {
		sourceUpdatedAt := self.workspaceFileModifiedAt(models.ScopeProject, projectId, "PROJECT.md")
		if project.ModifiedAt != nil && project.ModifiedAt.After(sourceUpdatedAt) {
			sourceUpdatedAt = *project.ModifiedAt
		}
		if !sourceUpdatedAt.After(*project.SummarizedAt) {
			return
		}
	}

	projectMarkdown := emptyFallback(self.loadWorkspaceFileFromStore(models.ScopeProject, projectId, "PROJECT.md", summarizerDescriptionMaxWorkspaceChars))
	systemPrompt := "You generate concise project descriptions for routing and discovery. Use only plain text."
	userPrompt := "Write a plain-text project description in 1-2 sentences. Include what work belongs in this project.\n\nPROJECT.md:\n" + projectMarkdown
	provider, modelName, ok := self.resolveSynthesisProvider()
	if !ok {
		return
	}
	description, ok := self.runSynthesisRequest(ctx, provider, modelName, summarizerModeProjectDescription, systemPrompt, userPrompt, summarizerDescriptionMaxTokens)
	if !ok {
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyProject(ctx, projectId, func(project *models.Project) error {
			project.Description = ptrto.Value(description)
			project.SummarizedAt = ptrto.TimeNowInLocal()
			return nil
		}, nil)
		return err
	}); err != nil {
		log.Debugf("summarizer: failed to save project metadata in store for %s: %v", projectId, err)
		return
	}
}

func (self *Summarizer) runSynthesisRequest(
	ctx context.Context,
	provider providers.ChatProvider,
	modelName string,
	mode summarizerMode,
	systemPrompt string,
	userPrompt string,
	maxTokens int,
) (string, bool) {
	request := providers.ChatRequest{
		ModelName: modelName,
		Messages: []providers.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
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
	content := response.Choices[0].Message.ContentText()
	if content == "" {
		return "", false
	}
	return content, true
}

func (self *Summarizer) resolveSynthesisProvider() (providers.ChatProvider, string, bool) {
	var configuration *models.Configuration
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		loadedConfiguration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		configuration = loadedConfiguration
		return nil
	}); err != nil {
		log.Debugf("summarizer: failed to load configuration from store: %v", err)
		return nil, "", false
	}
	providerModelName := ""
	if configuration != nil && configuration.Models != nil {
		if summarizerProviderModelName := configuration.Models.GetSummarizerProviderModelName(); summarizerProviderModelName != "" {
			providerModelName = summarizerProviderModelName
		}
	}
	resolved, _, modelName, err := self.providerRegistry.ResolveProviderAndModel(providerModelName)
	if err != nil {
		log.Debugf("summarizer: failed to resolve synthesis model %q: %v", providerModelName, err)
		return nil, "", false
	}
	provider, ok := resolved.(providers.ChatProvider)
	if !ok {
		log.Debugf("summarizer: provider does not support chat")
		return nil, "", false
	}
	return provider, modelName, true
}

func (self *Summarizer) userDescriptionSourceUpdatedAt(userId string) time.Time {
	userUpdatedAt := self.workspaceFileModifiedAt(models.ScopeUser, userId, "USER.md")
	memoryUpdatedAt := self.workspaceFileModifiedAt(models.ScopeUser, userId, "MEMORY.md")
	if memoryUpdatedAt.After(userUpdatedAt) {
		return memoryUpdatedAt
	}
	return userUpdatedAt
}

func (self *Summarizer) loadWorkspaceFileFromStore(scope models.Scope, scopeId string, relativePath string, maxChars int) string {
	content := ""
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		file, err := transaction.GetWorkspaceFileByPath(ctx, scope, scopeId, relativePath, nil)
		if err != nil || file.Content == nil {
			return nil
		}
		content = string(*file.Content)
		return nil
	}); err != nil {
		return ""
	}
	if len(content) > maxChars {
		content = content[:maxChars] + "\n... (truncated)"
	}
	return content
}

func (self *Summarizer) workspaceFileModifiedAt(scope models.Scope, scopeId string, relativePath string) time.Time {
	var modifiedAt time.Time
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		file, err := transaction.GetWorkspaceFileByPath(ctx, scope, scopeId, relativePath, nil)
		if err != nil || file.ModifiedAt == nil {
			return nil
		}
		modifiedAt = *file.ModifiedAt
		return nil
	})
	return modifiedAt
}

func (self *Summarizer) agentDescriptionSourceUpdatedAt(agentId string) time.Time {
	agentUpdatedAt := self.workspaceFileModifiedAt(models.ScopeAgent, agentId, "AGENT.md")
	memoryUpdatedAt := self.workspaceFileModifiedAt(models.ScopeAgent, agentId, "MEMORY.md")
	if memoryUpdatedAt.After(agentUpdatedAt) {
		return memoryUpdatedAt
	}
	return agentUpdatedAt
}

// --- Private helpers (duplicated from agents to avoid cross-package dependency) ---

// resolveIdentityLine determines the identity line for the system prompt.
func resolveIdentityLine(agentId string, agentName string) string {
	return fmt.Sprintf("%s %s", prompts.DefaultIdentityLine, agentIdentitySuffix(agentId, agentName))
}

// agentIdentitySuffix returns a sentence fragment identifying the agent by name
// and ID (e.g. "You are 'Research Assistant' (agent: research).") or just by ID
// when no friendly name is set.
func agentIdentitySuffix(agentId string, agentName string) string {
	if agentName != "" {
		return fmt.Sprintf("You are '%s' (agent: %s).", agentName, agentId)
	}
	return fmt.Sprintf("You are the '%s' agent.", agentId)
}

// conversationMessageContentText extracts the text content from a ConversationMessage.
func conversationMessageContentText(message models.ConversationMessage) string {
	if len(message.Content) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(message.Content, &text); err == nil {
		return text
	}
	return string(message.Content)
}

// findLastSummaryIndex returns the index of the last context_summary message
// in history, or -1 if none exists.
func findLastSummaryIndex(messages []*models.ConversationMessage) int {
	for index := len(messages) - 1; index >= 0; index-- {
		if string(messages[index].GetRole()) == "system" &&
			string(messages[index].GetStopReason()) == "context_summary" {
			return index
		}
	}
	return -1
}

// listConversationMessages loads all messages for a conversation from the store.
func listConversationMessages(ctx context.Context, conversationId string) ([]*models.ConversationMessage, error) {
	result := make([]*models.ConversationMessage, 0)
	err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		items, err := transaction.ListConversationMessages(ctx, conversationId, nil)
		if err != nil {
			return err
		}
		result = append(result, items...)
		return nil
	})
	if err == store.ErrNotFound {
		return nil, nil
	}
	return result, err
}

// messagesText builds a truncated text representation of conversation messages,
// collecting from the end to prioritize recent messages. Returns chronologically
// ordered text. Pass maxTotalChars <= 0 for no total limit.
func messagesText(messages []*models.ConversationMessage, maxTotalChars int, maxMessageChars int) string {
	var lines []string
	totalChars := 0

	for index := len(messages) - 1; index >= 0; index-- {
		role := string(messages[index].GetRole())
		toolName := messages[index].GetToolName()
		if role == "tool" && toolName != "" {
			role = fmt.Sprintf("tool(%s)", toolName)
		}

		content := conversationMessageContentText(*messages[index])
		if maxMessageChars > 0 && len(content) > maxMessageChars {
			content = content[:maxMessageChars] + "..."
		}

		line := fmt.Sprintf("[%s]: %s\n", role, content)
		if maxTotalChars > 0 && totalChars+len(line) > maxTotalChars {
			remaining := maxTotalChars - totalChars
			if remaining > 0 {
				lines = append(lines, line[:remaining])
			}
			break
		}
		lines = append(lines, line)
		totalChars += len(line)
	}

	// Reverse to chronological order.
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}

	return strings.Join(lines, "")
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
	if value == "" {
		return "(empty)"
	}
	return value
}

// --- Exported helpers for on-demand synthesis (used by memory tools) ---

// RunSynthesis resolves the summarizer provider, sends a system+user prompt
// pair, and returns the response text. Returns ("", false) on failure.
func (self *Summarizer) RunSynthesis(ctx context.Context, systemPrompt string, userPrompt string) (string, bool) {
	provider, modelName, ok := self.resolveSynthesisProvider()
	if !ok {
		return "", false
	}
	request := providers.ChatRequest{
		ModelName: modelName,
		Messages: []providers.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}
	requestContext, cancel := context.WithTimeout(ctx, summarizerRequestTimeout)
	defer cancel()
	response, err := provider.ChatCompletion(requestContext, request)
	if err != nil || len(response.Choices) == 0 {
		log.Debugf("summarizer: on-demand synthesis failed: %v", err)
		return "", false
	}
	content := response.Choices[0].Message.ContentText()
	if content == "" {
		return "", false
	}
	return content, true
}

// BuildMessagesText builds a truncated text representation of conversation
// messages, collecting from the end to prioritize recent messages. Returns
// chronologically ordered text.
func BuildMessagesText(messages []*models.ConversationMessage, maxTotalChars int, maxMessageChars int) string {
	return messagesText(messages, maxTotalChars, maxMessageChars)
}
