package v1api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/web"
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

func (self *v1Api) handleChatCompletions(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodPost {
		return web.ErrMethodNotAllowed
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		return web.Errorf(400, "reading body: %s", err)
	}

	var chatRequest openaiRequest
	if err := json.Unmarshal(body, &chatRequest); err != nil {
		return web.Errorf(400, "invalid json: %s", err)
	}

	if len(chatRequest.Messages) == 0 {
		return web.Error(400, "messages is required")
	}

	// Use user field or generate ephemeral conversation id.
	conversationId := chatRequest.User
	if conversationId == "" {
		conversationId = security.NewULID()
	}

	// Extract the last user message.
	lastMessage := chatRequest.Messages[len(chatRequest.Messages)-1]

	user := models.UserFromContext(request.Context())
	if user == nil || user.ID == "" {
		return web.Error(http.StatusUnauthorized, "unauthorized")
	}
	agentId := user.GetDefaultAgentID()
	agentExists := false
	_ = store.StoreFromContext(request.Context()).Transaction(request.Context(), func(ctx context.Context, transaction store.Transaction) error {
		if _, getError := transaction.GetAgent(ctx, agentId, nil); getError == nil {
			agentExists = true
		}
		return nil
	})
	if !agentExists {
		return web.Error(500, "no default agent configured")
	}

	if chatRequest.Stream {
		return self.handleChatCompletionsStream(writer, request, chatRequest, agentId, conversationId, lastMessage)
	}
	return self.handleChatCompletionsSync(writer, request, chatRequest, agentId, conversationId, lastMessage)
}

func (self *v1Api) handleChatCompletionsSync(writer http.ResponseWriter, httpRequest *http.Request, request openaiRequest, agentId string, conversationId string, lastMessage openaiMessage) error {
	ctx, cancel := context.WithTimeout(httpRequest.Context(), 5*time.Minute)
	defer cancel()

	handle, sendError := self.coordinator.SendMessage(ctx, coordinators.SendMessageParameters{
		AgentID:        agentId,
		ConversationID: conversationId,
		Message:        lastMessage.Content,
		Model:          request.Model,
	}, nil) // no callbacks for sync mode
	if sendError != nil {
		return web.Error(500, sendError.Error())
	}
	result, _, err := handle.Wait()
	if err != nil {
		return web.Error(500, err.Error())
	}

	finishReason := "stop"
	if result.StopReason != "" {
		finishReason = result.StopReason
	}

	response := openaiResponse{
		ID:      security.NewULID(),
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
			PromptTokens:     result.Usage["input"],
			CompletionTokens: result.Usage["output"],
			TotalTokens:      result.Usage["totalTokens"],
		}
	}

	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(response)
	return nil
}

func (self *v1Api) handleChatCompletionsStream(writer http.ResponseWriter, httpRequest *http.Request, request openaiRequest, agentId string, conversationId string, lastMessage openaiMessage) error {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return web.Error(500, "streaming not supported")
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	ctx, cancel := context.WithCancel(httpRequest.Context())
	defer cancel()

	responseId := security.NewULID()

	handle, sendError := self.coordinator.SendMessage(ctx, coordinators.SendMessageParameters{
		AgentID:        agentId,
		ConversationID: conversationId,
		Message:        lastMessage.Content,
		Model:          request.Model,
	}, &runners.RunCallbacks{
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
	if sendError != nil {
		errData, _ := json.Marshal(map[string]string{"error": sendError.Error()})
		fmt.Fprintf(writer, "data: %s\n\n", errData)
		flusher.Flush()
		return nil
	}

	result, _, err := handle.Wait()
	if err != nil {
		errData, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintf(writer, "data: %s\n\n", errData)
		flusher.Flush()
		return nil
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
	return nil
}
