package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"encoding/base64"

	"github.com/kaptinlin/jsonrepair"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/security"
)

// conversationState holds per-conversation concurrency control channels.
type conversationState struct {
	lock chan struct{} // semaphore (buffered, size 1) — serializes runs
	done chan struct{} // closed on conversation cancellation — wakes all waiters
}

// Runner orchestrates: load conversation -> build prompt -> call LLM -> save response.
type Runner struct {
	mutex                sync.RWMutex
	AgentID              string
	Providers            *providers.Registry
	ResolveConversations func(userId, agentId string) *conversations.Store
	ResolveUserProfile   func(userId string) (*configs.UserProfile, error)
	Config               *configs.Config
	Tools                *ToolRegistry
	MediaStore           *media.Store
	WorkspaceDirectory   string
	SkillPrompts         string

	// contextWindows maps "provider:model" -> context window size.
	contextWindows sync.Map

	// conversationStates maps conversation id -> *conversationState for per-conversation serial execution.
	conversationStates sync.Map
}

// Reconfigure hot-swaps the runner's configuration, providers, tools, and skill prompts.
// In-progress runs continue with their snapshotted references; new runs use the updated values.
func (self *Runner) Reconfigure(config *configs.Config, providerRegistry *providers.Registry, tools *ToolRegistry, skillPrompts string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.Config = config
	self.Providers = providerRegistry
	self.Tools = tools
	self.SkillPrompts = skillPrompts
}

// Snapshot captures the runner's current state under the read lock.
func (self *Runner) Snapshot() (config *configs.Config, providerRegistry *providers.Registry, tools *ToolRegistry, workspaceDirectory string, skillPrompts string) {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.Config, self.Providers, self.Tools, self.WorkspaceDirectory, self.SkillPrompts
}

// SetModels populates the context window map for models from a given provider.
func (self *Runner) SetModels(providerName string, models []providers.ModelInfo) {
	for _, model := range models {
		if model.ContextLength > 0 {
			key := providers.QualifyModel(providerName, model.ID)
			self.contextWindows.Store(key, model.ContextLength)
		}
	}
}

// lookupContextWindow returns the context window for a qualified model, or 0 if unknown.
func (self *Runner) lookupContextWindow(qualifiedModel string) int {
	if value, ok := self.contextWindows.Load(qualifiedModel); ok {
		return value.(int)
	}
	return 0
}

// RunParams holds the parameters for a single agent run.
type RunParams struct {
	ConversationID     string
	Message            string
	Model              string // override config default
	Attachments        []conversations.Attachment
	SystemPromptSuffix string // optional; appended to system prompt for this run only
}

// RunResult holds the result of a completed agent run.
type RunResult struct {
	RunID         string
	Response      string
	Usage         *conversations.Usage
	Model         string
	StopReason    string
	ContextWindow int // context window size of the model used
}

// RunCallbacks receives events during an agent run.
type RunCallbacks struct {
	OnQueued     func() // called when the run must wait for the semaphore
	OnStart      func() // called after the semaphore is acquired, before execution
	OnTextDelta  func(text string)
	OnToolCall   func(toolName string, arguments string)
	OnToolResult func(toolName string, result string)
}

// getConversationState returns the conversationState for a given conversation id, creating one if needed.
func (self *Runner) getConversationState(conversationId string) *conversationState {
	state, _ := self.conversationStates.LoadOrStore(conversationId, &conversationState{
		lock: make(chan struct{}, 1),
		done: make(chan struct{}),
	})
	return state.(*conversationState)
}

// CancelConversation closes the done channel for the given conversation id, waking all queued
// goroutines so they return context.Canceled. A fresh conversationState is created on the
// next Run call via LoadOrStore.
func (self *Runner) CancelConversation(conversationId string) {
	if value, loaded := self.conversationStates.LoadAndDelete(conversationId); loaded {
		state := value.(*conversationState)
		close(state.done)
	}
}

// Run acquires a per-conversation semaphore so that only one run executes at a time per
// conversation id. Additional calls block until the current run completes. If the conversation
// is cancelled via CancelConversation, all waiting goroutines return context.Canceled.
func (self *Runner) Run(ctx context.Context, params RunParams, callbacks *RunCallbacks) (*RunResult, error) {
	state := self.getConversationState(params.ConversationID)

	// Try to acquire the semaphore without blocking first.
	select {
	case state.lock <- struct{}{}:
		// Acquired immediately.
	default:
		// Must wait — notify caller that this run is queued.
		if callbacks != nil && callbacks.OnQueued != nil {
			callbacks.OnQueued()
		}
		select {
		case state.lock <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-state.done:
			return nil, context.Canceled
		}
	}
	defer func() { <-state.lock }()

	// Re-check conversation cancellation after acquiring.
	select {
	case <-state.done:
		return nil, context.Canceled
	default:
	}

	// Notify caller that the run is starting (semaphore acquired).
	if callbacks != nil && callbacks.OnStart != nil {
		callbacks.OnStart()
	}

	return self.executeRun(ctx, params, callbacks)
}

// executeRun performs the actual chat turn: appends the user message, calls the LLM
// (streaming), and appends the assistant response. Loops when the LLM requests tool calls.
func (self *Runner) executeRun(ctx context.Context, params RunParams, callbacks *RunCallbacks) (*RunResult, error) {
	// Snapshot mutable fields so in-progress runs aren't affected by hot-reloads.
	configuration, providerRegistry, tools, workspaceDirectory, skillPrompts := self.Snapshot()
	userId := UserIDFromContext(ctx)
	isAdmin, hasAdminContext := AdminFromContext(ctx)
	if !hasAdminContext {
		// Non-gateway callers/tests may not set user role context.
		// Default to admin in that case to preserve existing behavior.
		isAdmin = true
	}
	if strings.TrimSpace(userId) == "" {
		return nil, fmt.Errorf("userId is required")
	}
	userWorkspaceDirectory := ""
	if resolvedUserWorkspaceDirectory, resolveErr := configs.UserWorkspaceDirectory(userId); resolveErr == nil {
		userWorkspaceDirectory = resolvedUserWorkspaceDirectory
	}
	if self.ResolveUserProfile == nil {
		return nil, fmt.Errorf("ResolveUserProfile is required")
	}
	profile, err := self.ResolveUserProfile(userId)
	if err != nil {
		return nil, fmt.Errorf("resolving user profile for %q: %w", userId, err)
	}
	if profile == nil {
		return nil, fmt.Errorf("user profile is required for user %q", userId)
	}
	conversationStore := self.conversationStore(userId)
	if conversationStore == nil {
		return nil, fmt.Errorf("conversation store is not configured")
	}

	runId := security.NewULID()
	now := time.Now().UnixMilli()

	log.Debugf("run start id=%s conversation=%s model=%s", runId, params.ConversationID, params.Model)

	// Enrich context with conversation id and runner for tools.
	ctx = ContextWithRun(ctx, params.ConversationID)
	ctx = contextWithRunner(ctx, self)

	// 1. Choose model and resolve provider (before appending, so we can stamp the header).
	qualifiedModel := configuration.AgentModel(self.AgentID)
	if params.Model != "" {
		qualifiedModel = params.Model
	}

	// Validate provider/model alignment with existing conversation.
	header, headerError := conversationStore.LoadHeader(params.ConversationID)
	if headerError == nil && header.Model != "" {
		// Existing conversation with a locked model.
		if params.Model != "" && params.Model != header.Model {
			return nil, fmt.Errorf("model mismatch: conversation %s is locked to %q but request specified %q",
				params.ConversationID, header.Model, params.Model)
		}
		qualifiedModel = header.Model
	}

	// Validate that we resolved a non-empty model.
	if qualifiedModel == "" {
		return nil, fmt.Errorf("no model configured: set a default model in config or specify one in the request")
	}

	limits := configuration.ResolveModelLimits(qualifiedModel)

	provider, model, err := providerRegistry.Resolve(qualifiedModel)
	if err != nil {
		return nil, fmt.Errorf("resolving model %q: %w", qualifiedModel, err)
	}
	resolvedProvider, _ := providers.ParseQualifiedModel(qualifiedModel, providerRegistry.DefaultProvider())

	// 2. Append user message to conversation (sets provider/model on new or backfills existing).
	var userMessage conversations.Message
	if len(params.Attachments) > 0 {
		userMessage = conversations.NewMessageWithAttachments("user", params.Message, params.Attachments, now)
	} else {
		userMessage = conversations.NewTextMessage("user", params.Message, now)
	}
	if err := conversationStore.Append(params.ConversationID, userMessage,
		conversations.WithProviderAndModel(resolvedProvider, qualifiedModel)); err != nil {
		return nil, fmt.Errorf("saving user message: %w", err)
	}

	// 3. Load full conversation history.
	history, err := conversationStore.Load(params.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("loading conversation: %w", err)
	}

	// 4. Tool-call loop.
	var totalUsage *conversations.Usage
	var responseText string
	var responseModel string
	var stopReason string
	var contextWindow int

	for round := 0; round < limits.MaxToolRounds; round++ {
		log.Debugf("run id=%s round=%d history_len=%d", runId, round, len(history))

		// Build messages for the LLM.
		llmMessages := self.buildMessages(history, limits, params.SystemPromptSuffix, configuration, userId, workspaceDirectory, userWorkspaceDirectory, skillPrompts, profile)

		// Tier 1: truncate old tool results.
		llmMessages = truncateOldToolResults(llmMessages, limits.MinKeepMessages, limits.MaxToolResultChars)

		// Build tool definitions for the request.
		var toolDefs []providers.ToolDefinition
		if tools != nil {
			toolDefs = tools.Definitions()
		}

		// Tier 2: compress context if approaching the context window limit.
		// Use per-model context window if available, otherwise fall back to configs.
		contextWindow = self.lookupContextWindow(qualifiedModel)
		if contextWindow <= 0 {
			contextWindow = configuration.Models.ContextWindow
		}
		llmMessages, err = self.compressContext(ctx, providerRegistry, configuration, llmMessages, toolDefs, params.ConversationID, contextWindow, limits)
		if err != nil {
			log.Debugf("context compression error (non-fatal): %v", err)
		}

		// Build request.
		request := providers.ChatRequest{
			Model:         model,
			Messages:      llmMessages,
			Stream:        true,
			StreamOptions: &providers.StreamOptions{IncludeUsage: true},
		}
		if len(toolDefs) > 0 {
			request.Tools = toolDefs
		}

		// Call LLM with streaming.
		stream, err := provider.ChatCompletionStream(ctx, request)
		if err != nil {
			return nil, fmt.Errorf("calling LLM: %w", err)
		}

		// Collect response + tool call deltas.
		var textBuilder strings.Builder
		var toolCalls []providers.ToolCall
		toolCallMap := make(map[int]*providers.ToolCall)
		var usage *providers.UsageInfo

		for event := range stream {
			if ctx.Err() != nil {
				break
			}
			if event.Err != nil {
				return nil, fmt.Errorf("stream error: %w", event.Err)
			}
			if event.Done {
				break
			}
			if event.Chunk == nil {
				continue
			}

			if responseModel == "" {
				responseModel = event.Chunk.Model
			}
			if event.Chunk.Usage != nil {
				if usage == nil {
					usage = &providers.UsageInfo{}
				}
				usage.PromptTokens += event.Chunk.Usage.PromptTokens
				usage.CompletionTokens += event.Chunk.Usage.CompletionTokens
				usage.TotalTokens += event.Chunk.Usage.TotalTokens
				usage.CacheCreationInputTokens += event.Chunk.Usage.CacheCreationInputTokens
				usage.CacheReadInputTokens += event.Chunk.Usage.CacheReadInputTokens
			}

			for _, choice := range event.Chunk.Choices {
				if choice.Delta.Content != "" {
					textBuilder.WriteString(choice.Delta.Content)
					if callbacks != nil && callbacks.OnTextDelta != nil {
						callbacks.OnTextDelta(choice.Delta.Content)
					}
				}
				if choice.FinishReason != "" {
					stopReason = choice.FinishReason
				}

				// Assemble tool call deltas by index.
				for _, toolCallDelta := range choice.Delta.ToolCalls {
					toolCall, ok := toolCallMap[toolCallDelta.Index]
					if !ok {
						toolCall = &providers.ToolCall{Type: "function"}
						toolCallMap[toolCallDelta.Index] = toolCall
					}
					if toolCallDelta.ID != "" {
						toolCall.ID = toolCallDelta.ID
					}
					if toolCallDelta.Function.Name != "" {
						toolCall.Function.Name += toolCallDelta.Function.Name
					}
					if toolCallDelta.Function.Arguments != "" {
						toolCall.Function.Arguments += toolCallDelta.Function.Arguments
					}
				}
			}
		}

		// Flatten tool call map to sorted slice.
		if len(toolCallMap) > 0 {
			toolCalls = make([]providers.ToolCall, len(toolCallMap))
			for index, toolCall := range toolCallMap {
				toolCalls[index] = *toolCall
			}
		}

		// Accumulate usage.
		if usage != nil {
			if totalUsage == nil {
				totalUsage = &conversations.Usage{}
			}
			totalUsage.Input += usage.PromptTokens
			totalUsage.Output += usage.CompletionTokens
			totalUsage.Total += usage.TotalTokens
			totalUsage.CacheCreated += usage.CacheCreationInputTokens
			totalUsage.CacheRead += usage.CacheReadInputTokens
		}

		responseText = textBuilder.String()

		// Save assistant message.
		assistantMessage := conversations.NewTextMessage("assistant", responseText, time.Now().UnixMilli())
		assistantMessage.Model = responseModel
		assistantMessage.Provider = resolvedProvider
		assistantMessage.StopReason = stopReason
		if usage != nil {
			assistantMessage.Usage = &conversations.Usage{
				Input:        usage.PromptTokens,
				Output:       usage.CompletionTokens,
				Total:        usage.TotalTokens,
				CacheCreated: usage.CacheCreationInputTokens,
				CacheRead:    usage.CacheReadInputTokens,
			}
		}
		if len(toolCalls) > 0 {
			assistantMessage.ToolCalls, _ = json.Marshal(toolCalls)
		}

		if err := conversationStore.Append(params.ConversationID, assistantMessage); err != nil {
			return nil, fmt.Errorf("saving assistant message: %w", err)
		}
		history = append(history, assistantMessage)

		// If no tool calls, we're done.
		if len(toolCalls) == 0 || tools == nil {
			break
		}

		// Phase 1 — Filter & notify: resolve tools, fire OnToolCall callbacks in order.
		type toolWorkItem struct {
			toolCall  providers.ToolCall
			tool      Tool
			arguments string
		}

		var workItems []toolWorkItem
		for _, toolCall := range toolCalls {
			tool := tools.Get(toolCall.Function.Name)
			if tool == nil {
				continue
			}

			if callbacks != nil && callbacks.OnToolCall != nil {
				callbacks.OnToolCall(toolCall.Function.Name, toolCall.Function.Arguments)
			}

			arguments := repairToolArgs(toolCall.Function.Arguments)
			if authorizationErr := validateToolAuthorization(toolCall.Function.Name, arguments, isAdmin); authorizationErr != nil {
				result := "error: " + authorizationErr.Error()
				log.Debugf("tool denied id=%s name=%s err=%v", toolCall.ID, toolCall.Function.Name, authorizationErr)
				if callbacks != nil && callbacks.OnToolResult != nil {
					callbacks.OnToolResult(toolCall.Function.Name, result)
				}
				toolMessage := conversations.NewToolMessage(toolCall.ID, toolCall.Function.Name, result, time.Now().UnixMilli())
				if err := conversationStore.Append(params.ConversationID, toolMessage); err != nil {
					return nil, fmt.Errorf("saving tool denial: %w", err)
				}
				history = append(history, toolMessage)
				continue
			}
			workItems = append(workItems, toolWorkItem{
				toolCall:  toolCall,
				tool:      tool,
				arguments: arguments,
			})
		}

		if len(workItems) == 0 {
			break
		}

		// Phase 2 — Parallel execute: run all tool calls concurrently.
		// Each goroutine writes to its own index, so no mutex is needed.
		type toolResult struct {
			result string
			err    error
		}

		results := make([]toolResult, len(workItems))
		var waitGroup sync.WaitGroup
		waitGroup.Add(len(workItems))
		for position, item := range workItems {
			go func(position int, item toolWorkItem) {
				defer waitGroup.Done()
				log.Debugf("tool call id=%s name=%s", item.toolCall.ID, item.toolCall.Function.Name)
				result, executeError := item.tool.Execute(ctx, item.arguments)
				results[position] = toolResult{result: result, err: executeError}
			}(position, item)
		}
		waitGroup.Wait()

		// Phase 3 — Sequential persist: process results in original order
		// so conversation JSONL ordering and callbacks remain deterministic.
		for position, item := range workItems {
			result := results[position].result
			if results[position].err != nil {
				log.Debugf("tool error id=%s name=%s err=%v", item.toolCall.ID, item.toolCall.Function.Name, results[position].err)
				result = "error: " + results[position].err.Error()
			} else {
				log.Debugf("tool done id=%s name=%s result_len=%d", item.toolCall.ID, item.toolCall.Function.Name, len(result))
			}

			// Detect media in tool result. If base64 media is found and we
			// have a media store, save to disk and create a compact reference
			// for the conversation file and LLM history, while sending the original
			// (with base64) to live consumers via OnToolResult.
			liveResult := result
			storedResult := result
			if self.MediaStore != nil {
				if detected := media.DetectMedia(result); detected != nil && detected.Base64 != "" && media.IsImageFormat(detected.Format) {
					rawData, decodeError := base64.StdEncoding.DecodeString(detected.Base64)
					if decodeError == nil {
						saved, saveError := self.MediaStore.Save(rawData, detected.Format, media.SaveOptions{
							SourceType:     "tool",
							AgentID:        self.AgentID,
							ConversationID: params.ConversationID,
							ToolName:       item.toolCall.Function.Name,
							ToolCallID:     item.toolCall.ID,
						})
						if saveError == nil {
							ref, _ := json.Marshal(map[string]interface{}{
								"mediaId":   saved.MediaID,
								"format":    detected.Format,
								"displayed": true,
							})
							storedResult = string(ref)
						}
					}
				}
			}

			if callbacks != nil && callbacks.OnToolResult != nil {
				callbacks.OnToolResult(item.toolCall.Function.Name, liveResult)
			}

			toolMessage := conversations.NewToolMessage(item.toolCall.ID, item.toolCall.Function.Name, storedResult, time.Now().UnixMilli())
			if err := conversationStore.Append(params.ConversationID, toolMessage); err != nil {
				return nil, fmt.Errorf("saving tool result: %w", err)
			}
			history = append(history, toolMessage)
		}
	}

	log.Debugf("run done id=%s model=%s stop=%s", runId, responseModel, stopReason)

	return &RunResult{
		RunID:         runId,
		Response:      responseText,
		Usage:         totalUsage,
		Model:         responseModel,
		StopReason:    stopReason,
		ContextWindow: contextWindow,
	}, nil
}

func (self *Runner) conversationStore(userId string) *conversations.Store {
	self.mutex.RLock()
	resolver := self.ResolveConversations
	self.mutex.RUnlock()
	if resolver == nil {
		return nil
	}
	return resolver(userId, self.AgentID)
}

// ConversationsForUser resolves the conversation store for a user.
func (self *Runner) ConversationsForUser(userId string) *conversations.Store {
	return self.conversationStore(userId)
}

// repairToolArgs attempts to fix malformed JSON from the LLM.
// It only runs the repair if the JSON is actually invalid.
func repairToolArgs(input string) string {
	if json.Valid([]byte(input)) {
		return input
	}
	fixed, err := jsonrepair.JSONRepair(input)
	if err != nil {
		return input
	}
	return fixed
}

func validateToolAuthorization(toolName, arguments string, isAdmin bool) error {
	if isAdmin {
		return nil
	}
	switch toolName {
	case "shell":
		return fmt.Errorf("admin access required for shell tool")
	case "agent_create":
		return fmt.Errorf("admin access required for agent_create")
	case "config":
		action := parseToolAction(arguments)
		if action == "set" {
			return fmt.Errorf("admin access required for config.set")
		}
	case "projects":
		action := parseToolAction(arguments)
		if action != "list" && action != "info" {
			if action == "" {
				return fmt.Errorf("admin access required for projects management actions")
			}
			return fmt.Errorf("admin access required for projects.%s", action)
		}
	case "project_workspace":
		action := parseToolAction(arguments)
		if action != "list" && action != "read" && action != "search" {
			if action == "" {
				return fmt.Errorf("admin access required for project_workspace management actions")
			}
			return fmt.Errorf("admin access required for project_workspace.%s", action)
		}
	case "skills":
		action := parseToolAction(arguments)
		if action != "list_registry" && action != "search" && action != "list_installed" {
			if action == "" {
				return fmt.Errorf("admin access required for skills management actions")
			}
			return fmt.Errorf("admin access required for skills.%s", action)
		}
	case "filesystem":
		action := parseToolAction(arguments)
		if action != "read" && action != "list" && action != "info" {
			if action == "" {
				return fmt.Errorf("admin access required for filesystem write actions")
			}
			return fmt.Errorf("admin access required for filesystem.%s", action)
		}
	}
	return nil
}

func parseToolAction(arguments string) string {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return ""
	}
	action, _ := payload["action"].(string)
	return strings.TrimSpace(strings.ToLower(action))
}

// buildMessages converts conversation history into LLM messages.
// It scans backward for the last context_summary message and skips everything before it.
func (self *Runner) buildMessages(
	history []conversations.Message,
	limits configs.AgentLimits,
	systemPromptSuffix string,
	configuration *configs.Config,
	currentUserId string,
	agentWorkspaceDirectory string,
	userWorkspaceDirectory string,
	skillPrompts string,
	profile *configs.UserProfile,
) []providers.ChatMessage {
	systemPrompt := BuildSystemPrompt(
		configuration,
		self.AgentID,
		currentUserId,
		agentWorkspaceDirectory,
		userWorkspaceDirectory,
		skillPrompts,
		limits.MaxWorkspaceFileChars,
		profile,
	)
	if systemPromptSuffix != "" {
		systemPrompt += "\n\n" + systemPromptSuffix
	}
	messages := make([]providers.ChatMessage, 0, len(history)+1)
	messages = append(messages, providers.ChatMessage{
		Role:    "system",
		Content: systemPrompt,
	})

	// Find the last context summary and start from there.
	startIndex := 0
	if idx := findLastSummaryIndex(history); idx >= 0 {
		messages = append(messages, providers.ChatMessage{
			Role:    "system",
			Content: "Previous conversation summary:\n" + history[idx].ContentText(),
		})
		startIndex = idx + 1
	}

	// Skip the remainder of any in-progress run after the summary. When a
	// summary is appended mid-run (e.g. conversation_compact tool or
	// auto-compression), messages from that run follow the summary on disk.
	// Advance past the next complete LLM turn (an assistant message with a
	// terminal stopReason) so we start from a clean boundary.
	if startIndex < len(history) && history[startIndex].Role != "user" {
		for startIndex < len(history) {
			message := history[startIndex]
			startIndex++
			if message.StopReason != "" && message.StopReason != "tool_calls" && message.StopReason != "context_summary" {
				break
			}
		}
	}

	for _, message := range history[startIndex:] {
		chatMessage := providers.ChatMessage{
			Role: message.Role,
		}

		// Check for multimodal content (attachments).
		blocks := message.ContentBlocks()
		hasAttachments := false
		for _, block := range blocks {
			if block.Type == "attachment" {
				hasAttachments = true
				break
			}
		}

		if hasAttachments && message.Role == "user" {
			chatMessage.Content = self.buildMultimodalContent(blocks)
		} else {
			chatMessage.Content = message.ContentText()
		}

		// Attach tool calls on assistant messages.
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			var toolCalls []providers.ToolCall
			if err := json.Unmarshal(message.ToolCalls, &toolCalls); err == nil {
				chatMessage.ToolCalls = toolCalls
			}
		}

		// Attach tool metadata on tool result messages.
		if message.Role == "tool" {
			chatMessage.ToolCallID = message.ToolCallID
			chatMessage.Name = message.ToolName
		}

		messages = append(messages, chatMessage)
	}

	// Fix interrupted tool calls: if an assistant message has tool_calls but
	// some or all lack corresponding tool result messages, the LLM API will
	// reject the request. Append synthetic tool results for any unanswered calls.
	messages = fixInterruptedToolCalls(messages)

	return messages
}

// fixInterruptedToolCalls scans for assistant messages with tool_calls whose
// IDs have no corresponding tool result message following them. For each
// missing result, a synthetic tool message is appended so the LLM API does
// not reject the request.
func fixInterruptedToolCalls(messages []providers.ChatMessage) []providers.ChatMessage {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != "assistant" || len(message.ToolCalls) == 0 {
			continue
		}

		// Collect tool_call IDs that have results after this assistant message.
		answered := make(map[string]bool)
		for j := i + 1; j < len(messages); j++ {
			if messages[j].Role == "tool" && messages[j].ToolCallID != "" {
				answered[messages[j].ToolCallID] = true
			}
			// Stop at the next assistant or user message — tool results only
			// apply between this assistant message and the next turn.
			if messages[j].Role == "assistant" || messages[j].Role == "user" {
				break
			}
		}

		// Append synthetic results for any unanswered tool calls.
		var synthetic []providers.ChatMessage
		for _, tc := range message.ToolCalls {
			if !answered[tc.ID] {
				synthetic = append(synthetic, providers.ChatMessage{
					Role:       "tool",
					Content:    "Tool call was interrupted and did not complete.",
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
				})
			}
		}

		if len(synthetic) > 0 {
			// Insert synthetic results right after the existing tool results
			// for this assistant message (or right after the assistant message).
			insertAt := i + 1
			for insertAt < len(messages) && messages[insertAt].Role == "tool" {
				insertAt++
			}
			result := make([]providers.ChatMessage, 0, len(messages)+len(synthetic))
			result = append(result, messages[:insertAt]...)
			result = append(result, synthetic...)
			result = append(result, messages[insertAt:]...)
			messages = result
		}
	}
	return messages
}

// buildMultimodalContent converts conversation ContentBlocks into provider ContentParts.
// Image attachments are sent as image_url parts with base64 data URIs.
// Non-image attachments are included as text references.
func (self *Runner) buildMultimodalContent(blocks []conversations.ContentBlock) []providers.ContentPart {
	var parts []providers.ContentPart

	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, providers.ContentPart{Type: "text", Text: block.Text})
			}
		case "attachment":
			if media.IsImageFormat(block.Format) {
				imageUrl := self.resolveMediaUrl(block.MediaID, block.Format)
				if imageUrl != "" {
					parts = append(parts, providers.ContentPart{
						Type:     "image_url",
						ImageURL: &providers.ImageURLPart{URL: imageUrl},
					})
				}
			} else {
				// Non-image: include as text reference.
				label := block.Filename
				if label == "" {
					label = block.MediaID
				}
				parts = append(parts, providers.ContentPart{
					Type: "text",
					Text: fmt.Sprintf("[Attached file: %s (%s)]", label, block.Format),
				})
			}
		}
	}

	if len(parts) == 0 {
		parts = append(parts, providers.ContentPart{Type: "text", Text: ""})
	}
	return parts
}

// resolveMediaUrl returns a base64 data URI for a media file.
func (self *Runner) resolveMediaUrl(mediaId, format string) string {
	if self.MediaStore == nil {
		return ""
	}
	data, metadata, err := self.MediaStore.Load(mediaId)
	if err != nil {
		log.Debugf("failed to load media %s for multimodal: %v", mediaId, err)
		return ""
	}
	mimeType := media.MimeType(metadata.Format)
	encoded := base64.StdEncoding.EncodeToString(data)
	return "data:" + mimeType + ";base64," + encoded
}
