package runners

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"encoding/base64"

	"github.com/kaptinlin/jsonrepair"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/skills"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/mimetypes"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

// Runner orchestrates: load conversation -> build prompt -> call LLM -> save response.
type Runner struct {
	ID               string
	AgentID          string
	ConversationID   string
	providerRegistry *providers.ProviderRegistry
	toolRegistry     *tools.ToolRegistry
	skillPrompts     string
}

// NewRunner creates a new Runner. It builds the tool registry from the agent's
// configured skills and tool allow-list.
func NewRunner(ctx context.Context, agentId, conversationId string, providerRegistry *providers.ProviderRegistry, agent models.Agent) *Runner {
	toolRegistry := tools.NewToolRegistry()
	skillPrompts := skills.RegisterSkills(ctx, toolRegistry, agent.GetSkills())
	toolRegistry.ApplyFilter(agent.GetTools())
	return &Runner{
		ID:               security.NewULID(),
		AgentID:          agentId,
		ConversationID:   conversationId,
		providerRegistry: providerRegistry,
		toolRegistry:     toolRegistry,
		skillPrompts:     skillPrompts,
	}
}

// RunParameters holds the parameters for a single agent run.
type RunParameters struct {
	Message            string
	ProviderModelName  string // override config default
	Attachments        []map[string]string
	SystemPromptSuffix string // optional; appended to system prompt for this run only
	SystemPromptMode   SystemPromptMode
	Origin             string // channel origin (e.g. "webui", "telegram"); propagated to context for tool gating
}

// RunResult holds the result of a completed agent run.
type RunResult struct {
	Response          string
	Usage             map[string]int
	ProviderModelName string
	StopReason        string
	ContextWindow     int // context window size of the model used
}

// RunCallbacks receives events during an agent run.
type RunCallbacks struct {
	OnQueued     func() // called when the run must wait for the semaphore
	OnTextDelta  func(text string)
	OnTextDone   func(text string) // fired when text streaming ends and tool calls follow
	OnToolCall   func(toolName string, arguments string)
	OnToolResult func(toolName string, result string)
}

// Run directly executes a run for this runner's conversation.
func (self *Runner) Run(ctx context.Context, params RunParameters, callbacks *RunCallbacks) (*RunResult, error) {
	return self.executeRun(ctx, params, callbacks)
}

// executeRun performs the actual chat turn: appends the user message, calls the LLM
// (streaming), and appends the assistant response. Loops when the LLM requests tool calls.
func (self *Runner) executeRun(ctx context.Context, params RunParameters, callbacks *RunCallbacks) (*RunResult, error) {
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return nil, fmt.Errorf("userId is required")
	}
	userId := user.ID
	isAdmin := user.GetAdmin()

	log.Debugf("run %q start agent %q conversation %q model %q", self.ID, self.AgentID, self.ConversationID, params.ProviderModelName)

	// Enrich context with conversation id and runner for tools.
	ctx = ContextWithRunner(ctx, self)

	// 1. Choose model and resolve provider (before appending, so we can stamp the header).
	providerModelName, _ := self.resolveAgentProviderModelAndName(ctx)
	if params.ProviderModelName != "" {
		providerModelName = params.ProviderModelName
	}

	// Validate provider/model alignment with existing conversation.
	conversationMessagesForHeader, headerError := listConversationMessages(ctx, self.ConversationID)
	if headerError == nil {
		existingProviderModelName := ""
		for index := len(conversationMessagesForHeader) - 1; index >= 0; index-- {
			existingProviderModelName = conversationMessagesForHeader[index].GetProviderModelName()
			if existingProviderModelName != "" {
				break
			}
		}
		if existingProviderModelName != "" {
			// Existing conversation with a locked model.
			if params.ProviderModelName != "" && params.ProviderModelName != existingProviderModelName {
				return nil, fmt.Errorf("model mismatch: conversation %s is locked to %q but request specified %q",
					self.ConversationID, existingProviderModelName, params.ProviderModelName)
			}
			providerModelName = existingProviderModelName
		}
	}

	provider, providerName, modelName, err := self.providerRegistry.ResolveProviderAndModel(providerModelName)
	if err != nil {
		return nil, fmt.Errorf("resolving model %q: %w", providerModelName, err)
	}

	// 2. Append user message to conversation (sets provider/model on new or backfills existing).
	var userMessage models.ConversationMessage
	if len(params.Attachments) > 0 {
		userMessage = newMessageWithAttachments("user", params.Message, params.Attachments)
	} else {
		userMessage = newTextMessage("user", params.Message)
	}
	userMessage.ProviderName = ptrto.Value(providerName)
	userMessage.ProviderModelName = ptrto.Value(providers.FormatProviderModelName(providerName, modelName))
	if err := self.appendConversationMessage(ctx, userMessage); err != nil {
		return nil, fmt.Errorf("saving user message: %w", err)
	}

	// 3. Load full conversation history.
	history, err := listConversationMessages(ctx, self.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("loading conversation: %w", err)
	}

	// 4. Tool-call loop.
	var totalUsage map[string]int
	var responseText string
	var responseModelName string
	var stopReason string
	contextWindow := self.resolveContextWindow(ctx)

	for round := 0; round < 250; round++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		log.Debugf("run id %q round %d history %d", self.ID, round, len(history))

		// Build messages for the LLM.
		llmMessages := self.buildMessages(ctx, history, params.SystemPromptSuffix, params.SystemPromptMode, self.skillPrompts)

		// Tier 1: truncate old tool results.
		llmMessages = truncateOldToolResults(llmMessages, 10, 8000)

		// Build tool definitions for the request.
		toolDefinitions := self.toolRegistry.Definitions()

		// Tier 2: compress context if approaching the context window limit.
		// Re-read models configuration for fresh values.
		llmMessages, err = self.compressContext(
			ctx,
			llmMessages,
			toolDefinitions,
			contextCompressionLimits{
				CompressionThreshold: 0.8,
				MinKeepMessages:      10,
				MinKeepRecentTokens:  8000,
			},
		)
		if err != nil {
			log.Debugf("context compression error (non-fatal): %v", err)
		}

		// Build request.
		request := providers.ChatRequest{
			ModelName:     modelName,
			Messages:      llmMessages,
			Stream:        true,
			StreamOptions: &providers.StreamOptions{IncludeUsage: true},
		}
		if len(toolDefinitions) > 0 {
			request.Tools = toolDefinitions
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
		var usage *providers.UsageInformation

		var streamError error
		for event := range stream {
			if ctx.Err() != nil {
				break
			}
			if event.Err != nil {
				streamError = fmt.Errorf("stream error: %w", event.Err)
				break
			}
			if event.Done {
				break
			}
			if event.Chunk == nil {
				continue
			}

			if responseModelName == "" {
				responseModelName = event.Chunk.ModelName
			}
			if event.Chunk.Usage != nil {
				if usage == nil {
					usage = &providers.UsageInformation{}
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

		// Drain any remaining stream events to prevent the producer goroutine from blocking.
		for range stream {
		}

		if streamError != nil {
			return nil, streamError
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Flatten tool call map to sorted slice.
		if len(toolCallMap) > 0 {
			indices := make([]int, 0, len(toolCallMap))
			for index := range toolCallMap {
				indices = append(indices, index)
			}
			sort.Ints(indices)
			toolCalls = make([]providers.ToolCall, len(indices))
			for position, index := range indices {
				toolCalls[position] = *toolCallMap[index]
			}
		}

		// Accumulate usage.
		if usage != nil {
			if totalUsage == nil {
				totalUsage = map[string]int{}
			}
			totalUsage["input"] += usage.PromptTokens
			totalUsage["output"] += usage.CompletionTokens
			totalUsage["totalTokens"] += usage.TotalTokens
			totalUsage["cacheCreated"] += usage.CacheCreationInputTokens
			totalUsage["cacheRead"] += usage.CacheReadInputTokens
		}

		responseText = textBuilder.String()

		// Signal that text streaming is done before tool calls begin.
		if len(toolCalls) > 0 && callbacks != nil && callbacks.OnTextDone != nil {
			callbacks.OnTextDone(responseText)
		}

		// Save assistant message.
		assistantMessage := newTextMessage("assistant", responseText)
		assistantMessage.ProviderModelName = ptrto.Value(providers.FormatProviderModelName(providerName, responseModelName))
		assistantMessage.ProviderName = ptrto.Value(providerName)
		if stopReason != "" {
			stopReasonValue := models.StopReason(stopReason)
			assistantMessage.StopReason = &stopReasonValue
		}
		usageMap := map[string]int(nil)
		if usage != nil {
			usageMap = map[string]int{
				"input":        usage.PromptTokens,
				"output":       usage.CompletionTokens,
				"totalTokens":  usage.TotalTokens,
				"cacheCreated": usage.CacheCreationInputTokens,
				"cacheRead":    usage.CacheReadInputTokens,
			}
		}
		if len(toolCalls) > 0 {
			toolCallsJson, _ := json.Marshal(toolCalls)
			assistantMessage.ToolCalls = toolCallsJson
		}
		if usageMap != nil {
			usageJson, _ := json.Marshal(usageMap)
			assistantMessage.Usage = usageJson
		}

		if err := self.appendConversationMessage(ctx, assistantMessage); err != nil {
			return nil, fmt.Errorf("saving assistant message: %w", err)
		}
		history = append(history, &assistantMessage)

		// If no tool calls, we're done.
		if len(toolCalls) == 0 || self.toolRegistry == nil {
			break
		}

		// Phase 1 — Filter & notify: resolve tools, fire OnToolCall callbacks in order.
		type toolWorkItem struct {
			toolCall  providers.ToolCall
			tool      tools.Tool
			arguments string
		}

		var workItems []toolWorkItem
		for _, toolCall := range toolCalls {
			tool := self.toolRegistry.Get(toolCall.Function.Name)
			if tool == nil {
				continue
			}

			if callbacks != nil && callbacks.OnToolCall != nil {
				callbacks.OnToolCall(toolCall.Function.Name, toolCall.Function.Arguments)
			}

			arguments := repairToolArguments(toolCall.Function.Arguments)
			if authorizationErr := validateToolAuthorization(toolCall.Function.Name, arguments, isAdmin); authorizationErr != nil {
				result := "error: " + authorizationErr.Error()
				log.Debugf("tool denied id=%s name=%s err=%v", toolCall.ID, toolCall.Function.Name, authorizationErr)
				if callbacks != nil && callbacks.OnToolResult != nil {
					callbacks.OnToolResult(toolCall.Function.Name, result)
				}
				toolMessage := newToolMessage(toolCall.ID, toolCall.Function.Name, result)
				if err := self.appendConversationMessage(ctx, toolMessage); err != nil {
					return nil, fmt.Errorf("saving tool denial: %w", err)
				}
				history = append(history, &toolMessage)
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
				defer deferutil.Recover()
				defer waitGroup.Done()
				log.Debugf("tool call id=%s name=%s", item.toolCall.ID, item.toolCall.Function.Name)
				result, executeError := item.tool.Execute(ctx, item.arguments)
				results[position] = toolResult{result: result, err: executeError}
			}(position, item)
		}
		waitGroup.Wait()

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

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

			// Detect media in tool result.
			liveResult := result
			storedResult := result
			if detected := mimetypes.DetectMedia(result); detected != nil && detected.Base64 != "" && mimetypes.IsImageFormat(detected.Format) {
				rawData, decodeError := base64.StdEncoding.DecodeString(detected.Base64)
				if decodeError == nil {
					contentType := mimetypes.MIMETypeFromFormat(detected.Format)
					var createdMedia *models.Media
					createError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
						var saveError error
						createdMedia, saveError = transaction.CreateMedia(ctx, bytes.NewReader(rawData), &models.Media{
							UserID:         ptrto.Value(userId),
							Format:         ptrto.Value(detected.Format),
							ContentType:    ptrto.Value(contentType),
							Source:         ptrto.Value(models.MediaSourceTool),
							SourceAgentID:  ptrto.Value(self.AgentID),
							ConversationID: ptrto.Value(self.ConversationID),
							ToolName:       ptrto.Value(item.toolCall.Function.Name),
							ToolCallID:     ptrto.Value(item.toolCall.ID),
						}, nil)
						return saveError
					})
					if createError == nil {
						ref, _ := json.Marshal(map[string]interface{}{
							"mediaId":   createdMedia.ID,
							"format":    detected.Format,
							"displayed": true,
						})
						storedResult = string(ref)
					}
				}
			}

			if callbacks != nil && callbacks.OnToolResult != nil {
				callbacks.OnToolResult(item.toolCall.Function.Name, liveResult)
			}

			toolMessage := newToolMessage(item.toolCall.ID, item.toolCall.Function.Name, storedResult)
			if err := self.appendConversationMessage(ctx, toolMessage); err != nil {
				return nil, fmt.Errorf("saving tool result: %w", err)
			}
			history = append(history, &toolMessage)
		}
	}

	responseProviderModelName := providers.FormatProviderModelName(providerName, responseModelName)
	log.Debugf("run done id=%s model=%s stop=%s", self.ID, responseProviderModelName, stopReason)

	return &RunResult{
		Response:          responseText,
		Usage:             totalUsage,
		ProviderModelName: responseProviderModelName,
		StopReason:        stopReason,
		ContextWindow:     contextWindow,
	}, nil
}

// repairToolArguments attempts to fix malformed JSON from the LLM.
// It only runs the repair if the JSON is actually invalid.
func repairToolArguments(input string) string {
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
	case "gateway":
		return fmt.Errorf("admin access required for gateway tool")
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
	case "project_todo":
		action := parseToolAction(arguments)
		if action != "list" {
			if action == "" {
				return fmt.Errorf("admin access required for project_todo management actions")
			}
			return fmt.Errorf("admin access required for project_todo.%s", action)
		}
		// conversation_todo: all actions allowed at runner level — ownership checked in tool.
	}
	return nil
}

func parseToolAction(arguments string) string {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return ""
	}
	action, _ := payload["action"].(string)
	return strings.ToLower(action)
}

func (self *Runner) resolveAgentProviderModelAndName(ctx context.Context) (string, string) {
	resolvedProviderModelName := ""
	resolvedName := ""
	_ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		agent, err := transaction.GetAgent(ctx, self.AgentID, nil)
		if err != nil || agent == nil {
			return nil
		}
		resolvedProviderModelName = agent.GetProviderModelName()
		resolvedName = agent.GetName()
		return nil
	})
	return resolvedProviderModelName, resolvedName
}

// buildMessages converts conversation history into LLM messages.
// It scans backward for the last context_summary message and skips everything before it.
func (self *Runner) buildMessages(
	ctx context.Context,
	history []*models.ConversationMessage,
	systemPromptSuffix string,
	systemPromptMode SystemPromptMode,
	skillPrompts string,
) []providers.ChatMessage {
	_, agentName := self.resolveAgentProviderModelAndName(ctx)
	systemPrompt := buildSystemPrompt(ctx, buildSystemPromptParameters{
		IdentityLine: resolveIdentityLine(self.AgentID, agentName),
		AgentID:      self.AgentID,
		SkillPrompts: skillPrompts,
		Mode:         systemPromptMode,
	})
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
	hasSummary := false
	if summaryIndex := findLastSummaryIndex(history); summaryIndex >= 0 {
		hasSummary = true
		messages = append(messages, providers.ChatMessage{
			Role:    "system",
			Content: prompts.PreviousConversationSummaryPrefix + conversationMessageContentText(*history[summaryIndex]),
		})
		startIndex = summaryIndex + 1
	}

	// Skip the remainder of any in-progress run after the summary.
	if hasSummary && startIndex < len(history) && conversationMessageRole(*history[startIndex]) != "user" {
		for startIndex < len(history) {
			message := history[startIndex]
			startIndex++
			stopReason := conversationMessageStopReason(*message)
			if stopReason != "" && stopReason != "tool_calls" && stopReason != "context_summary" {
				break
			}
		}
	}

	for _, message := range history[startIndex:] {
		chatMessage := providers.ChatMessage{
			Role: conversationMessageRole(*message),
		}

		// Check for multimodal content (attachments).
		blocks := conversationMessageContentBlocks(*message)
		hasAttachments := false
		for _, block := range blocks {
			if block["type"] == "attachment" {
				hasAttachments = true
				break
			}
		}

		if hasAttachments && chatMessage.Role == "user" {
			chatMessage.Content = self.buildMultimodalContent(ctx, blocks)
		} else {
			chatMessage.Content = conversationMessageContentText(*message)
		}

		// Attach tool calls on assistant messages.
		toolCalls := conversationMessageToolCalls(*message)
		if chatMessage.Role == "assistant" && len(toolCalls) > 0 {
			chatMessage.ToolCalls = append(chatMessage.ToolCalls, toolCalls...)
		}

		// Attach tool metadata on tool result messages.
		if chatMessage.Role == "tool" {
			chatMessage.ToolCallID = message.GetToolCallID()
			chatMessage.Name = message.GetToolName()
		}

		messages = append(messages, chatMessage)
	}

	// Fix interrupted tool calls.
	messages = fixInterruptedToolCalls(messages)

	return messages
}

// fixInterruptedToolCalls scans for assistant messages with tool_calls whose
// IDs have no corresponding tool result message following them.
func fixInterruptedToolCalls(messages []providers.ChatMessage) []providers.ChatMessage {
	for messageIndex := len(messages) - 1; messageIndex >= 0; messageIndex-- {
		message := messages[messageIndex]
		if message.Role != "assistant" || len(message.ToolCalls) == 0 {
			continue
		}

		// Collect tool_call IDs that have results after this assistant message.
		answered := make(map[string]bool)
		for searchIndex := messageIndex + 1; searchIndex < len(messages); searchIndex++ {
			if messages[searchIndex].Role == "tool" && messages[searchIndex].ToolCallID != "" {
				answered[messages[searchIndex].ToolCallID] = true
			}
			if messages[searchIndex].Role == "assistant" || messages[searchIndex].Role == "user" {
				break
			}
		}

		// Append synthetic results for any unanswered tool calls.
		var synthetic []providers.ChatMessage
		for _, toolCall := range message.ToolCalls {
			if !answered[toolCall.ID] {
				synthetic = append(synthetic, providers.ChatMessage{
					Role:       "tool",
					Content:    "Tool call was interrupted and did not complete.",
					ToolCallID: toolCall.ID,
					Name:       toolCall.Function.Name,
				})
			}
		}

		if len(synthetic) > 0 {
			insertAt := messageIndex + 1
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
func (self *Runner) buildMultimodalContent(ctx context.Context, blocks []map[string]string) []providers.ContentPart {
	var parts []providers.ContentPart

	for _, block := range blocks {
		switch block["type"] {
		case "text":
			if block["text"] != "" {
				parts = append(parts, providers.ContentPart{Type: "text", Text: block["text"]})
			}
		case "attachment":
			format := block["format"]
			mediaId := block["mediaId"]
			fileName := block["filename"]
			if mimetypes.IsImageFormat(format) {
				imageUrl := self.resolveMediaUrl(ctx, mediaId)
				if imageUrl != "" {
					parts = append(parts, providers.ContentPart{
						Type:     "image_url",
						ImageURL: &providers.ImageURLPart{URL: imageUrl},
					})
				}
			} else {
				// Non-image: include as text reference.
				label := fileName
				if label == "" {
					label = mediaId
				}
				parts = append(parts, providers.ContentPart{
					Type: "text",
					Text: fmt.Sprintf("[Attached file: %s (%s)]", label, format),
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
func (self *Runner) resolveMediaUrl(ctx context.Context, mediaId string) string {
	var data []byte
	var metadata *models.Media
	transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		var getError error
		data, metadata, getError = transaction.GetMedia(ctx, mediaId, nil)
		return getError
	})
	if transactionError != nil {
		log.Debugf("failed to load media %s for multimodal: %v", mediaId, transactionError)
		return ""
	}
	mimeType := metadata.GetContentType()
	if mimeType == "" {
		mimeType = mimetypes.MIMETypeFromFormat(metadata.GetFormat())
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return "data:" + mimeType + ";base64," + encoded
}

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

func (self *Runner) appendConversationMessage(ctx context.Context, message models.ConversationMessage) error {
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return fmt.Errorf("userId is required")
	}
	return store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		if _, err := transaction.GetConversation(ctx, self.ConversationID, nil); err != nil {
			if err != store.ErrNotFound {
				return err
			}
			if _, createError := transaction.CreateConversation(ctx, &models.Conversation{
				ID:      self.ConversationID,
				UserID:  ptrto.Value(user.ID),
				AgentID: ptrto.Value(self.AgentID),
			}, nil); createError != nil {
				return createError
			}
		}
		message.ID = security.NewULID()
		message.ConversationID = ptrto.Value(self.ConversationID)
		_, err := transaction.CreateConversationMessage(ctx, &message, nil)
		return err
	})
}

func newTextMessage(role, text string) models.ConversationMessage {
	roleValue := models.Role(role)
	content, _ := json.Marshal(text)
	return models.ConversationMessage{
		Role:    &roleValue,
		Content: content,
	}
}

func newMessageWithAttachments(role, text string, attachments []map[string]string) models.ConversationMessage {
	blocks := make([]map[string]string, 0, len(attachments)+1)
	blocks = append(blocks, map[string]string{
		"type": "text",
		"text": text,
	})
	for _, attachment := range attachments {
		blocks = append(blocks, map[string]string{
			"type":     "attachment",
			"mediaId":  attachment["mediaId"],
			"format":   attachment["format"],
			"filename": attachment["filename"],
		})
	}
	content, _ := json.Marshal(blocks)
	roleValue := models.Role(role)
	return models.ConversationMessage{
		Role:    &roleValue,
		Content: content,
	}
}

func newToolMessage(toolCallId, toolName, content string) models.ConversationMessage {
	message := newTextMessage("tool", content)
	message.ToolCallID = ptrto.Value(toolCallId)
	message.ToolName = ptrto.Value(toolName)
	return message
}

func conversationMessageRole(message models.ConversationMessage) string {
	if message.Role == nil {
		return ""
	}
	return string(*message.Role)
}

func conversationMessageStopReason(message models.ConversationMessage) string {
	if message.StopReason == nil {
		return ""
	}
	return string(*message.StopReason)
}

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

func conversationMessageToolCalls(message models.ConversationMessage) []providers.ToolCall {
	if len(message.ToolCalls) == 0 {
		return nil
	}
	var toolCalls []providers.ToolCall
	_ = json.Unmarshal(message.ToolCalls, &toolCalls)
	return toolCalls
}

func conversationMessageContentBlocks(message models.ConversationMessage) []map[string]string {
	if len(message.Content) == 0 {
		return nil
	}
	var blocks []map[string]string
	if err := json.Unmarshal(message.Content, &blocks); err == nil && len(blocks) > 0 {
		if blocks[0]["type"] != "" {
			return blocks
		}
	}
	return []map[string]string{
		{
			"type": "text",
			"text": conversationMessageContentText(message),
		},
	}
}
