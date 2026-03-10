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

// GeminiClient talks to the Google Gemini (Generative Language) API.
type GeminiClient struct {
	BaseProvider
	baseUrl    string
	apiKey     string
	httpClient *http.Client
}

// NewGeminiClient creates a Gemini provider client.
func NewGeminiClient(baseUrl, apiKey string) *GeminiClient {
	return &GeminiClient{
		baseUrl:    strings.TrimRight(baseUrl, "/"),
		apiKey:     apiKey,
		httpClient: http.DefaultClient,
	}
}

// --- Gemini API types ---

type geminiRequest struct {
	Contents         []geminiContent         `json:"contents"`
	SystemInstruct   *geminiContent          `json:"systemInstruction,omitempty"`
	Tools            []geminiToolDeclaration `json:"tools,omitempty"`
	GenerationConfig *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string              `json:"text,omitempty"`
	InlineData       *geminiInlineData   `json:"inlineData,omitempty"`
	FileData         *geminiFileData     `json:"fileData,omitempty"`
	FunctionCall     *geminiFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResponse `json:"functionResponse,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiFileData struct {
	MimeType string `json:"mimeType,omitempty"`
	FileURI  string `json:"fileUri"`
}

type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type geminiFuncResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type geminiToolDeclaration struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations,omitempty"`
}

type geminiFunctionDecl struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

type geminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
}

// --- Response types ---

type geminiResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata *geminiUsage      `json:"usageMetadata,omitempty"`
	ModelVersion  string            `json:"modelVersion,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason,omitempty"`
}

type geminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// --- Models list types ---

type geminiModelsResponse struct {
	Models []geminiModelInfo `json:"models"`
}

type geminiModelInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// --- ChatProvider implementation ---

// ChatCompletion sends a non-streaming chat completion request.
func (self *GeminiClient) ChatCompletion(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultNonStreamingRequestTimeout)
	defer cancel()

	geminiReq := self.translateRequest(request)
	body, _ := json.Marshal(geminiReq)

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", self.baseUrl, request.ModelName, self.apiKey)
	log.Debugf("POST %s/v1beta/models/%s:generateContent messages=%d stream=false", self.baseUrl, request.ModelName, len(request.Messages))

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	self.setHeaders(httpRequest)

	response, err := self.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer response.Body.Close()

	log.Debugf("POST %s/v1beta/models/%s:generateContent status=%d", self.baseUrl, request.ModelName, response.StatusCode)

	if response.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("API error %d: %s", response.StatusCode, string(responseBody))
	}

	var geminiResp geminiResponse
	if err := json.NewDecoder(response.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	chatResponse := self.translateResponse(request.ModelName, geminiResp)

	if chatResponse.Usage != nil {
		log.Debugf("chat completion done model=%s prompt_tokens=%d completion_tokens=%d", chatResponse.ModelName, chatResponse.Usage.PromptTokens, chatResponse.Usage.CompletionTokens)
	}

	return chatResponse, nil
}

// ChatCompletionStream sends a streaming chat completion request.
func (self *GeminiClient) ChatCompletionStream(ctx context.Context, request ChatRequest) (<-chan StreamEvent, error) {
	geminiReq := self.translateRequest(request)
	body, _ := json.Marshal(geminiReq)

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", self.baseUrl, request.ModelName, self.apiKey)
	log.Debugf("POST %s/v1beta/models/%s:streamGenerateContent messages=%d stream=true", self.baseUrl, request.ModelName, len(request.Messages))

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	self.setHeaders(httpRequest)

	response, err := self.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	log.Debugf("POST %s/v1beta/models/%s:streamGenerateContent status=%d (stream opened)", self.baseUrl, request.ModelName, response.StatusCode)

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
		self.readSse(ctx, request.ModelName, response.Body, events)
	}()

	return events, nil
}

// ListModels fetches available models from the Gemini API.
func (self *GeminiClient) ListModels(ctx context.Context) ([]ModelInformation, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultModelsRequestTimeout)
	defer cancel()

	url := fmt.Sprintf("%s/v1beta/models?key=%s", self.baseUrl, self.apiKey)
	httpRequest, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return self.fallbackModels(), nil
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := self.httpClient.Do(httpRequest)
	if err != nil {
		return self.fallbackModels(), nil
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return self.fallbackModels(), nil
	}

	var result geminiModelsResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return self.fallbackModels(), nil
	}

	var models []ModelInformation
	for _, entry := range result.Models {
		// Gemini model names are "models/gemini-2.0-flash"; strip the prefix.
		id := strings.TrimPrefix(entry.Name, "models/")
		// Only include generative models.
		if strings.HasPrefix(id, "gemini") {
			models = append(models, ModelInformation{ID: id})
		}
	}

	sort.Slice(models, func(first, second int) bool {
		return models[first].ID < models[second].ID
	})

	return models, nil
}

func (self *GeminiClient) fallbackModels() []ModelInformation {
	return []ModelInformation{
		{ID: "gemini-2.5-pro"},
		{ID: "gemini-2.5-flash"},
		{ID: "gemini-2.0-flash"},
	}
}

// --- Request translation ---

func (self *GeminiClient) translateRequest(request ChatRequest) geminiRequest {
	systemContent, contents := self.translateMessages(request.Messages)

	result := geminiRequest{
		Contents:       contents,
		SystemInstruct: systemContent,
	}

	if request.Temperature != nil || request.MaxTokens > 0 {
		result.GenerationConfig = &geminiGenerationConfig{
			Temperature: request.Temperature,
		}
		if request.MaxTokens > 0 {
			result.GenerationConfig.MaxOutputTokens = request.MaxTokens
		}
	}

	if len(request.Tools) > 0 {
		result.Tools = self.translateTools(request.Tools)
	}

	return result
}

func (self *GeminiClient) translateMessages(messages []ChatMessage) (*geminiContent, []geminiContent) {
	var systemParts []geminiPart
	var contents []geminiContent

	for _, message := range messages {
		switch message.Role {
		case "system":
			systemParts = append(systemParts, geminiPart{Text: message.ContentText()})

		case "user":
			parts := self.translateUserParts(message)
			content := geminiContent{Role: "user", Parts: parts}
			// Merge consecutive same-role messages.
			if len(contents) > 0 && contents[len(contents)-1].Role == "user" {
				contents[len(contents)-1].Parts = append(contents[len(contents)-1].Parts, parts...)
			} else {
				contents = append(contents, content)
			}

		case "assistant":
			parts := self.translateAssistantParts(message)
			content := geminiContent{Role: "model", Parts: parts}
			if len(contents) > 0 && contents[len(contents)-1].Role == "model" {
				contents[len(contents)-1].Parts = append(contents[len(contents)-1].Parts, parts...)
			} else {
				contents = append(contents, content)
			}

		case "tool":
			part := geminiPart{
				FunctionResponse: &geminiFuncResponse{
					Name:     message.Name,
					Response: self.toolResultJSON(message.ContentText()),
				},
			}
			// Tool results go in a user turn for Gemini (or merge into existing user turn).
			if len(contents) > 0 && contents[len(contents)-1].Role == "user" {
				contents[len(contents)-1].Parts = append(contents[len(contents)-1].Parts, part)
			} else {
				contents = append(contents, geminiContent{Role: "user", Parts: []geminiPart{part}})
			}
		}
	}

	var systemContent *geminiContent
	if len(systemParts) > 0 {
		systemContent = &geminiContent{Parts: systemParts}
	}

	return systemContent, contents
}

func (self *GeminiClient) translateUserParts(message ChatMessage) []geminiPart {
	if parts, ok := message.Content.([]ContentPart); ok {
		var geminiParts []geminiPart
		for _, part := range parts {
			switch part.Type {
			case "text":
				geminiParts = append(geminiParts, geminiPart{Text: part.Text})
			case "image_url":
				if part.ImageURL != nil {
					geminiParts = append(geminiParts, self.translateImagePart(*part.ImageURL))
				}
			}
		}
		if len(geminiParts) == 0 {
			geminiParts = append(geminiParts, geminiPart{Text: ""})
		}
		return geminiParts
	}
	return []geminiPart{{Text: message.ContentText()}}
}

func (self *GeminiClient) translateImagePart(imageUrl ImageURLPart) geminiPart {
	if strings.HasPrefix(imageUrl.URL, "data:") {
		// Parse data URI: data:<mediaType>;base64,<data>
		parts := strings.SplitN(imageUrl.URL, ",", 2)
		if len(parts) == 2 {
			mediaType := strings.TrimPrefix(parts[0], "data:")
			mediaType = strings.TrimSuffix(mediaType, ";base64")
			return geminiPart{
				InlineData: &geminiInlineData{
					MimeType: mediaType,
					Data:     parts[1],
				},
			}
		}
	}
	return geminiPart{
		FileData: &geminiFileData{
			FileURI: imageUrl.URL,
		},
	}
}

func (self *GeminiClient) translateAssistantParts(message ChatMessage) []geminiPart {
	var parts []geminiPart

	if text := message.ContentText(); text != "" {
		parts = append(parts, geminiPart{Text: text})
	}

	for _, toolCall := range message.ToolCalls {
		var args json.RawMessage
		if toolCall.Function.Arguments != "" {
			args = json.RawMessage(toolCall.Function.Arguments)
		} else {
			args = json.RawMessage("{}")
		}
		parts = append(parts, geminiPart{
			FunctionCall: &geminiFunctionCall{
				Name: toolCall.Function.Name,
				Args: args,
			},
		})
	}

	if len(parts) == 0 {
		parts = append(parts, geminiPart{Text: ""})
	}

	return parts
}

func (self *GeminiClient) toolResultJSON(text string) json.RawMessage {
	result := map[string]string{"result": text}
	encoded, _ := json.Marshal(result)
	return encoded
}

func (self *GeminiClient) translateTools(tools []ToolDefinition) []geminiToolDeclaration {
	declarations := make([]geminiFunctionDecl, len(tools))
	for index, tool := range tools {
		declarations[index] = geminiFunctionDecl{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		}
	}
	return []geminiToolDeclaration{{FunctionDeclarations: declarations}}
}

// --- Response translation ---

func (self *GeminiClient) translateResponse(modelName string, response geminiResponse) *ChatResponse {
	message := ChatMessage{Role: "assistant"}

	var toolCalls []ToolCall
	var textParts []string
	finishReason := "stop"

	if len(response.Candidates) > 0 {
		candidate := response.Candidates[0]
		finishReason = self.translateFinishReason(candidate.FinishReason)

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
			if part.FunctionCall != nil {
				arguments, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, ToolCall{
					ID:   fmt.Sprintf("call_%s", part.FunctionCall.Name),
					Type: "function",
					Function: FunctionCall{
						Name:      part.FunctionCall.Name,
						Arguments: string(arguments),
					},
				})
			}
		}
	}

	message.Content = strings.Join(textParts, "")
	message.ToolCalls = toolCalls

	var usage *UsageInformation
	if response.UsageMetadata != nil {
		usage = &UsageInformation{
			PromptTokens:     response.UsageMetadata.PromptTokenCount,
			CompletionTokens: response.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      response.UsageMetadata.TotalTokenCount,
		}
	}

	return &ChatResponse{
		ModelName: modelName,
		Choices: []Choice{{
			Index:        0,
			Message:      message,
			FinishReason: finishReason,
		}},
		Usage: usage,
	}
}

func (self *GeminiClient) translateFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		return "content_filter"
	default:
		return "stop"
	}
}

// --- Streaming SSE ---

func (self *GeminiClient) readSse(ctx context.Context, modelName string, reader io.Reader, events chan<- StreamEvent) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Track tool call indices across chunks for consistent delta indexing.
	toolCallIndex := 0

	// Gemini sends cumulative usageMetadata on every chunk. The runner
	// accumulates usage with +=, so we must emit usage exactly once — at the
	// end — using the last (final) cumulative values.
	var finalUsage *geminiUsage

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var geminiResp geminiResponse
		if err := json.Unmarshal([]byte(data), &geminiResp); err != nil {
			events <- StreamEvent{Err: fmt.Errorf("parsing stream chunk: %w", err)}
			return
		}

		// Always capture the latest cumulative usage.
		if geminiResp.UsageMetadata != nil {
			finalUsage = geminiResp.UsageMetadata
		}

		if len(geminiResp.Candidates) == 0 {
			continue
		}

		candidate := geminiResp.Candidates[0]

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				events <- StreamEvent{
					Chunk: &StreamChunk{
						ModelName: modelName,
						Choices: []StreamChoice{{
							Index: 0,
							Delta: ChatDelta{
								Content: part.Text,
							},
						}},
					},
				}
			}

			if part.FunctionCall != nil {
				arguments, _ := json.Marshal(part.FunctionCall.Args)
				events <- StreamEvent{
					Chunk: &StreamChunk{
						ModelName: modelName,
						Choices: []StreamChoice{{
							Index: 0,
							Delta: ChatDelta{
								ToolCalls: []ToolCallDelta{{
									Index: toolCallIndex,
									ID:    fmt.Sprintf("call_%s", part.FunctionCall.Name),
									Type:  "function",
									Function: FunctionCallDelta{
										Name:      part.FunctionCall.Name,
										Arguments: string(arguments),
									},
								}},
							},
						}},
					},
				}
				toolCallIndex++
			}
		}

		// Emit finish reason when present.
		if candidate.FinishReason != "" {
			events <- StreamEvent{
				Chunk: &StreamChunk{
					ModelName: modelName,
					Choices: []StreamChoice{{
						Index:        0,
						FinishReason: self.translateFinishReason(candidate.FinishReason),
					}},
				},
			}
		}
	}

	// Emit the final cumulative usage exactly once so the runner's += is correct.
	if finalUsage != nil {
		events <- StreamEvent{
			Chunk: &StreamChunk{
				ModelName: modelName,
				Usage: &UsageInformation{
					PromptTokens:     finalUsage.PromptTokenCount,
					CompletionTokens: finalUsage.CandidatesTokenCount,
					TotalTokens:      finalUsage.TotalTokenCount,
				},
			},
		}
	}

	events <- StreamEvent{Done: true}

	if err := scanner.Err(); err != nil {
		events <- StreamEvent{Err: fmt.Errorf("reading stream: %w", err)}
	}
}

func (self *GeminiClient) setHeaders(request *http.Request) {
	request.Header.Set("Content-Type", "application/json")
}
