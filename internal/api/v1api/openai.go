package v1api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/util/ulid"
)

// openaiRequest mirrors the OpenAI chat completions request format.
type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	Stream      bool            `json:"stream"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	User        string          `json:"user,omitempty"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openaiResponse mirrors the OpenAI chat completions response format.
type openaiResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openaiChoice `json:"choices"`
	Usage   *openaiUsage   `json:"usage,omitempty"`
}

type openaiChoice struct {
	Index        int            `json:"index"`
	Message      *openaiMessage `json:"message,omitempty"`
	Delta        *openaiMessage `json:"delta,omitempty"`
	FinishReason *string        `json:"finish_reason"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (self *API) handleChatCompletions(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, "reading body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var chatRequest openaiRequest
	if err := json.Unmarshal(body, &chatRequest); err != nil {
		http.Error(writer, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(chatRequest.Messages) == 0 {
		http.Error(writer, "messages is required", http.StatusBadRequest)
		return
	}

	// Use user field or generate ephemeral conversation id.
	conversationId := chatRequest.User
	if conversationId == "" {
		conversationId = ulid.GenerateString()
	}

	// Extract the last user message.
	lastMessage := chatRequest.Messages[len(chatRequest.Messages)-1]

	if chatRequest.Stream {
		self.handleChatCompletionsStream(writer, request, chatRequest, conversationId, lastMessage)
	} else {
		self.handleChatCompletionsSync(writer, chatRequest, conversationId, lastMessage)
	}
}

func (self *API) handleChatCompletionsSync(writer http.ResponseWriter, request openaiRequest, conversationId string, lastMessage openaiMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	runner := self.gateway.AgentRegistry().Default()
	if runner == nil {
		http.Error(writer, "no default agent configured", http.StatusInternalServerError)
		return
	}

	result, err := runner.Run(ctx, agents.RunParams{
		ConversationID: conversationId,
		Message:        lastMessage.Content,
		Model:          request.Model,
	}, nil) // no callbacks for sync mode
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	finishReason := "stop"
	if result.StopReason != "" {
		finishReason = result.StopReason
	}

	response := openaiResponse{
		ID:      ulid.GenerateString(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   result.Model,
		Choices: []openaiChoice{
			{
				Index: 0,
				Message: &openaiMessage{
					Role:    "assistant",
					Content: result.Response,
				},
				FinishReason: &finishReason,
			},
		},
	}
	if result.Usage != nil {
		response.Usage = &openaiUsage{
			PromptTokens:     result.Usage.Input,
			CompletionTokens: result.Usage.Output,
			TotalTokens:      result.Usage.Total,
		}
	}

	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(response)
}

func (self *API) handleChatCompletionsStream(writer http.ResponseWriter, httpRequest *http.Request, request openaiRequest, conversationId string, lastMessage openaiMessage) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming not supported", http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	ctx, cancel := context.WithCancel(httpRequest.Context())
	defer cancel()

	responseId := ulid.GenerateString()

	runner := self.gateway.AgentRegistry().Default()
	if runner == nil {
		errData, _ := json.Marshal(map[string]string{"error": "no default agent configured"})
		fmt.Fprintf(writer, "data: %s\n\n", errData)
		flusher.Flush()
		return
	}

	result, err := runner.Run(ctx, agents.RunParams{
		ConversationID: conversationId,
		Message:        lastMessage.Content,
		Model:          request.Model,
	}, &agents.RunCallbacks{
		OnTextDelta: func(text string) {
			chunk := openaiResponse{
				ID:      responseId,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   request.Model,
				Choices: []openaiChoice{
					{
						Index: 0,
						Delta: &openaiMessage{
							Content: text,
						},
					},
				},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(writer, "data: %s\n\n", data)
			flusher.Flush()
		},
	})

	if err != nil {
		errData, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintf(writer, "data: %s\n\n", errData)
		flusher.Flush()
		return
	}

	// Send final chunk with finish_reason.
	finishReason := "stop"
	if result.StopReason != "" {
		finishReason = result.StopReason
	}
	finalChunk := openaiResponse{
		ID:      responseId,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   result.Model,
		Choices: []openaiChoice{
			{
				Index:        0,
				Delta:        &openaiMessage{},
				FinishReason: &finishReason,
			},
		},
	}
	data, _ := json.Marshal(finalChunk)
	fmt.Fprintf(writer, "data: %s\n\n", data)
	fmt.Fprintf(writer, "data: [DONE]\n\n")
	flusher.Flush()
}
