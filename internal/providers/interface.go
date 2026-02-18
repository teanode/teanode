package providers

import "context"

// Provider is the interface for LLM chat completion backends.
type Provider interface {
	ChatCompletion(ctx context.Context, request ChatRequest) (*ChatResponse, error)
	ChatCompletionStream(ctx context.Context, request ChatRequest) (<-chan StreamEvent, error)
	ListModels(ctx context.Context) ([]ModelInfo, error)
}

// NewProvider creates a Provider for the given type. Supported types:
// "anthropic" returns an AnthropicClient; everything else returns an
// OpenAI-compatible Client.
func NewProvider(providerType, baseURL, apiKey string) Provider {
	switch providerType {
	case "anthropic":
		return NewAnthropicClient(baseURL, apiKey)
	default:
		return NewClient(baseURL, apiKey)
	}
}
