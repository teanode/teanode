package providers

import (
	"context"
	"encoding/json"
	"io"
)

// Provider is a marker interface satisfied by all provider backends.
// Specific capabilities are expressed via optional interfaces such as
// ChatProvider, TranscribeProvider, StreamingTranscribeProvider,
// SynthesizeProvider, and StreamingSynthesizeProvider.  Embed BaseProvider to satisfy this.
type Provider interface {
	isProvider()
}

// BaseProvider is embedded by concrete providers to satisfy the Provider marker.
type BaseProvider struct{}

func (BaseProvider) isProvider() {}

// ChatProvider is an optional capability interface for LLM chat completion.
type ChatProvider interface {
	Provider
	ChatCompletion(ctx context.Context, request ChatRequest) (*ChatResponse, error)
	ChatCompletionStream(ctx context.Context, request ChatRequest) (<-chan StreamEvent, error)
	ListModels(ctx context.Context) ([]ModelInformation, error)
}

// TranscribeProvider is an optional capability interface for speech-to-text.
type TranscribeProvider interface {
	Transcribe(ctx context.Context, request TranscribeRequest) (*TranscribeResponse, error)
}

// StreamingTranscribeProvider is an optional capability interface for real-time STT.
type StreamingTranscribeProvider interface {
	TranscribeStream(ctx context.Context, request StreamTranscribeRequest) (TranscribeStream, error)
}

// SynthesizeProvider is an optional capability interface for text-to-speech.
type SynthesizeProvider interface {
	Synthesize(ctx context.Context, request SynthesizeRequest) (*SynthesizeResponse, error)
}

// StreamingSynthesizeProvider is an optional capability for chunked TTS.
type StreamingSynthesizeProvider interface {
	SynthesizeStream(ctx context.Context, request SynthesizeStreamRequest) (<-chan SynthesizeChunk, error)
}

// RealtimeProvider is an optional capability interface for OpenAI Realtime API connections.
// Providers that implement this can open a bidirectional WebSocket for audio-in/audio-out
// conversations with built-in VAD, STT, and TTS.
type RealtimeProvider interface {
	DialRealtime(ctx context.Context, config RealtimeSessionConfig) (RealtimeConn, error)
}

// RealtimeSessionConfig configures an OpenAI Realtime API session.
type RealtimeSessionConfig struct {
	Model             string                 `json:"model"`
	Voice             string                 `json:"voice"`
	Instructions      string                 `json:"instructions"`
	Tools             []ToolDefinition       `json:"tools,omitempty"`
	Modalities        []string               `json:"modalities"`
	InputAudioFormat  string                 `json:"input_audio_format"`
	OutputAudioFormat string                 `json:"output_audio_format"`
	TurnDetection     *RealtimeTurnDetection `json:"turn_detection,omitempty"`
}

// RealtimeTurnDetection configures server-side VAD for the Realtime API.
type RealtimeTurnDetection struct {
	Type              string  `json:"type"` // "server_vad" or "semantic_vad"
	Threshold         float64 `json:"threshold,omitempty"`
	PrefixPaddingMs   int     `json:"prefix_padding_ms,omitempty"`
	SilenceDurationMs int     `json:"silence_duration_ms,omitempty"`
	CreateResponse    bool    `json:"create_response"`
}

// RealtimeConn is a bidirectional connection to the OpenAI Realtime API.
type RealtimeConn interface {
	// SendJSON sends a JSON event to the Realtime API.
	SendJSON(event map[string]any) error
	// ReadJSON reads the next JSON event from the Realtime API.
	ReadJSON(v any) error
	// Close closes the connection.
	Close() error
}

// RealtimeEvent is a generic event envelope from the Realtime API.
type RealtimeEvent struct {
	Type         string          `json:"type"`
	EventID      string          `json:"event_id,omitempty"`
	SessionID    string          `json:"session_id,omitempty"`
	Session      json.RawMessage `json:"session,omitempty"`
	Response     json.RawMessage `json:"response,omitempty"`
	Item         json.RawMessage `json:"item,omitempty"`
	Delta        string          `json:"delta,omitempty"`
	Audio        string          `json:"audio,omitempty"`
	Transcript   string          `json:"transcript,omitempty"`
	ContentIndex int             `json:"content_index,omitempty"`
	OutputIndex  int             `json:"output_index,omitempty"`
	ItemID       string          `json:"item_id,omitempty"`
	ResponseID   string          `json:"response_id,omitempty"`
	CallID       string          `json:"call_id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Arguments    string          `json:"arguments,omitempty"`
	Error        *RealtimeError  `json:"error,omitempty"`
}

// RealtimeError describes an error from the Realtime API.
type RealtimeError struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Param   string `json:"param,omitempty"`
}

// EmbeddingProvider is an optional capability interface for computing vector embeddings.
type EmbeddingProvider interface {
	Embed(ctx context.Context, model string, inputText string) ([]float64, error)
	EmbedMany(ctx context.Context, model string, inputTexts []string) ([][]float64, error)
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

// StreamTranscribeRequest configures a streaming STT session.
type StreamTranscribeRequest struct {
	SampleRate int
	Channels   int
	Language   string
	Prompt     string
}

// TranscribeStreamEvent carries interim/final transcript deltas from a stream.
type TranscribeStreamEvent struct {
	Type       string // "interim" | "final"
	Text       string
	Confidence float64
	Err        error
}

// TranscribeStream is a duplex audio stream with transcript events.
type TranscribeStream interface {
	SendAudio(pcm []byte) error
	Events() <-chan TranscribeStreamEvent
	Close() error
}

// SynthesizeRequest is the input for text-to-speech.
type SynthesizeRequest struct {
	Text   string
	Voice  string  // "alloy", "echo", "fable", "onyx", "nova", "shimmer"
	Format string  // "mp3" (default, universally supported)
	Speed  float64 // 0.25–4.0, default 1.0
}

// SynthesizeStreamRequest is the input for streaming text-to-speech.
type SynthesizeStreamRequest struct {
	Text         string
	Voice        string
	SampleRateHz int
}

// SynthesizeChunk carries one streaming audio chunk or an error.
type SynthesizeChunk struct {
	Audio []byte
	Err   error
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
	case "deepgram":
		return NewDeepgramClient(baseUrl, apiKey)
	case "elevenlabs":
		return NewElevenLabsClient(baseUrl, apiKey)
	default:
		return NewClient(baseUrl, apiKey)
	}
}
