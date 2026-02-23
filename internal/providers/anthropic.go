package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/util/deferutil"
)

// AnthropicClient talks to the Anthropic Messages API.
type AnthropicClient struct {
	baseUrl    string
	apiKey     string
	httpClient *http.Client
}

// NewAnthropicClient creates an Anthropic provider client.
func NewAnthropicClient(baseUrl, apiKey string) *AnthropicClient {
	return &AnthropicClient{
		baseUrl:    strings.TrimRight(baseUrl, "/"),
		apiKey:     apiKey,
		httpClient: http.DefaultClient,
	}
}

// --- Anthropic API types ---

type anthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      json.RawMessage    `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float64           `json:"temperature,omitempty"`
	Tools       []anthropicToolDef `json:"tools,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthropicContentBlock struct {
	Type      string                `json:"type"`
	Text      string                `json:"text,omitempty"`
	ID        string                `json:"id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Input     json.RawMessage       `json:"input,omitempty"`
	ToolUseID string                `json:"tool_use_id,omitempty"`
	Content   string                `json:"content,omitempty"`
	Source    *anthropicImageSource `json:"source,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`                 // "base64" or "url"
	MediaType string `json:"media_type,omitempty"` // e.g. "image/png"
	Data      string `json:"data,omitempty"`       // base64 data
	URL       string `json:"url,omitempty"`        // URL reference
}

type anthropicCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

type anthropicToolDef struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  interface{}            `json:"input_schema"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Model      string                  `json:"model"`
	Role       string                  `json:"role"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      anthropicUsage          `json:"usage"`
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// --- SSE event types ---

type anthropicSSEMessageStart struct {
	Type    string            `json:"type"`
	Message anthropicResponse `json:"message"`
}

type anthropicSSEContentBlockStart struct {
	Type         string                `json:"type"`
	Index        int                   `json:"index"`
	ContentBlock anthropicContentBlock `json:"content_block"`
}

type anthropicSSEContentBlockDelta struct {
	Type  string                     `json:"type"`
	Index int                        `json:"index"`
	Delta anthropicContentBlockDelta `json:"delta"`
}

type anthropicContentBlockDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

type anthropicSSEMessageDelta struct {
	Type  string         `json:"type"`
	Delta anthropicDelta `json:"delta"`
	Usage anthropicUsage `json:"usage"`
}

type anthropicDelta struct {
	StopReason string `json:"stop_reason"`
}

// --- System content block type ---

type anthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

// --- Request translation ---

// ChatCompletion sends a non-streaming chat completion request.
func (self *AnthropicClient) ChatCompletion(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultNonStreamingRequestTimeout)
	defer cancel()

	anthropicRequest := self.translateRequest(request, false)
	body, _ := json.Marshal(anthropicRequest)

	log.Debugf("POST %s/messages model=%s messages=%d stream=false", self.baseUrl, request.Model, len(request.Messages))

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", self.baseUrl+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	self.setHeaders(httpRequest)

	response, err := self.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer response.Body.Close()

	log.Debugf("POST %s/messages status=%d", self.baseUrl, response.StatusCode)

	if response.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("API error %d: %s", response.StatusCode, string(responseBody))
	}

	var anthropicResponse anthropicResponse
	if err := json.NewDecoder(response.Body).Decode(&anthropicResponse); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	chatResponse := self.translateResponse(anthropicResponse)

	if chatResponse.Usage != nil {
		log.Debugf("chat completion done model=%s prompt_tokens=%d completion_tokens=%d", chatResponse.Model, chatResponse.Usage.PromptTokens, chatResponse.Usage.CompletionTokens)
	}

	return chatResponse, nil
}

// ChatCompletionStream sends a streaming chat completion request.
func (self *AnthropicClient) ChatCompletionStream(ctx context.Context, request ChatRequest) (<-chan StreamEvent, error) {
	anthropicRequest := self.translateRequest(request, true)
	body, _ := json.Marshal(anthropicRequest)

	log.Debugf("POST %s/messages model=%s messages=%d stream=true", self.baseUrl, request.Model, len(request.Messages))

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", self.baseUrl+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	self.setHeaders(httpRequest)

	response, err := self.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	log.Debugf("POST %s/messages status=%d (stream opened)", self.baseUrl, response.StatusCode)

	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		responseBody, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("API error %d: %s", response.StatusCode, string(responseBody))
	}

	events := make(chan StreamEvent, 32)
	go func() {
		defer deferutil.Recover()
		defer close(events)
		defer response.Body.Close()
		self.readSSE(ctx, response.Body, events)
	}()

	return events, nil
}

// ListModels fetches available models from Anthropic's /v1/models endpoint.
// On failure, returns a hardcoded list of known Claude models.
func (self *AnthropicClient) ListModels(ctx context.Context) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultModelsRequestTimeout)
	defer cancel()

	httpRequest, err := http.NewRequestWithContext(ctx, "GET", self.baseUrl+"/models", nil)
	if err != nil {
		return self.fallbackModels(), nil
	}
	self.setHeaders(httpRequest)

	response, err := self.httpClient.Do(httpRequest)
	if err != nil {
		return self.fallbackModels(), nil
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return self.fallbackModels(), nil
	}

	var result struct {
		Data []struct {
			ID        string `json:"id"`
			CreatedAt string `json:"created_at"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return self.fallbackModels(), nil
	}

	models := make([]ModelInfo, len(result.Data))
	for index, model := range result.Data {
		models[index] = ModelInfo{ID: model.ID}
	}

	sort.Slice(models, func(first, second int) bool {
		return models[first].ID < models[second].ID
	})

	return models, nil
}

func (self *AnthropicClient) fallbackModels() []ModelInfo {
	return []ModelInfo{
		{ID: "claude-opus-4-20250514"},
		{ID: "claude-sonnet-4-20250514"},
		{ID: "claude-haiku-4-20250514"},
		{ID: "claude-3-5-sonnet-20241022"},
		{ID: "claude-3-5-haiku-20241022"},
	}
}

// --- Translation helpers ---

func (self *AnthropicClient) translateRequest(request ChatRequest, stream bool) anthropicRequest {
	systemBlocks, messages := self.extractSystemAndMessages(request.Messages)

	maxTokens := request.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	result := anthropicRequest{
		Model:       request.Model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: request.Temperature,
		Stream:      stream,
	}

	if len(systemBlocks) > 0 {
		// Mark the last system block for prompt caching.
		systemBlocks[len(systemBlocks)-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
		systemJSON, _ := json.Marshal(systemBlocks)
		result.System = systemJSON
	}

	if len(request.Tools) > 0 {
		result.Tools = self.translateTools(request.Tools)
		// Mark the last tool for prompt caching.
		result.Tools[len(result.Tools)-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}

	return result
}

// extractSystemAndMessages separates system messages and translates the
// remaining messages into Anthropic format with proper alternation.
func (self *AnthropicClient) extractSystemAndMessages(messages []ChatMessage) ([]anthropicSystemBlock, []anthropicMessage) {
	var systemBlocks []anthropicSystemBlock
	var remaining []ChatMessage

	// Extract leading system messages as top-level system blocks.
	leadingDone := false
	for _, message := range messages {
		if !leadingDone && message.Role == "system" {
			systemBlocks = append(systemBlocks, anthropicSystemBlock{
				Type: "text",
				Text: message.ContentText(),
			})
			continue
		}
		leadingDone = true

		if message.Role == "system" {
			// Mid-conversation system messages become user messages with [System] prefix.
			remaining = append(remaining, ChatMessage{
				Role:    "user",
				Content: "[System] " + message.ContentText(),
			})
		} else {
			remaining = append(remaining, message)
		}
	}

	// Translate messages and ensure proper alternation.
	var anthropicMessages []anthropicMessage
	for _, message := range remaining {
		translated := self.translateMessage(message)
		// Merge consecutive same-role messages.
		if len(anthropicMessages) > 0 && anthropicMessages[len(anthropicMessages)-1].Role == translated.Role {
			anthropicMessages[len(anthropicMessages)-1] = self.mergeMessages(anthropicMessages[len(anthropicMessages)-1], translated)
		} else {
			anthropicMessages = append(anthropicMessages, translated)
		}
	}

	return systemBlocks, anthropicMessages
}

func (self *AnthropicClient) translateMessage(message ChatMessage) anthropicMessage {
	switch message.Role {
	case "assistant":
		return self.translateAssistantMessage(message)
	case "tool":
		return self.translateToolResultMessage(message)
	default:
		// User messages: may contain multimodal content parts.
		if parts, ok := message.Content.([]ContentPart); ok {
			var blocks []anthropicContentBlock
			for _, part := range parts {
				switch part.Type {
				case "text":
					blocks = append(blocks, anthropicContentBlock{Type: "text", Text: part.Text})
				case "image_url":
					if part.ImageURL != nil {
						blocks = append(blocks, self.translateImagePart(*part.ImageURL))
					}
				}
			}
			if len(blocks) == 0 {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: ""})
			}
			content, _ := json.Marshal(blocks)
			return anthropicMessage{Role: "user", Content: content}
		}
		// Plain string content.
		blocks := []anthropicContentBlock{{Type: "text", Text: message.ContentText()}}
		content, _ := json.Marshal(blocks)
		return anthropicMessage{Role: "user", Content: content}
	}
}

// translateImagePart converts an ImageURLPart into an Anthropic image content block.
// If the URL is a data URI (base64), it emits a base64 source block.
// Otherwise it emits a URL source block.
func (self *AnthropicClient) translateImagePart(imageUrl ImageURLPart) anthropicContentBlock {
	if strings.HasPrefix(imageUrl.URL, "data:") {
		// Parse data URI: data:<mediaType>;base64,<data>
		parts := strings.SplitN(imageUrl.URL, ",", 2)
		if len(parts) == 2 {
			mediaType := strings.TrimPrefix(parts[0], "data:")
			mediaType = strings.TrimSuffix(mediaType, ";base64")
			return anthropicContentBlock{
				Type: "image",
				Source: &anthropicImageSource{
					Type:      "base64",
					MediaType: mediaType,
					Data:      parts[1],
				},
			}
		}
	}
	return anthropicContentBlock{
		Type: "image",
		Source: &anthropicImageSource{
			Type: "url",
			URL:  imageUrl.URL,
		},
	}
}

func (self *AnthropicClient) translateAssistantMessage(message ChatMessage) anthropicMessage {
	var blocks []anthropicContentBlock

	if text := message.ContentText(); text != "" {
		blocks = append(blocks, anthropicContentBlock{Type: "text", Text: text})
	}

	for _, toolCall := range message.ToolCalls {
		var input json.RawMessage
		if toolCall.Function.Arguments != "" {
			// Parse the JSON string arguments into a raw JSON object.
			input = json.RawMessage(toolCall.Function.Arguments)
		} else {
			input = json.RawMessage("{}")
		}
		blocks = append(blocks, anthropicContentBlock{
			Type:  "tool_use",
			ID:    toolCall.ID,
			Name:  toolCall.Function.Name,
			Input: input,
		})
	}

	if len(blocks) == 0 {
		blocks = append(blocks, anthropicContentBlock{Type: "text", Text: ""})
	}

	content, _ := json.Marshal(blocks)
	return anthropicMessage{Role: "assistant", Content: content}
}

func (self *AnthropicClient) translateToolResultMessage(message ChatMessage) anthropicMessage {
	blocks := []anthropicContentBlock{{
		Type:      "tool_result",
		ToolUseID: message.ToolCallID,
		Content:   message.ContentText(),
	}}
	content, _ := json.Marshal(blocks)
	return anthropicMessage{Role: "user", Content: content}
}

// mergeMessages combines two same-role messages by concatenating their content block arrays.
func (self *AnthropicClient) mergeMessages(existing, incoming anthropicMessage) anthropicMessage {
	var existingBlocks []json.RawMessage
	json.Unmarshal(existing.Content, &existingBlocks)

	var incomingBlocks []json.RawMessage
	json.Unmarshal(incoming.Content, &incomingBlocks)

	merged := append(existingBlocks, incomingBlocks...)
	content, _ := json.Marshal(merged)
	return anthropicMessage{Role: existing.Role, Content: content}
}

func (self *AnthropicClient) translateTools(tools []ToolDefinition) []anthropicToolDef {
	result := make([]anthropicToolDef, len(tools))
	for index, tool := range tools {
		result[index] = anthropicToolDef{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		}
	}
	return result
}

// --- Response translation ---

func (self *AnthropicClient) translateResponse(response anthropicResponse) *ChatResponse {
	message := ChatMessage{Role: "assistant"}

	var toolCalls []ToolCall
	var textParts []string

	for _, block := range response.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			arguments, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      block.Name,
					Arguments: string(arguments),
				},
			})
		}
	}

	message.Content = strings.Join(textParts, "")
	message.ToolCalls = toolCalls

	return &ChatResponse{
		ID:    response.ID,
		Model: response.Model,
		Choices: []Choice{{
			Index:        0,
			Message:      message,
			FinishReason: translateStopReason(response.StopReason),
		}},
		Usage: &UsageInfo{
			PromptTokens:             response.Usage.InputTokens,
			CompletionTokens:         response.Usage.OutputTokens,
			TotalTokens:              response.Usage.InputTokens + response.Usage.OutputTokens,
			CacheCreationInputTokens: response.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     response.Usage.CacheReadInputTokens,
		},
	}
}

func translateStopReason(stopReason string) string {
	switch stopReason {
	case "end_turn":
		return "stop"
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	default:
		return stopReason
	}
}

// --- Streaming SSE ---

func (self *AnthropicClient) readSSE(ctx context.Context, reader io.Reader, events chan<- StreamEvent) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Track content blocks by index for mapping tool_use deltas.
	type blockInfo struct {
		blockType string
		toolId    string
		toolName  string
	}
	blocks := make(map[int]blockInfo)

	var messageId string
	var messageModel string
	var pendingEvent string

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}

		line := scanner.Text()

		// Anthropic SSE uses "event: <type>" lines followed by "data: <json>" lines.
		if strings.HasPrefix(line, "event: ") {
			pendingEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		switch pendingEvent {
		case "message_start":
			var event anthropicSSEMessageStart
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				events <- StreamEvent{Err: fmt.Errorf("parsing message_start: %w", err)}
				return
			}
			messageId = event.Message.ID
			messageModel = event.Message.Model

			// Emit input token usage (including cache metrics) from message_start.
			if event.Message.Usage.InputTokens > 0 || event.Message.Usage.CacheCreationInputTokens > 0 || event.Message.Usage.CacheReadInputTokens > 0 {
				events <- StreamEvent{
					Chunk: &StreamChunk{
						ID:    messageId,
						Model: messageModel,
						Usage: &UsageInfo{
							PromptTokens:             event.Message.Usage.InputTokens,
							CacheCreationInputTokens: event.Message.Usage.CacheCreationInputTokens,
							CacheReadInputTokens:     event.Message.Usage.CacheReadInputTokens,
						},
					},
				}
			}

		case "content_block_start":
			var event anthropicSSEContentBlockStart
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				events <- StreamEvent{Err: fmt.Errorf("parsing content_block_start: %w", err)}
				return
			}
			blocks[event.Index] = blockInfo{
				blockType: event.ContentBlock.Type,
				toolId:    event.ContentBlock.ID,
				toolName:  event.ContentBlock.Name,
			}
			// For tool_use blocks, emit the initial tool call delta with ID and name.
			if event.ContentBlock.Type == "tool_use" {
				events <- StreamEvent{
					Chunk: &StreamChunk{
						ID:    messageId,
						Model: messageModel,
						Choices: []StreamChoice{{
							Index: 0,
							Delta: ChatDelta{
								ToolCalls: []ToolCallDelta{{
									Index: event.Index,
									ID:    event.ContentBlock.ID,
									Type:  "function",
									Function: FunctionCallDelta{
										Name: event.ContentBlock.Name,
									},
								}},
							},
						}},
					},
				}
			}

		case "content_block_delta":
			var event anthropicSSEContentBlockDelta
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				events <- StreamEvent{Err: fmt.Errorf("parsing content_block_delta: %w", err)}
				return
			}

			info := blocks[event.Index]
			switch info.blockType {
			case "text":
				events <- StreamEvent{
					Chunk: &StreamChunk{
						ID:    messageId,
						Model: messageModel,
						Choices: []StreamChoice{{
							Index: 0,
							Delta: ChatDelta{
								Content: event.Delta.Text,
							},
						}},
					},
				}
			case "tool_use":
				events <- StreamEvent{
					Chunk: &StreamChunk{
						ID:    messageId,
						Model: messageModel,
						Choices: []StreamChoice{{
							Index: 0,
							Delta: ChatDelta{
								ToolCalls: []ToolCallDelta{{
									Index: event.Index,
									Function: FunctionCallDelta{
										Arguments: event.Delta.PartialJSON,
									},
								}},
							},
						}},
					},
				}
			}

		case "message_delta":
			var event anthropicSSEMessageDelta
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				events <- StreamEvent{Err: fmt.Errorf("parsing message_delta: %w", err)}
				return
			}
			finishReason := translateStopReason(event.Delta.StopReason)
			events <- StreamEvent{
				Chunk: &StreamChunk{
					ID:    messageId,
					Model: messageModel,
					Choices: []StreamChoice{{
						Index:        0,
						FinishReason: finishReason,
					}},
					Usage: &UsageInfo{
						CompletionTokens: event.Usage.OutputTokens,
					},
				},
			}

		case "message_stop":
			events <- StreamEvent{Done: true}
			return
		}

		pendingEvent = ""
	}

	if err := scanner.Err(); err != nil {
		events <- StreamEvent{Err: fmt.Errorf("reading stream: %w", err)}
	}
}

func (self *AnthropicClient) setHeaders(request *http.Request) {
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-api-key", self.apiKey)
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
}
