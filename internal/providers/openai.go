package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/util/deferutil"
)

// Client talks to an OpenAI-compatible chat completions API.
type Client struct {
	baseUrl    string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a provider client.
func NewClient(baseUrl, apiKey string) *Client {
	return &Client{
		baseUrl:    strings.TrimRight(baseUrl, "/"),
		apiKey:     apiKey,
		httpClient: http.DefaultClient,
	}
}

// StreamOptions controls streaming behavior.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// ChatRequest is the request body for chat completions.
type ChatRequest struct {
	Model               string           `json:"model"`
	Messages            []ChatMessage    `json:"messages"`
	Stream              bool             `json:"stream"`
	StreamOptions       *StreamOptions   `json:"stream_options,omitempty"`
	MaxTokens           int              `json:"max_tokens,omitempty"`
	MaxCompletionTokens int              `json:"max_completion_tokens,omitempty"`
	Temperature         *float64         `json:"temperature,omitempty"`
	Tools               []ToolDefinition `json:"tools,omitempty"`
}

// ContentPart represents a single part of multimodal message content.
type ContentPart struct {
	Type     string        `json:"type"`                // "text" or "image_url"
	Text     string        `json:"text,omitempty"`      // for type="text"
	ImageURL *ImageURLPart `json:"image_url,omitempty"` // for type="image_url"
}

// ImageURLPart holds the URL for an image content part.
type ImageURLPart struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto", "low", "high"
}

// ChatMessage is a single message in the conversation.
// Content can be a string (text-only) or []ContentPart (multimodal).
type ChatMessage struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"-"` // string or []ContentPart; custom marshaling below
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Name       string      `json:"name,omitempty"`
}

// ContentText returns the text content of a message as a plain string.
// If Content is a []ContentPart, it concatenates all text parts.
func (self *ChatMessage) ContentText() string {
	if self.Content == nil {
		return ""
	}
	if text, ok := self.Content.(string); ok {
		return text
	}
	if parts, ok := self.Content.([]ContentPart); ok {
		var texts []string
		for _, part := range parts {
			if part.Type == "text" {
				texts = append(texts, part.Text)
			}
		}
		return strings.Join(texts, "")
	}
	return fmt.Sprintf("%v", self.Content)
}

// chatMessageJSON is the wire format for ChatMessage.
type chatMessageJSON struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

// MarshalJSON implements custom JSON marshaling for ChatMessage.
// Emits Content as a string when text-only, or as an array when multimodal.
func (self ChatMessage) MarshalJSON() ([]byte, error) {
	wire := chatMessageJSON{
		Role:       self.Role,
		ToolCalls:  self.ToolCalls,
		ToolCallID: self.ToolCallID,
		Name:       self.Name,
	}
	switch content := self.Content.(type) {
	case string:
		wire.Content, _ = json.Marshal(content)
	case []ContentPart:
		wire.Content, _ = json.Marshal(content)
	case nil:
		// leave Content nil
	default:
		wire.Content, _ = json.Marshal(content)
	}
	return json.Marshal(wire)
}

// UnmarshalJSON implements custom JSON unmarshaling for ChatMessage.
func (self *ChatMessage) UnmarshalJSON(data []byte) error {
	var wire chatMessageJSON
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	self.Role = wire.Role
	self.ToolCalls = wire.ToolCalls
	self.ToolCallID = wire.ToolCallID
	self.Name = wire.Name

	// Try to unmarshal Content as a string first, then as []ContentPart.
	var text string
	if err := json.Unmarshal(wire.Content, &text); err == nil {
		self.Content = text
	} else {
		var parts []ContentPart
		if err := json.Unmarshal(wire.Content, &parts); err == nil {
			self.Content = parts
		} else {
			self.Content = string(wire.Content)
		}
	}
	return nil
}

// ToolDefinition defines a tool available to the model.
type ToolDefinition struct {
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
	PromptTokens             int `json:"prompt_tokens"`
	CompletionTokens         int `json:"completion_tokens"`
	TotalTokens              int `json:"total_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
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
	ctx, cancel := context.WithTimeout(ctx, defaultNonStreamingRequestTimeout)
	defer cancel()

	request.normalizeMaxTokensParam()
	request.Stream = false
	body, _ := json.Marshal(request)

	log.Debugf("POST %s/chat/completions model=%s messages=%d stream=false", self.baseUrl, request.Model, len(request.Messages))

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", self.baseUrl+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if err := self.setHeaders(httpRequest); err != nil {
		return nil, err
	}

	response, err := self.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer response.Body.Close()

	log.Debugf("POST %s/chat/completions status=%d", self.baseUrl, response.StatusCode)

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
	request.normalizeMaxTokensParam()
	request.Stream = true
	body, _ := json.Marshal(request)

	log.Debugf("POST %s/chat/completions model=%s messages=%d stream=true", self.baseUrl, request.Model, len(request.Messages))

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", self.baseUrl+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if err := self.setHeaders(httpRequest); err != nil {
		return nil, err
	}

	response, err := self.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	log.Debugf("POST %s/chat/completions status=%d (stream opened)", self.baseUrl, response.StatusCode)

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

func (self *ChatRequest) normalizeMaxTokensParam() {
	if self.MaxCompletionTokens > 0 || self.MaxTokens <= 0 {
		return
	}
	if usesMaxCompletionTokens(self.Model) {
		self.MaxCompletionTokens = self.MaxTokens
		self.MaxTokens = 0
	}
}

func usesMaxCompletionTokens(model string) bool {
	lower := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(lower, "gpt-5")
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
	ID            string `json:"id" yaml:"id"`
	Created       int64  `json:"created,omitempty" yaml:"created,omitempty"`
	OwnedBy       string `json:"owned_by,omitempty" yaml:"owned_by,omitempty"`
	ContextLength int    `json:"context_length,omitempty" yaml:"context_length,omitempty"`
}

// ListModels fetches available models from the provider's /models endpoint.
func (self *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultModelsRequestTimeout)
	defer cancel()

	httpRequest, err := http.NewRequestWithContext(ctx, "GET", self.baseUrl+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if err := self.setHeaders(httpRequest); err != nil {
		return nil, err
	}

	response, err := self.httpClient.Do(httpRequest)
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

func (self *Client) setHeaders(request *http.Request) error {
	request.Header.Set("Content-Type", "application/json")
	if err := self.setAuthorizationHeader(request); err != nil {
		return err
	}
	return nil
}

func (self *Client) setAuthorizationHeader(request *http.Request) error {
	if self.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+self.apiKey)
	}
	return nil
}

// Transcribe sends audio to the OpenAI Whisper API and returns transcribed text.
func (self *Client) Transcribe(ctx context.Context, request TranscribeRequest) (*TranscribeResponse, error) {
	// Build multipart form body.
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Determine file extension for the form field.
	ext := request.Format
	if ext == "" {
		ext = "webm"
	}
	part, err := writer.CreateFormFile("file", "audio."+ext)
	if err != nil {
		return nil, fmt.Errorf("creating form file: %w", err)
	}
	if _, err := io.Copy(part, request.Audio); err != nil {
		return nil, fmt.Errorf("copying audio data: %w", err)
	}

	writer.WriteField("model", "whisper-1")
	writer.WriteField("response_format", "json")
	if request.Language != "" {
		writer.WriteField("language", request.Language)
	}
	if request.Prompt != "" {
		writer.WriteField("prompt", request.Prompt)
	}
	writer.Close()

	log.Debugf("POST %s/audio/transcriptions format=%s", self.baseUrl, ext)

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", self.baseUrl+"/audio/transcriptions", &body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", writer.FormDataContentType())
	if err := self.setAuthorizationHeader(httpRequest); err != nil {
		return nil, err
	}

	response, err := self.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("transcription API error %d: %s", response.StatusCode, string(responseBody))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding transcription response: %w", err)
	}

	return &TranscribeResponse{Text: result.Text}, nil
}

// Synthesize sends text to the OpenAI TTS API and returns an audio stream.
func (self *Client) Synthesize(ctx context.Context, request SynthesizeRequest) (*SynthesizeResponse, error) {
	voice := request.Voice
	if voice == "" {
		voice = "alloy"
	}
	format := request.Format
	if format == "" {
		format = "mp3"
	}
	speed := request.Speed
	if speed <= 0 {
		speed = 1.0
	}

	payload := map[string]interface{}{
		"model":           "tts-1",
		"input":           request.Text,
		"voice":           voice,
		"response_format": format,
		"speed":           speed,
	}
	body, _ := json.Marshal(payload)

	log.Debugf("POST %s/audio/speech voice=%s format=%s", self.baseUrl, voice, format)

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", self.baseUrl+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if err := self.setHeaders(httpRequest); err != nil {
		return nil, err
	}

	response, err := self.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		responseBody, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("TTS API error %d: %s", response.StatusCode, string(responseBody))
	}

	contentType := "audio/mpeg"
	if format == "opus" {
		contentType = "audio/ogg"
	} else if format == "aac" {
		contentType = "audio/aac"
	} else if format == "flac" {
		contentType = "audio/flac"
	}

	return &SynthesizeResponse{
		Audio:       response.Body,
		Format:      format,
		ContentType: contentType,
	}, nil
}
