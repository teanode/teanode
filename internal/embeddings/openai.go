package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIProvider implements Provider using the OpenAI /v1/embeddings API.
type OpenAIProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewOpenAIProvider creates a new OpenAI embeddings provider.
func NewOpenAIProvider(baseURL, apiKey string, timeoutSeconds int) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	return &OpenAIProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
		},
	}
}

type openAIEmbeddingRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

type openAIEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Embed calls the OpenAI embeddings API and returns the resulting vector.
func (self *OpenAIProvider) Embed(ctx context.Context, model string, inputText string) ([]float32, error) {
	if self.apiKey == "" {
		return nil, fmt.Errorf("embeddings: API key not configured")
	}

	requestBody := openAIEmbeddingRequest{
		Input: inputText,
		Model: model,
	}
	encoded, marshalError := json.Marshal(requestBody)
	if marshalError != nil {
		return nil, fmt.Errorf("embeddings: marshal request: %w", marshalError)
	}

	request, requestError := http.NewRequestWithContext(ctx, http.MethodPost, self.baseURL+"/embeddings", bytes.NewReader(encoded))
	if requestError != nil {
		return nil, fmt.Errorf("embeddings: create request: %w", requestError)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+self.apiKey)

	response, doError := self.client.Do(request)
	if doError != nil {
		return nil, fmt.Errorf("embeddings: request failed: %w", doError)
	}
	defer response.Body.Close()

	body, readError := io.ReadAll(response.Body)
	if readError != nil {
		return nil, fmt.Errorf("embeddings: read response: %w", readError)
	}

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings: HTTP %d: %s", response.StatusCode, string(body))
	}

	var result openAIEmbeddingResponse
	if unmarshalError := json.Unmarshal(body, &result); unmarshalError != nil {
		return nil, fmt.Errorf("embeddings: parse response: %w", unmarshalError)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("embeddings: API error: %s", result.Error.Message)
	}

	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embeddings: empty response")
	}

	return result.Data[0].Embedding, nil
}
