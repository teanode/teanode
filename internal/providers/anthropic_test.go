package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTranslateRequest_SystemExtraction(t *testing.T) {
	client := NewAnthropicClient("https://api.anthropic.com/v1", "test-key")

	request := ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []ChatMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi!"},
			{Role: "system", Content: "Remember this context."},
			{Role: "user", Content: "What's up?"},
		},
		MaxTokens: 1024,
	}

	translated := client.translateRequest(request, false)

	// System should contain only the leading system message.
	var systemBlocks []anthropicSystemBlock
	if err := json.Unmarshal(translated.System, &systemBlocks); err != nil {
		t.Fatalf("unmarshal system: %v", err)
	}
	if len(systemBlocks) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(systemBlocks))
	}
	if systemBlocks[0].Text != "You are helpful." {
		t.Errorf("system text = %q, want %q", systemBlocks[0].Text, "You are helpful.")
	}

	// Messages should not contain any system role.
	for _, message := range translated.Messages {
		if message.Role == "system" {
			t.Error("messages should not contain system role")
		}
	}

	// Mid-conversation system message should be converted to user with [System] prefix.
	// Messages should be: user("Hello"), assistant("Hi!"), user("[System] Remember..."), user("What's up?")
	// After alternation merging: user("Hello"), assistant("Hi!"), user([System]+What's up? merged)
	if len(translated.Messages) != 3 {
		t.Fatalf("expected 3 messages after alternation, got %d", len(translated.Messages))
	}
	if translated.Messages[2].Role != "user" {
		t.Errorf("message[2] role = %q, want user", translated.Messages[2].Role)
	}
}

func TestTranslateRequest_ToolDefinitions(t *testing.T) {
	client := NewAnthropicClient("https://api.anthropic.com/v1", "test-key")

	request := ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hello"},
		},
		Tools: []ToolDefinition{
			{
				Type: "function",
				Function: FunctionSpec{
					Name:        "get_weather",
					Description: "Get the weather",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"location": map[string]interface{}{"type": "string"},
						},
					},
					Returns: map[string]interface{}{"type": "object"},
				},
			},
		},
	}

	translated := client.translateRequest(request, false)

	if len(translated.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(translated.Tools))
	}
	if translated.Tools[0].Name != "get_weather" {
		t.Errorf("tool name = %q, want %q", translated.Tools[0].Name, "get_weather")
	}
	if translated.Tools[0].Description != "Get the weather" {
		t.Errorf("tool description = %q", translated.Tools[0].Description)
	}
	// InputSchema should be the parameters, not include Returns.
	schemaJSON, _ := json.Marshal(translated.Tools[0].InputSchema)
	if string(schemaJSON) == "" {
		t.Error("input_schema should not be empty")
	}
}

func TestTranslateRequest_ToolResults(t *testing.T) {
	client := NewAnthropicClient("https://api.anthropic.com/v1", "test-key")

	request := ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []ChatMessage{
			{Role: "user", Content: "What's the weather?"},
			{
				Role:    "assistant",
				Content: "",
				ToolCalls: []ToolCall{
					{
						ID:   "call_123",
						Type: "function",
						Function: FunctionCall{
							Name:      "get_weather",
							Arguments: `{"location":"NYC"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				Content:    "Sunny, 72F",
				ToolCallID: "call_123",
				Name:       "get_weather",
			},
		},
	}

	translated := client.translateRequest(request, false)

	// Tool result messages should become user messages with tool_result content blocks.
	// After translation: user, assistant, user(tool_result)
	if len(translated.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(translated.Messages))
	}

	// The tool result should be a user message.
	if translated.Messages[2].Role != "user" {
		t.Errorf("tool result role = %q, want user", translated.Messages[2].Role)
	}

	// Parse the content blocks of the tool result message.
	var blocks []anthropicContentBlock
	if err := json.Unmarshal(translated.Messages[2].Content, &blocks); err != nil {
		t.Fatalf("unmarshal tool result content: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Type != "tool_result" {
		t.Errorf("block type = %q, want tool_result", blocks[0].Type)
	}
	if blocks[0].ToolUseID != "call_123" {
		t.Errorf("tool_use_id = %q, want call_123", blocks[0].ToolUseID)
	}
}

func TestTranslateRequest_ConsecutiveToolResults(t *testing.T) {
	client := NewAnthropicClient("https://api.anthropic.com/v1", "test-key")

	request := ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []ChatMessage{
			{Role: "user", Content: "Do both"},
			{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{ID: "call_1", Type: "function", Function: FunctionCall{Name: "tool_a", Arguments: "{}"}},
					{ID: "call_2", Type: "function", Function: FunctionCall{Name: "tool_b", Arguments: "{}"}},
				},
			},
			{Role: "tool", Content: "result_a", ToolCallID: "call_1"},
			{Role: "tool", Content: "result_b", ToolCallID: "call_2"},
		},
	}

	translated := client.translateRequest(request, false)

	// Consecutive tool results (both become user role) should be merged into one user message.
	if len(translated.Messages) != 3 {
		t.Fatalf("expected 3 messages (user, assistant, merged user), got %d", len(translated.Messages))
	}

	// The merged user message should have 2 tool_result blocks.
	var blocks []json.RawMessage
	if err := json.Unmarshal(translated.Messages[2].Content, &blocks); err != nil {
		t.Fatalf("unmarshal merged content: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks in merged message, got %d", len(blocks))
	}
}

func TestTranslateRequest_MaxTokensDefault(t *testing.T) {
	client := NewAnthropicClient("https://api.anthropic.com/v1", "test-key")

	request := ChatRequest{
		Model:    "claude-sonnet-4-20250514",
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	}

	translated := client.translateRequest(request, false)

	if translated.MaxTokens != 8192 {
		t.Errorf("max_tokens = %d, want 8192", translated.MaxTokens)
	}
}

func TestTranslateResponse_TextContent(t *testing.T) {
	client := NewAnthropicClient("https://api.anthropic.com/v1", "test-key")

	response := anthropicResponse{
		ID:    "msg_123",
		Model: "claude-sonnet-4-20250514",
		Content: []anthropicContentBlock{
			{Type: "text", Text: "Hello! How can I help?"},
		},
		StopReason: "end_turn",
		Usage:      anthropicUsage{InputTokens: 10, OutputTokens: 20},
	}

	chatResponse := client.translateResponse(response)

	if chatResponse.ID != "msg_123" {
		t.Errorf("id = %q, want msg_123", chatResponse.ID)
	}
	if len(chatResponse.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(chatResponse.Choices))
	}
	if chatResponse.Choices[0].Message.Content != "Hello! How can I help?" {
		t.Errorf("content = %q", chatResponse.Choices[0].Message.Content)
	}
	if chatResponse.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", chatResponse.Choices[0].FinishReason)
	}
	if chatResponse.Usage.PromptTokens != 10 {
		t.Errorf("prompt_tokens = %d, want 10", chatResponse.Usage.PromptTokens)
	}
	if chatResponse.Usage.CompletionTokens != 20 {
		t.Errorf("completion_tokens = %d, want 20", chatResponse.Usage.CompletionTokens)
	}
	if chatResponse.Usage.TotalTokens != 30 {
		t.Errorf("total_tokens = %d, want 30", chatResponse.Usage.TotalTokens)
	}
}

func TestTranslateResponse_ToolUseBlocks(t *testing.T) {
	client := NewAnthropicClient("https://api.anthropic.com/v1", "test-key")

	response := anthropicResponse{
		ID:    "msg_456",
		Model: "claude-sonnet-4-20250514",
		Content: []anthropicContentBlock{
			{Type: "text", Text: "Let me check the weather."},
			{
				Type:  "tool_use",
				ID:    "toolu_123",
				Name:  "get_weather",
				Input: json.RawMessage(`{"location":"NYC"}`),
			},
		},
		StopReason: "tool_use",
		Usage:      anthropicUsage{InputTokens: 15, OutputTokens: 25},
	}

	chatResponse := client.translateResponse(response)

	if chatResponse.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", chatResponse.Choices[0].FinishReason)
	}
	if chatResponse.Choices[0].Message.Content != "Let me check the weather." {
		t.Errorf("content = %q", chatResponse.Choices[0].Message.Content)
	}
	if len(chatResponse.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(chatResponse.Choices[0].Message.ToolCalls))
	}

	toolCall := chatResponse.Choices[0].Message.ToolCalls[0]
	if toolCall.ID != "toolu_123" {
		t.Errorf("tool call id = %q", toolCall.ID)
	}
	if toolCall.Function.Name != "get_weather" {
		t.Errorf("tool call name = %q", toolCall.Function.Name)
	}
	if toolCall.Function.Arguments != `{"location":"NYC"}` {
		t.Errorf("tool call arguments = %q", toolCall.Function.Arguments)
	}
}

func TestTranslateStopReason(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"end_turn", "stop"},
		{"tool_use", "tool_calls"},
		{"max_tokens", "length"},
		{"unknown", "unknown"},
	}
	for _, testCase := range cases {
		result := translateStopReason(testCase.input)
		if result != testCase.expected {
			t.Errorf("translateStopReason(%q) = %q, want %q", testCase.input, result, testCase.expected)
		}
	}
}

func TestAnthropicSSEStreaming(t *testing.T) {
	// Create an httptest server that mimics Anthropic's SSE format.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Verify headers.
		if request.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q, want test-key", request.Header.Get("x-api-key"))
		}
		if request.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("anthropic-version = %q", request.Header.Get("anthropic-version"))
		}

		// Verify the request body is valid.
		body, _ := io.ReadAll(request.Body)
		var anthropicRequest anthropicRequest
		if err := json.Unmarshal(body, &anthropicRequest); err != nil {
			t.Errorf("invalid request body: %v", err)
		}
		if !anthropicRequest.Stream {
			t.Error("expected stream=true")
		}

		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := writer.(http.Flusher)

		// message_start
		fmt.Fprintf(writer, "event: message_start\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`)
		flusher.Flush()

		// content_block_start (text)
		fmt.Fprintf(writer, "event: content_block_start\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		flusher.Flush()

		// content_block_delta (text)
		fmt.Fprintf(writer, "event: content_block_delta\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`)
		flusher.Flush()

		fmt.Fprintf(writer, "event: content_block_delta\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world!"}}`)
		flusher.Flush()

		// content_block_stop
		fmt.Fprintf(writer, "event: content_block_stop\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"content_block_stop","index":0}`)
		flusher.Flush()

		// message_delta
		fmt.Fprintf(writer, "event: message_delta\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`)
		flusher.Flush()

		// message_stop
		fmt.Fprintf(writer, "event: message_stop\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"message_stop"}`)
		flusher.Flush()
	}))
	defer server.Close()

	client := NewAnthropicClient(server.URL, "test-key")

	stream, err := client.ChatCompletionStream(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-20250514",
		Messages: []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletionStream: %v", err)
	}

	var textContent string
	var finishReason string
	var gotDone bool
	var gotUsage bool

	for event := range stream {
		if event.Err != nil {
			t.Fatalf("stream error: %v", event.Err)
		}
		if event.Done {
			gotDone = true
			continue
		}
		if event.Chunk == nil {
			continue
		}
		for _, choice := range event.Chunk.Choices {
			textContent += choice.Delta.Content
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
		}
		if event.Chunk.Usage != nil && event.Chunk.Usage.CompletionTokens > 0 {
			gotUsage = true
		}
	}

	if textContent != "Hello world!" {
		t.Errorf("text = %q, want %q", textContent, "Hello world!")
	}
	if finishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", finishReason)
	}
	if !gotDone {
		t.Error("expected Done event")
	}
	if !gotUsage {
		t.Error("expected usage in message_delta")
	}
}

func TestAnthropicSSEToolUseStreaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := writer.(http.Flusher)

		// message_start
		fmt.Fprintf(writer, "event: message_start\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"message_start","message":{"id":"msg_tool","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`)
		flusher.Flush()

		// content_block_start (tool_use)
		fmt.Fprintf(writer, "event: content_block_start\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_123","name":"get_weather"}}`)
		flusher.Flush()

		// content_block_delta (tool input JSON)
		fmt.Fprintf(writer, "event: content_block_delta\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"location\":"}}`)
		flusher.Flush()

		fmt.Fprintf(writer, "event: content_block_delta\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"NYC\"}"}}`)
		flusher.Flush()

		// content_block_stop
		fmt.Fprintf(writer, "event: content_block_stop\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"content_block_stop","index":0}`)
		flusher.Flush()

		// message_delta
		fmt.Fprintf(writer, "event: message_delta\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":15}}`)
		flusher.Flush()

		// message_stop
		fmt.Fprintf(writer, "event: message_stop\n")
		fmt.Fprintf(writer, "data: %s\n\n", `{"type":"message_stop"}`)
		flusher.Flush()
	}))
	defer server.Close()

	client := NewAnthropicClient(server.URL, "test-key")

	stream, err := client.ChatCompletionStream(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-20250514",
		Messages: []ChatMessage{{Role: "user", Content: "What's the weather?"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletionStream: %v", err)
	}

	var toolCallDeltas []ToolCallDelta
	var finishReason string

	for event := range stream {
		if event.Err != nil {
			t.Fatalf("stream error: %v", event.Err)
		}
		if event.Done || event.Chunk == nil {
			continue
		}
		for _, choice := range event.Chunk.Choices {
			for _, delta := range choice.Delta.ToolCalls {
				toolCallDeltas = append(toolCallDeltas, delta)
			}
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
		}
	}

	if finishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", finishReason)
	}

	// We should have the initial delta (with ID and name) plus argument deltas.
	if len(toolCallDeltas) < 2 {
		t.Fatalf("expected at least 2 tool call deltas, got %d", len(toolCallDeltas))
	}

	// First delta should have the tool ID and name.
	if toolCallDeltas[0].ID != "toolu_123" {
		t.Errorf("first delta id = %q, want toolu_123", toolCallDeltas[0].ID)
	}
	if toolCallDeltas[0].Function.Name != "get_weather" {
		t.Errorf("first delta name = %q, want get_weather", toolCallDeltas[0].Function.Name)
	}
}

func TestAnthropicNonStreamingChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(anthropicResponse{
			ID:    "msg_non_stream",
			Model: "claude-sonnet-4-20250514",
			Content: []anthropicContentBlock{
				{Type: "text", Text: "Non-streaming response"},
			},
			StopReason: "end_turn",
			Usage:      anthropicUsage{InputTokens: 5, OutputTokens: 3},
		})
	}))
	defer server.Close()

	client := NewAnthropicClient(server.URL, "test-key")

	response, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-20250514",
		Messages: []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if response.ID != "msg_non_stream" {
		t.Errorf("id = %q", response.ID)
	}
	if len(response.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(response.Choices))
	}
	if response.Choices[0].Message.Content != "Non-streaming response" {
		t.Errorf("content = %q", response.Choices[0].Message.Content)
	}
	if response.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q", response.Choices[0].FinishReason)
	}
}

func TestAnthropicListModels_Fallback(t *testing.T) {
	// Server returns 404 to trigger fallback.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewAnthropicClient(server.URL, "test-key")

	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}

	if len(models) == 0 {
		t.Fatal("expected fallback models, got none")
	}

	// Check that known models are in the list.
	hasOpus := false
	for _, model := range models {
		if model.ID == "claude-opus-4-20250514" {
			hasOpus = true
		}
	}
	if !hasOpus {
		t.Error("fallback models should include claude-opus-4-20250514")
	}
}
