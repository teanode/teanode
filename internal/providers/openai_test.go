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

func TestOpenAINonStreamingChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Verify method and path.
		if request.Method != "POST" {
			t.Errorf("method = %q, want POST", request.Method)
		}
		if request.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", request.URL.Path)
		}

		// Verify headers.
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", request.Header.Get("Content-Type"))
		}

		// Verify request body.
		body, _ := io.ReadAll(request.Body)
		var chatRequest ChatRequest
		if err := json.Unmarshal(body, &chatRequest); err != nil {
			t.Fatalf("invalid request body: %v", err)
		}
		if chatRequest.Stream {
			t.Error("expected stream=false for non-streaming")
		}
		if chatRequest.ModelName != "gpt-4o" {
			t.Errorf("model = %q, want gpt-4o", chatRequest.ModelName)
		}

		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(ChatResponse{
			ID:        "chatcmpl-123",
			ModelName: "gpt-4o",
			Choices: []Choice{{
				Index:        0,
				Message:      ChatMessage{Role: "assistant", Content: "Hello from OpenAI!"},
				FinishReason: "stop",
			}},
			Usage: &UsageInformation{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")

	response, err := client.ChatCompletion(context.Background(), ChatRequest{
		ModelName: "gpt-4o",
		Messages:  []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if response.ID != "chatcmpl-123" {
		t.Errorf("id = %q, want chatcmpl-123", response.ID)
	}
	if response.ModelName != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", response.ModelName)
	}
	if len(response.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(response.Choices))
	}
	if response.Choices[0].Message.Content != "Hello from OpenAI!" {
		t.Errorf("content = %q", response.Choices[0].Message.Content)
	}
	if response.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q", response.Choices[0].FinishReason)
	}
	if response.Usage.PromptTokens != 10 {
		t.Errorf("prompt_tokens = %d, want 10", response.Usage.PromptTokens)
	}
	if response.Usage.CompletionTokens != 5 {
		t.Errorf("completion_tokens = %d, want 5", response.Usage.CompletionTokens)
	}
	if response.Usage.TotalTokens != 15 {
		t.Errorf("total_tokens = %d, want 15", response.Usage.TotalTokens)
	}
}

func TestOpenAINonStreamingAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
		writer.Write([]byte(`{"error":{"message":"rate limit exceeded"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		ModelName: "gpt-4o",
		Messages:  []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	if got := err.Error(); got == "" {
		t.Error("expected non-empty error message")
	}
}

func TestOpenAINonStreamingInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Write([]byte(`not valid json`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		ModelName: "gpt-4o",
		Messages:  []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestOpenAIStreamingChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Verify request body has stream=true.
		body, _ := io.ReadAll(request.Body)
		var chatRequest ChatRequest
		json.Unmarshal(body, &chatRequest)
		if !chatRequest.Stream {
			t.Error("expected stream=true")
		}

		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := writer.(http.Flusher)

		// Send content deltas.
		chunks := []StreamChunk{
			{
				ID:        "chatcmpl-stream",
				ModelName: "gpt-4o",
				Choices: []StreamChoice{{
					Index: 0,
					Delta: ChatDelta{Role: "assistant"},
				}},
			},
			{
				ID:        "chatcmpl-stream",
				ModelName: "gpt-4o",
				Choices: []StreamChoice{{
					Index: 0,
					Delta: ChatDelta{Content: "Hello"},
				}},
			},
			{
				ID:        "chatcmpl-stream",
				ModelName: "gpt-4o",
				Choices: []StreamChoice{{
					Index: 0,
					Delta: ChatDelta{Content: " world!"},
				}},
			},
			{
				ID:        "chatcmpl-stream",
				ModelName: "gpt-4o",
				Choices: []StreamChoice{{
					Index:        0,
					Delta:        ChatDelta{},
					FinishReason: "stop",
				}},
			},
		}

		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(writer, "data: %s\n\n", data)
			flusher.Flush()
		}

		fmt.Fprintf(writer, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")

	stream, err := client.ChatCompletionStream(context.Background(), ChatRequest{
		ModelName: "gpt-4o",
		Messages:  []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletionStream: %v", err)
	}

	var textContent string
	var finishReason string
	var gotDone bool

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
}

func TestOpenAIStreamingWithToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := writer.(http.Flusher)

		chunks := []StreamChunk{
			{
				ID:        "chatcmpl-tools",
				ModelName: "gpt-4o",
				Choices: []StreamChoice{{
					Index: 0,
					Delta: ChatDelta{
						Role: "assistant",
						ToolCalls: []ToolCallDelta{{
							Index: 0,
							ID:    "call_abc123",
							Type:  "function",
							Function: FunctionCallDelta{
								Name:      "get_weather",
								Arguments: "",
							},
						}},
					},
				}},
			},
			{
				ID:        "chatcmpl-tools",
				ModelName: "gpt-4o",
				Choices: []StreamChoice{{
					Index: 0,
					Delta: ChatDelta{
						ToolCalls: []ToolCallDelta{{
							Index: 0,
							Function: FunctionCallDelta{
								Arguments: `{"location":`,
							},
						}},
					},
				}},
			},
			{
				ID:        "chatcmpl-tools",
				ModelName: "gpt-4o",
				Choices: []StreamChoice{{
					Index: 0,
					Delta: ChatDelta{
						ToolCalls: []ToolCallDelta{{
							Index: 0,
							Function: FunctionCallDelta{
								Arguments: `"NYC"}`,
							},
						}},
					},
				}},
			},
			{
				ID:        "chatcmpl-tools",
				ModelName: "gpt-4o",
				Choices: []StreamChoice{{
					Index:        0,
					Delta:        ChatDelta{},
					FinishReason: "tool_calls",
				}},
			},
		}

		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(writer, "data: %s\n\n", data)
			flusher.Flush()
		}

		fmt.Fprintf(writer, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")

	stream, err := client.ChatCompletionStream(context.Background(), ChatRequest{
		ModelName: "gpt-4o",
		Messages:  []ChatMessage{{Role: "user", Content: "What's the weather?"}},
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
			toolCallDeltas = append(toolCallDeltas, choice.Delta.ToolCalls...)
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
		}
	}

	if finishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", finishReason)
	}
	if len(toolCallDeltas) < 2 {
		t.Fatalf("expected at least 2 tool call deltas, got %d", len(toolCallDeltas))
	}
	if toolCallDeltas[0].ID != "call_abc123" {
		t.Errorf("first delta id = %q, want call_abc123", toolCallDeltas[0].ID)
	}
	if toolCallDeltas[0].Function.Name != "get_weather" {
		t.Errorf("first delta name = %q, want get_weather", toolCallDeltas[0].Function.Name)
	}

	// Reassemble the arguments.
	var fullArguments string
	for _, delta := range toolCallDeltas {
		fullArguments += delta.Function.Arguments
	}
	if fullArguments != `{"location":"NYC"}` {
		t.Errorf("arguments = %q, want %q", fullArguments, `{"location":"NYC"}`)
	}
}

func TestOpenAIStreamingAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		writer.Write([]byte(`{"error":{"message":"server error"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")

	_, err := client.ChatCompletionStream(context.Background(), ChatRequest{
		ModelName: "gpt-4o",
		Messages:  []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestOpenAIStreamingContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := writer.(http.Flusher)

		// Send one chunk then hang — the client should cancel.
		chunk := StreamChunk{
			ID:        "chatcmpl-cancel",
			ModelName: "gpt-4o",
			Choices: []StreamChoice{{
				Index: 0,
				Delta: ChatDelta{Content: "partial"},
			}},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(writer, "data: %s\n\n", data)
		flusher.Flush()

		// Block until the request context is done (client cancelled).
		<-request.Context().Done()
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")

	ctx, cancel := context.WithCancel(context.Background())

	stream, err := client.ChatCompletionStream(ctx, ChatRequest{
		ModelName: "gpt-4o",
		Messages:  []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletionStream: %v", err)
	}

	// Read the first event.
	event := <-stream
	if event.Chunk == nil || event.Chunk.Choices[0].Delta.Content != "partial" {
		t.Errorf("expected partial content in first event")
	}

	// Cancel the context — the stream should close.
	cancel()

	// Drain remaining events; channel should close without blocking forever.
	for range stream {
		// drain
	}
}

func TestOpenAIListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != "GET" {
			t.Errorf("method = %q, want GET", request.Method)
		}
		if request.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", request.URL.Path)
		}

		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"id": "gpt-4o", "owned_by": "openai"},
				{"id": "gpt-3.5-turbo", "owned_by": "openai"},
				{"id": "dall-e-3", "owned_by": "openai"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")

	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}

	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(models))
	}

	// Should be sorted by ID.
	if models[0].ID != "dall-e-3" {
		t.Errorf("models[0].ID = %q, want dall-e-3", models[0].ID)
	}
	if models[1].ID != "gpt-3.5-turbo" {
		t.Errorf("models[1].ID = %q, want gpt-3.5-turbo", models[1].ID)
	}
	if models[2].ID != "gpt-4o" {
		t.Errorf("models[2].ID = %q, want gpt-4o", models[2].ID)
	}
}

func TestOpenAIListModelsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		writer.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")

	_, err := client.ListModels(context.Background())
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestOpenAINoAuthHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "" {
			t.Error("expected no Authorization header when apiKey is empty")
		}
		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(ChatResponse{
			ID:        "chatcmpl-noauth",
			ModelName: "local-model",
			Choices: []Choice{{
				Index:        0,
				Message:      ChatMessage{Role: "assistant", Content: "ok"},
				FinishReason: "stop",
			}},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "")

	response, err := client.ChatCompletion(context.Background(), ChatRequest{
		ModelName: "local-model",
		Messages:  []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if response.ID != "chatcmpl-noauth" {
		t.Errorf("id = %q", response.ID)
	}
}

func TestOpenAIBaseURLTrailingSlash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(ChatResponse{
			ID:        "chatcmpl-trim",
			ModelName: "gpt-4o",
			Choices:   []Choice{{Message: ChatMessage{Content: "ok"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	// URL with trailing slash should still work.
	client := NewClient(server.URL+"/", "key")

	response, err := client.ChatCompletion(context.Background(), ChatRequest{
		ModelName: "gpt-4o",
		Messages:  []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if response.ID != "chatcmpl-trim" {
		t.Errorf("id = %q", response.ID)
	}
}

func TestOpenAIStreamingInvalidChunkJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := writer.(http.Flusher)

		fmt.Fprintf(writer, "data: {invalid json}\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")

	stream, err := client.ChatCompletionStream(context.Background(), ChatRequest{
		ModelName: "gpt-4o",
		Messages:  []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletionStream: %v", err)
	}

	// Should receive an error event for invalid JSON.
	var gotError bool
	for event := range stream {
		if event.Err != nil {
			gotError = true
			break
		}
	}
	if !gotError {
		t.Error("expected error event for invalid JSON chunk")
	}
}

func TestOpenAINonStreamingWithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var chatRequest ChatRequest
		json.Unmarshal(body, &chatRequest)

		if len(chatRequest.Tools) != 1 {
			t.Errorf("expected 1 tool, got %d", len(chatRequest.Tools))
		}
		if chatRequest.Tools[0].Function.Name != "search" {
			t.Errorf("tool name = %q, want search", chatRequest.Tools[0].Function.Name)
		}

		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(ChatResponse{
			ID:        "chatcmpl-tool",
			ModelName: "gpt-4o",
			Choices: []Choice{{
				Index: 0,
				Message: ChatMessage{
					Role: "assistant",
					ToolCalls: []ToolCall{{
						ID:   "call_xyz",
						Type: "function",
						Function: FunctionCall{
							Name:      "search",
							Arguments: `{"query":"Go testing"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: &UsageInformation{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")

	response, err := client.ChatCompletion(context.Background(), ChatRequest{
		ModelName: "gpt-4o",
		Messages:  []ChatMessage{{Role: "user", Content: "Search for Go testing"}},
		Tools: []ToolDefinition{{
			Type: "function",
			Function: FunctionSpec{
				Name:        "search",
				Description: "Search the web",
				Parameters:  map[string]interface{}{"type": "object"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if response.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", response.Choices[0].FinishReason)
	}
	if len(response.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(response.Choices[0].Message.ToolCalls))
	}
	toolCall := response.Choices[0].Message.ToolCalls[0]
	if toolCall.ID != "call_xyz" {
		t.Errorf("tool call id = %q", toolCall.ID)
	}
	if toolCall.Function.Name != "search" {
		t.Errorf("tool call name = %q", toolCall.Function.Name)
	}
}

func TestOpenAIMaxTokensUsesMaxCompletionTokensForGpt5(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("invalid request body: %v", err)
		}

		if _, exists := payload["max_tokens"]; exists {
			t.Error("did not expect max_tokens for gpt-5 model")
		}
		if got, ok := payload["max_completion_tokens"].(float64); !ok || int(got) != 123 {
			t.Errorf("max_completion_tokens = %v, want 123", payload["max_completion_tokens"])
		}

		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(ChatResponse{
			ID:        "chatcmpl-gpt5",
			ModelName: "gpt-5",
			Choices:   []Choice{{Message: ChatMessage{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		ModelName: "gpt-5",
		MaxTokens: 123,
		Messages:  []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
}

func TestOpenAIMaxTokensStaysLegacyForNonGpt5(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("invalid request body: %v", err)
		}

		if got, ok := payload["max_tokens"].(float64); !ok || int(got) != 77 {
			t.Errorf("max_tokens = %v, want 77", payload["max_tokens"])
		}
		if _, exists := payload["max_completion_tokens"]; exists {
			t.Error("did not expect max_completion_tokens for non-gpt-5 model")
		}

		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(ChatResponse{
			ID:        "chatcmpl-gpt4o",
			ModelName: "gpt-4o",
			Choices:   []Choice{{Message: ChatMessage{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		ModelName: "gpt-4o",
		MaxTokens: 77,
		Messages:  []ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
}
