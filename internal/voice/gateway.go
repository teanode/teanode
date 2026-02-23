package voice

import "context"

// GatewayDeps is the minimum gateway surface the voice session depends on.
type GatewayDeps interface {
	SendMessage(ctx context.Context, parameters VoiceSendMessageParams) VoiceRunHandle
	AbortRun(runId string) bool
	CancelRun(runId string)
	Subscribe(sub VoiceSubscriber)
	Unsubscribe(sub VoiceSubscriber)
	NewConversation(agentId, model string) string
	DefaultAgentID() string
	ProviderRegistry() VoiceProviderRegistry
}

// VoiceSendMessageParams holds message creation fields required by voice.
type VoiceSendMessageParams struct {
	AgentID            string
	ConversationID     string
	Message            string
	Model              string
	SystemPromptSuffix string
	IsSpeculative      bool
}

// VoiceRunHandle is a simplified run-handle used by voice.
type VoiceRunHandle struct {
	RunID          string
	ConversationID string
	Done           <-chan struct{}
}

// VoiceSubscriber receives gateway broadcasts used by voice.
type VoiceSubscriber interface {
	OnVoiceEvent(eventType string, payload interface{})
}

// VoiceProviderRegistry exposes optional voice-capable providers.
type VoiceProviderRegistry interface {
	FindTranscriber() (VoiceTranscriber, string, bool)
	FindStreamingTranscriber() (VoiceStreamingTranscriber, string, bool)
	FindSynthesizer() (VoiceSynthesizer, string, bool)
}

// VoiceTranscriber converts user audio to text.
type VoiceTranscriber interface {
	Transcribe(ctx context.Context, request VoiceTranscribeRequest) (*VoiceTranscribeResponse, error)
}

// VoiceTranscribeRequest is the normalized STT request used in voice.
type VoiceTranscribeRequest struct {
	Audio      []byte
	Format     string
	Language   string
	Prompt     string
	SampleRate int
	Channels   int
}

// VoiceTranscribeResponse is the normalized STT result.
type VoiceTranscribeResponse struct {
	Text string
}

// VoiceStreamingTranscriber opens a realtime STT stream.
type VoiceStreamingTranscriber interface {
	OpenTranscribeStream(ctx context.Context, request VoiceStreamTranscribeRequest) (VoiceTranscribeStream, error)
}

// VoiceStreamTranscribeRequest configures realtime STT.
type VoiceStreamTranscribeRequest struct {
	SampleRate int
	Channels   int
	Language   string
	Prompt     string
}

// VoiceTranscribeEvent is emitted by realtime STT streams.
type VoiceTranscribeEvent struct {
	Type       string // "interim" | "final"
	Text       string
	Confidence float64
	Err        error
}

// VoiceTranscribeStream is a duplex voice STT stream.
type VoiceTranscribeStream interface {
	SendAudio(pcm []byte) error
	Events() <-chan VoiceTranscribeEvent
	Close() error
}

// VoiceSynthesizer generates speech audio bytes.
type VoiceSynthesizer interface {
	SynthesizePCM(ctx context.Context, text, voice string, sampleRateHz int) ([]byte, error)
	SynthesizePCMStream(ctx context.Context, text, voice string, sampleRateHz int) (<-chan []byte, error)
}

// AudioDenoiser is a placeholder capability for future server-side denoise.
type AudioDenoiser interface {
	Denoise(ctx context.Context, pcm []byte, sampleRateHz int, channels int) ([]byte, error)
}
