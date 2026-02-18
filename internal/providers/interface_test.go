package providers

import (
	"testing"
)

func TestNewProvider_Anthropic(t *testing.T) {
	provider := NewProvider("anthropic", "https://api.anthropic.com/v1", "test-key")
	if _, ok := provider.(*AnthropicClient); !ok {
		t.Errorf("expected *AnthropicClient, got %T", provider)
	}
}

func TestNewProvider_OpenAI(t *testing.T) {
	provider := NewProvider("openai", "https://api.openai.com/v1", "test-key")
	if _, ok := provider.(*Client); !ok {
		t.Errorf("expected *Client, got %T", provider)
	}
}

func TestNewProvider_UnknownFallsBackToOpenAI(t *testing.T) {
	provider := NewProvider("ollama", "http://localhost:11434/v1", "")
	if _, ok := provider.(*Client); !ok {
		t.Errorf("expected *Client for unknown provider type, got %T", provider)
	}
}

func TestNewProvider_EmptyType(t *testing.T) {
	provider := NewProvider("", "http://localhost:8080/v1", "")
	if _, ok := provider.(*Client); !ok {
		t.Errorf("expected *Client for empty provider type, got %T", provider)
	}
}
