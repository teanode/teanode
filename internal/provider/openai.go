package provider

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

// Client talks to an OpenAI-compatible chat completions API.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// NewClient creates a provider client.
func NewClient(baseUrl, apiKey string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseUrl, "/"),
		APIKey:     apiKey,
		HTTPClient: http.DefaultClient,
	}
}

// StreamOptions controls streaming behavior.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// ChatRequest is the request body for chat completions.
type ChatRequest struct {
	Model         string         `json:"model"`
	Messages      []ChatMessage  `json:"messages"`
	Stream        bool           `json:"stream"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
	MaxTokens     int            `json:"max_tokens,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	Tools         []ToolDef      `json:"tools,omitempty"`
}

// ChatMessage is a single message in the conversation.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolDef defines a tool available to the model.
type ToolDef struct {
	Type     string       `json:"type"`
	Function FunctionSpec `json:"function"`
}

// FunctionSpec describes a function the model can call.
type FunctionSpec struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
	Returns     interface{} `json:"returns,omitempty"`
}

// ToolCall represents a tool call made by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is the function name and arguments in a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCallDelta is a partial tool call in a streaming response.
type ToolCallDelta struct {
	Index    int               `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function FunctionCallDelta `json:"function,omitempty"`
}

// FunctionCallDelta is partial function call data in a stream.
type FunctionCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ChatResponse is the non-streaming response.
type ChatResponse struct {
	ID      string     `json:"id"`
	Model   string     `json:"model"`
	Choices []Choice   `json:"choices"`
	Usage   *UsageInfo `json:"usage,omitempty"`
}

// Choice is a single completion choice.
type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// UsageInfo contains token usage info.
type UsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk is one piece of a streaming response.
type StreamChunk struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
	Usage   *UsageInfo     `json:"usage,omitempty"`
}

// StreamChoice is a choice delta in a stream chunk.
type StreamChoice struct {
	Index        int       `json:"index"`
	Delta        ChatDelta `json:"delta"`
	FinishReason string    `json:"finish_reason"`
}

// ChatDelta is the incremental content in a stream chunk.
type ChatDelta struct {
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []ToolCallDelta `json:"tool_calls,omitempty"`
}

// ChatCompletion sends a non-streaming chat completion request.
func (self *Client) ChatCompletion(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	request.Stream = false
	body, _ := json.Marshal(request)

	log.Debugf("POST %s/chat/completions model=%s messages=%d stream=false", self.BaseURL, request.Model, len(request.Messages))

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", self.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	self.setHeaders(httpRequest)

	response, err := self.HTTPClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer response.Body.Close()

	log.Debugf("POST %s/chat/completions status=%d", self.BaseURL, response.StatusCode)

	if response.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("API error %d: %s", response.StatusCode, string(responseBody))
	}

	var chatResponse ChatResponse
	if err := json.NewDecoder(response.Body).Decode(&chatResponse); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if chatResponse.Usage != nil {
		log.Debugf("chat completion done model=%s prompt_tokens=%d completion_tokens=%d", chatResponse.Model, chatResponse.Usage.PromptTokens, chatResponse.Usage.CompletionTokens)
	}

	return &chatResponse, nil
}

// ChatCompletionStream sends a streaming chat completion request.
// It returns a channel that receives delta chunks. The channel is closed
// when the stream ends or an error occurs. Errors are sent as a chunk
// with a nil Choices field and the error in a special format.
func (self *Client) ChatCompletionStream(ctx context.Context, request ChatRequest) (<-chan StreamEvent, error) {
	request.Stream = true
	body, _ := json.Marshal(request)

	log.Debugf("POST %s/chat/completions model=%s messages=%d stream=true", self.BaseURL, request.Model, len(request.Messages))

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", self.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	self.setHeaders(httpRequest)

	response, err := self.HTTPClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	log.Debugf("POST %s/chat/completions status=%d (stream opened)", self.BaseURL, response.StatusCode)

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

// StreamEvent wraps either a chunk or an error from the stream.
type StreamEvent struct {
	Chunk *StreamChunk
	Err   error
	Done  bool
}

func (self *Client) readSSE(ctx context.Context, reader io.Reader, events chan<- StreamEvent) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			events <- StreamEvent{Done: true}
			return
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			events <- StreamEvent{Err: fmt.Errorf("parsing stream chunk: %w", err)}
			return
		}
		events <- StreamEvent{Chunk: &chunk}
	}

	if err := scanner.Err(); err != nil {
		events <- StreamEvent{Err: fmt.Errorf("reading stream: %w", err)}
	}
}

// ModelInfo describes a model returned by the /models API.
type ModelInfo struct {
	ID            string `json:"id"`
	Created       int64  `json:"created,omitempty"`
	OwnedBy       string `json:"owned_by,omitempty"`
	ContextLength int    `json:"context_length,omitempty"`
}

// ListModels fetches available models from the provider's /models endpoint.
func (self *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, "GET", self.BaseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	self.setHeaders(httpRequest)

	response, err := self.HTTPClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("models API error %d: %s", response.StatusCode, string(body))
	}

	var result struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding models response: %w", err)
	}

	// Sort by ID for stable ordering.
	sort.Slice(result.Data, func(i, j int) bool {
		return result.Data[i].ID < result.Data[j].ID
	})

	return result.Data, nil
}

func (self *Client) setHeaders(request *http.Request) {
	request.Header.Set("Content-Type", "application/json")
	if self.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+self.APIKey)
	}
}
