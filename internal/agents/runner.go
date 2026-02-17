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
	"github.com/teanode/teanode/internal/provider"
	"github.com/teanode/teanode/internal/util/ulid"
)

// conversationState holds per-conversation concurrency control channels.
type conversationState struct {
	lock chan struct{} // semaphore (buffered, size 1) — serializes runs
	done chan struct{} // closed on conversation cancellation — wakes all waiters
}

// Runner orchestrates: load conversation -> build prompt -> call LLM -> save response.
type Runner struct {
	mutex              sync.RWMutex
	AgentID            string
	Providers          *provider.Registry
	Conversations      *conversations.Store
	Config             *configs.Config
	Tools              *ToolRegistry
	MediaStore         *media.Store
	WorkspaceDirectory string
	SkillPrompts       string

	// contextWindows maps "provider:model" -> context window size.
	contextWindows sync.Map

	// conversationStates maps conversation id -> *conversationState for per-conversation serial execution.
	conversationStates sync.Map
}

// Reconfigure hot-swaps the runner's configuration, providers, tools, and skill prompts.
// In-progress runs continue with their snapshotted references; new runs use the updated values.
func (self *Runner) Reconfigure(config *configs.Config, providers *provider.Registry, tools *ToolRegistry, skillPrompts string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.Config = config
	self.Providers = providers
	self.Tools = tools
	self.SkillPrompts = skillPrompts
}

// Snapshot captures the runner's current state under the read lock.
func (self *Runner) Snapshot() (config *configs.Config, providers *provider.Registry, tools *ToolRegistry, workspaceDirectory string, skillPrompts string) {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.Config, self.Providers, self.Tools, self.WorkspaceDirectory, self.SkillPrompts
}

// SetModels populates the context window map for models from a given provider.
func (self *Runner) SetModels(providerName string, models []provider.ModelInfo) {
	for _, m := range models {
		if m.ContextLength > 0 {
			key := provider.QualifyModel(providerName, m.ID)
			self.contextWindows.Store(key, m.ContextLength)
		}
	}
}

// lookupContextWindow returns the context window for a qualified model, or 0 if unknown.
func (self *Runner) lookupContextWindow(qualifiedModel string) int {
	if v, ok := self.contextWindows.Load(qualifiedModel); ok {
		return v.(int)
	}
	return 0
}

// RunParams holds the parameters for a single agent run.
type RunParams struct {
	ConversationID string
	Message        string
	Model          string // override config default
}

// RunResult holds the result of a completed agent run.
type RunResult struct {
	RunID      string
	Response   string
	Usage      *conversations.Usage
	Model      string
	StopReason string
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
	configuration, providers, tools, _, _ := self.Snapshot()

	// Resolve per-agent limits (falls back to defaults for unconfigured agents).
	var limits configs.AgentLimits
	if agentConfig := configuration.AgentByID(self.AgentID); agentConfig != nil {
		limits = agentConfig.ResolveLimits()
	} else {
		limits = configs.DefaultAgentLimits
	}

	runId := ulid.GenerateString()
	now := time.Now().UnixMilli()

	log.Debugf("run start id=%s conversation=%s model=%s", runId, params.ConversationID, params.Model)

	// Enrich context with conversation id for tools.
	ctx = ContextWithRun(ctx, params.ConversationID)

	// 1. Append user message to conversation.
	userMessage := conversations.NewTextMessage("user", params.Message, now)
	if err := self.Conversations.Append(params.ConversationID, userMessage); err != nil {
		return nil, fmt.Errorf("saving user message: %w", err)
	}

	// 2. Load full conversation history.
	history, err := self.Conversations.Load(params.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("loading conversation: %w", err)
	}

	// 3. Choose model and resolve provider.
	qualifiedModel := configuration.AgentModel(self.AgentID)
	if params.Model != "" {
		qualifiedModel = params.Model
	}
	providerClient, model, err := providers.Resolve(qualifiedModel)
	if err != nil {
		return nil, fmt.Errorf("resolving model %q: %w", qualifiedModel, err)
	}
	resolvedProvider, _ := provider.ParseQualifiedModel(qualifiedModel, providers.DefaultProvider())

	// 4. Tool-call loop.
	var totalUsage *conversations.Usage
	var responseText string
	var responseModel string
	var stopReason string

	for round := 0; round < limits.MaxToolRounds; round++ {
		log.Debugf("run id=%s round=%d history_len=%d", runId, round, len(history))

		// Build messages for the LLM.
		llmMessages := self.buildMessages(history, limits)

		// Tier 1: truncate old tool results.
		llmMessages = truncateOldToolResults(llmMessages, limits.MinKeepMessages, limits.MaxToolResultChars)

		// Build tool definitions for the request.
		var toolDefs []provider.ToolDefinition
		if tools != nil {
			toolDefs = tools.Definitions()
		}

		// Tier 2: compress context if approaching the context window limit.
		// Use per-model context window if available, otherwise fall back to configs.
		contextWindow := self.lookupContextWindow(qualifiedModel)
		if contextWindow <= 0 {
			contextWindow = configuration.Models.ContextWindow
		}
		llmMessages, err = self.compressContext(ctx, providers, configuration, llmMessages, toolDefs, params.ConversationID, contextWindow, limits)
		if err != nil {
			log.Debugf("context compression error (non-fatal): %v", err)
		}

		// Build request.
		request := provider.ChatRequest{
			Model:         model,
			Messages:      llmMessages,
			Stream:        true,
			StreamOptions: &provider.StreamOptions{IncludeUsage: true},
		}
		if len(toolDefs) > 0 {
			request.Tools = toolDefs
		}

		// Call LLM with streaming.
		stream, err := providerClient.ChatCompletionStream(ctx, request)
		if err != nil {
			return nil, fmt.Errorf("calling LLM: %w", err)
		}

		// Collect response + tool call deltas.
		var textBuilder strings.Builder
		var toolCalls []provider.ToolCall
		toolCallMap := make(map[int]*provider.ToolCall)
		var usage *provider.UsageInfo

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
				usage = event.Chunk.Usage
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
						toolCall = &provider.ToolCall{Type: "function"}
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
			toolCalls = make([]provider.ToolCall, len(toolCallMap))
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
		}

		responseText = textBuilder.String()

		// Save assistant message.
		assistantMessage := conversations.NewTextMessage("assistant", responseText, time.Now().UnixMilli())
		assistantMessage.Model = responseModel
		assistantMessage.Provider = resolvedProvider
		assistantMessage.StopReason = stopReason
		if usage != nil {
			assistantMessage.Usage = &conversations.Usage{
				Input:  usage.PromptTokens,
				Output: usage.CompletionTokens,
				Total:  usage.TotalTokens,
			}
		}
		if len(toolCalls) > 0 {
			assistantMessage.ToolCalls, _ = json.Marshal(toolCalls)
		}

		if err := self.Conversations.Append(params.ConversationID, assistantMessage); err != nil {
			return nil, fmt.Errorf("saving assistant message: %w", err)
		}
		history = append(history, assistantMessage)

		// If no tool calls, we're done.
		if len(toolCalls) == 0 || tools == nil {
			break
		}

		// Phase 1 — Filter & notify: resolve tools, fire OnToolCall callbacks in order.
		type toolWorkItem struct {
			toolCall  provider.ToolCall
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
						mediaId, saveError := self.MediaStore.Save(rawData, detected.Format)
						if saveError == nil {
							ref, _ := json.Marshal(map[string]interface{}{
								"mediaId":   mediaId,
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
			if err := self.Conversations.Append(params.ConversationID, toolMessage); err != nil {
				return nil, fmt.Errorf("saving tool result: %w", err)
			}
			history = append(history, toolMessage)
		}
	}

	log.Debugf("run done id=%s model=%s stop=%s", runId, responseModel, stopReason)

	return &RunResult{
		RunID:      runId,
		Response:   responseText,
		Usage:      totalUsage,
		Model:      responseModel,
		StopReason: stopReason,
	}, nil
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

// buildMessages converts conversation history into LLM messages.
// It scans backward for the last context_summary message and skips everything before it.
func (self *Runner) buildMessages(history []conversations.Message, limits configs.AgentLimits) []provider.ChatMessage {
	systemPrompt := BuildSystemPrompt(self.Config, self.AgentID, self.WorkspaceDirectory, self.SkillPrompts, limits.MaxWorkspaceFileChars)
	messages := make([]provider.ChatMessage, 0, len(history)+1)
	messages = append(messages, provider.ChatMessage{
		Role:    "system",
		Content: systemPrompt,
	})

	// Find the last context summary and start from there.
	startIndex := 0
	for index := len(history) - 1; index >= 0; index-- {
		if history[index].Role == "system" && history[index].StopReason == "context_summary" {
			messages = append(messages, provider.ChatMessage{
				Role:    "system",
				Content: "Previous conversation summary:\n" + history[index].ContentText(),
			})
			startIndex = index + 1
			break
		}
	}

	for _, message := range history[startIndex:] {
		chatMessage := provider.ChatMessage{
			Role:    message.Role,
			Content: message.ContentText(),
		}

		// Attach tool calls on assistant messages.
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			var toolCalls []provider.ToolCall
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

	return messages
}
