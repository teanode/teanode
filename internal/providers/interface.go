package providers

import (
	"context"
	"io"
)

// Provider is the interface for LLM chat completion backends.
type Provider interface {
	ChatCompletion(ctx context.Context, request ChatRequest) (*ChatResponse, error)
	ChatCompletionStream(ctx context.Context, request ChatRequest) (<-chan StreamEvent, error)
	ListModels(ctx context.Context) ([]ModelInformation, error)
}

// AudioTranscriber is an optional capability interface for speech-to-text.
type AudioTranscriber interface {
	Transcribe(ctx context.Context, request TranscribeRequest) (*TranscribeResponse, error)
}

// AudioSynthesizer is an optional capability interface for text-to-speech.
type AudioSynthesizer interface {
	Synthesize(ctx context.Context, request SynthesizeRequest) (*SynthesizeResponse, error)
}

// TranscribeRequest is the input for speech-to-text.
type TranscribeRequest struct {
	Audio    io.Reader
	Format   string // "mp3", "wav", "m4a", "webm", "mp4"
	Language string // optional BCP-47 hint
	Prompt   string // optional transcription hint/context
}

// TranscribeResponse is the output of speech-to-text.
type TranscribeResponse struct {
	Text string
}

// SynthesizeRequest is the input for text-to-speech.
type SynthesizeRequest struct {
	Text   string
	Voice  string  // "alloy", "echo", "fable", "onyx", "nova", "shimmer"
	Format string  // "mp3" (default, universally supported)
	Speed  float64 // 0.25–4.0, default 1.0
}

// SynthesizeResponse is the output of text-to-speech.
type SynthesizeResponse struct {
	Audio       io.ReadCloser
	Format      string
	ContentType string
}

// ModelInformation describes a model returned by the /models API.
type ModelInformation struct {
	ID            string `json:"id" yaml:"id"`
	Created       int64  `json:"created,omitempty" yaml:"created,omitempty"`
	OwnedBy       string `json:"owned_by,omitempty" yaml:"owned_by,omitempty"`
	ContextLength int    `json:"context_length,omitempty" yaml:"context_length,omitempty"`
}

// NewProvider creates a Provider for the given type. Supported types:
// "anthropic" returns an AnthropicClient; everything else returns an
// OpenAI-compatible Client.
func NewProvider(providerType, baseUrl, apiKey string) Provider {
	switch providerType {
	case "anthropic":
		return NewAnthropicClient(baseUrl, apiKey)
	default:
		return NewClient(baseUrl, apiKey)
	}
}
