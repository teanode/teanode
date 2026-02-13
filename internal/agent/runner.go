package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kaptinlin/jsonrepair"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/logging"
	"github.com/ziyan/teanode/internal/provider"
	"github.com/ziyan/teanode/internal/session"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

var runnerLog = logging.Get("agent")

const maxToolRounds = 100

// Runner orchestrates: load session -> build prompt -> call LLM -> save response.
type Runner struct {
	mutex        sync.RWMutex
	Provider     *provider.Client
	Sessions     *session.Store
	Config       *config.Config
	Tools        *ToolRegistry
	WorkspaceDir string
	SkillPrompts string
}

// Reconfigure hot-swaps the runner's configuration, provider, tools, and skill prompts.
// In-progress runs continue with their snapshotted references; new runs use the updated values.
func (self *Runner) Reconfigure(config *config.Config, provider *provider.Client, tools *ToolRegistry, skillPrompts string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.Config = config
	self.Provider = provider
	self.Tools = tools
	self.SkillPrompts = skillPrompts
}

// Snapshot captures the runner's current state under the read lock.
func (self *Runner) Snapshot() (config *config.Config, provider *provider.Client, tools *ToolRegistry, workspaceDirectory string, skillPrompts string) {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.Config, self.Provider, self.Tools, self.WorkspaceDir, self.SkillPrompts
}

// RunParams holds the parameters for a single agent run.
type RunParams struct {
	SessionKey string
	Message    string
	Model      string // override config default
}

// RunResult holds the result of a completed agent run.
type RunResult struct {
	RunID      string
	Response   string
	Usage      *session.Usage
	Model      string
	StopReason string
}

// RunCallbacks receives events during an agent run.
type RunCallbacks struct {
	OnTextDelta   func(text string)
	OnToolCall    func(toolName string, arguments string)
	OnToolResult  func(toolName string, result string)
	OnTitleUpdate func(title string)
}

// Run executes a chat turn: appends the user message, calls the LLM (streaming),
// and appends the assistant response. Loops when the LLM requests tool calls.
func (self *Runner) Run(ctx context.Context, params RunParams, callbacks *RunCallbacks) (*RunResult, error) {
	// Snapshot mutable fields so in-progress runs aren't affected by hot-reloads.
	config, providerClient, tools, _, _ := self.Snapshot()

	runId := uuid.New().String()
	now := time.Now().UnixMilli()

	runnerLog.Debugf("run start id=%s session=%s model=%s", runId, params.SessionKey, params.Model)

	// Enrich context with session key and title callback for tools.
	var onTitleUpdate func(string)
	if callbacks != nil && callbacks.OnTitleUpdate != nil {
		onTitleUpdate = callbacks.OnTitleUpdate
	}
	ctx = ContextWithRun(ctx, params.SessionKey, onTitleUpdate)

	// 1. Append user message to session.
	userMessage := session.NewTextMessage("user", params.Message, now)
	if err := self.Sessions.Append(params.SessionKey, userMessage); err != nil {
		return nil, fmt.Errorf("saving user message: %w", err)
	}

	// 2. Load full session history.
	history, err := self.Sessions.Load(params.SessionKey)
	if err != nil {
		return nil, fmt.Errorf("loading session: %w", err)
	}

	// 3. Choose model.
	model := config.Models.Default
	if params.Model != "" {
		model = params.Model
	}

	// 4. Tool-call loop.
	var totalUsage *session.Usage
	var responseText string
	var responseModel string
	var stopReason string

	for round := 0; round < maxToolRounds; round++ {
		runnerLog.Debugf("run id=%s round=%d history_len=%d", runId, round, len(history))

		// Build messages for the LLM.
		llmMessages := self.buildMessages(history)

		// Tier 1: truncate old tool results.
		llmMessages = truncateOldToolResults(llmMessages)

		// Build tool definitions for the request.
		var toolDefs []provider.ToolDef
		if tools != nil {
			toolDefs = tools.Definitions()
		}

		// Tier 2: compress context if approaching the context window limit.
		llmMessages, err = self.compressContext(ctx, llmMessages, toolDefs, params.SessionKey)
		if err != nil {
			runnerLog.Debugf("context compression error (non-fatal): %v", err)
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
				totalUsage = &session.Usage{}
			}
			totalUsage.Input += usage.PromptTokens
			totalUsage.Output += usage.CompletionTokens
			totalUsage.Total += usage.TotalTokens
		}

		responseText = textBuilder.String()

		// Save assistant message.
		assistantMessage := session.NewTextMessage("assistant", responseText, time.Now().UnixMilli())
		assistantMessage.Model = responseModel
		assistantMessage.Provider = config.Models.Provider
		assistantMessage.StopReason = stopReason
		if usage != nil {
			assistantMessage.Usage = &session.Usage{
				Input:  usage.PromptTokens,
				Output: usage.CompletionTokens,
				Total:  usage.TotalTokens,
			}
		}
		if len(toolCalls) > 0 {
			assistantMessage.ToolCalls, _ = json.Marshal(toolCalls)
		}

		if err := self.Sessions.Append(params.SessionKey, assistantMessage); err != nil {
			return nil, fmt.Errorf("saving assistant message: %w", err)
		}
		history = append(history, assistantMessage)

		// If no tool calls, we're done.
		if len(toolCalls) == 0 || tools == nil {
			break
		}

		// Execute tool calls and save results.
		hasToolCalls := false
		for _, toolCall := range toolCalls {
			tool := tools.Get(toolCall.Function.Name)
			if tool == nil {
				continue
			}
			hasToolCalls = true

			if callbacks != nil && callbacks.OnToolCall != nil {
				callbacks.OnToolCall(toolCall.Function.Name, toolCall.Function.Arguments)
			}

			arguments := repairToolArgs(toolCall.Function.Arguments)

			runnerLog.Debugf("tool call id=%s name=%s", toolCall.ID, toolCall.Function.Name)
			result, err := tool.Execute(ctx, arguments)
			if err != nil {
				runnerLog.Debugf("tool error id=%s name=%s err=%v", toolCall.ID, toolCall.Function.Name, err)
				result = "error: " + err.Error()
			} else {
				runnerLog.Debugf("tool done id=%s name=%s result_len=%d", toolCall.ID, toolCall.Function.Name, len(result))
			}

			if callbacks != nil && callbacks.OnToolResult != nil {
				callbacks.OnToolResult(toolCall.Function.Name, result)
			}

			toolMessage := session.NewToolMessage(toolCall.ID, toolCall.Function.Name, result, time.Now().UnixMilli())
			if err := self.Sessions.Append(params.SessionKey, toolMessage); err != nil {
				return nil, fmt.Errorf("saving tool result: %w", err)
			}
			history = append(history, toolMessage)
		}

		if !hasToolCalls {
			break
		}
	}

	runnerLog.Debugf("run done id=%s model=%s stop=%s", runId, responseModel, stopReason)

	// Generate title for new sessions (only 1 user message when we entered Run).
	if responseText != "" && callbacks != nil && callbacks.OnTitleUpdate != nil {
		userCount := 0
		for _, message := range history {
			if message.Role == "user" {
				userCount++
			}
		}
		if userCount == 1 {
			firstUserText := params.Message
			firstAssistantText := responseText
			if len(firstUserText) > 500 {
				firstUserText = firstUserText[:500]
			}
			if len(firstAssistantText) > 500 {
				firstAssistantText = firstAssistantText[:500]
			}
			titleModel := config.Models.Default
			if config.Models.TitleModel != "" {
				titleModel = config.Models.TitleModel
			}
			go func() {
				defer deferutil.Recover()

				titleRequest := provider.ChatRequest{
					Model: titleModel,
					Messages: []provider.ChatMessage{
						{Role: "system", Content: "Summarize the following conversation into a short title (max 8 words). Output only the title, nothing else."},
						{Role: "user", Content: "User: " + firstUserText + "\n\nAssistant: " + firstAssistantText},
					},
				}
				titleResponse, err := providerClient.ChatCompletion(context.Background(), titleRequest)
				var title string
				if err != nil || len(titleResponse.Choices) == 0 || strings.TrimSpace(titleResponse.Choices[0].Message.Content) == "" {
					title = time.Now().Format("Jan 2, 2006 3:04 PM")
				} else {
					title = strings.TrimSpace(titleResponse.Choices[0].Message.Content)
				}

				if err := self.Sessions.SetTitle(params.SessionKey, title); err != nil {
					runnerLog.Debugf("failed to set title: %v", err)
				}
				callbacks.OnTitleUpdate(title)
			}()
		}
	}

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

// buildMessages converts session history into LLM messages.
// It scans backward for the last context_summary message and skips everything before it.
func (self *Runner) buildMessages(history []session.Message) []provider.ChatMessage {
	systemPrompt := BuildSystemPrompt(self.Config, self.WorkspaceDir, self.SkillPrompts)
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
