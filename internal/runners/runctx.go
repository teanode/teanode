package runners

import (
	"context"

	"github.com/teanode/teanode/internal/models"
)

type contextKey int

const (
	contextKeySpawnDepth contextKey = iota
	contextKeyRunner
	contextKeyOrigin
	contextKeyVoiceMode
	contextKeyConversationHistory
)

// Origin represents the channel through which a message was sent.
type Origin string

const (
	OriginNone    Origin = ""        // automated or unspecified
	OriginWeb     Origin = "web"     // web interface
	OriginAPI     Origin = "api"     // REST API (OpenAI-compatible endpoint)
	OriginChannel Origin = "channel" // external channel (Telegram, Discord, etc.)
)

// VoiceMode represents the type of voice interaction.
type VoiceMode string

const (
	VoiceModeNone  VoiceMode = ""      // normal text interaction
	VoiceModeCall  VoiceMode = "call"  // live voice call (server or client STT)
	VoiceModeInput VoiceMode = "input" // one-off voice-dictated message
)

// DefaultMaxSpawnDepth is the maximum recursion depth for subagent spawning.
const DefaultMaxSpawnDepth = 5

// ContextWithSpawnDepth returns a context with the given spawn depth.
func ContextWithSpawnDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, contextKeySpawnDepth, depth)
}

// SpawnDepthFromContext returns the current spawn depth, or 0 if unset.
func SpawnDepthFromContext(ctx context.Context) int {
	value, _ := ctx.Value(contextKeySpawnDepth).(int)
	return value
}

// ContextWithRunner enriches a context with the current runner.
func ContextWithRunner(ctx context.Context, runner *Runner) context.Context {
	return context.WithValue(ctx, contextKeyRunner, runner)
}

// RunnerFromContext returns the Runner from the context, or nil.
func RunnerFromContext(ctx context.Context) *Runner {
	value, _ := ctx.Value(contextKeyRunner).(*Runner)
	return value
}

// ContextWithOrigin returns a context annotated with the channel origin.
func ContextWithOrigin(ctx context.Context, origin Origin) context.Context {
	return context.WithValue(ctx, contextKeyOrigin, origin)
}

// OriginFromContext returns the channel origin from the context, or OriginNone.
func OriginFromContext(ctx context.Context) Origin {
	value, _ := ctx.Value(contextKeyOrigin).(Origin)
	return value
}

// ContextWithVoiceMode returns a context annotated with a voice mode.
func ContextWithVoiceMode(ctx context.Context, mode VoiceMode) context.Context {
	return context.WithValue(ctx, contextKeyVoiceMode, mode)
}

// VoiceModeFromContext returns the voice mode from the context, or VoiceModeNone.
func VoiceModeFromContext(ctx context.Context) VoiceMode {
	value, _ := ctx.Value(contextKeyVoiceMode).(VoiceMode)
	return value
}

// ContextWithConversationHistory returns a context with the conversation history attached.
func ContextWithConversationHistory(ctx context.Context, history []*models.ConversationMessage) context.Context {
	return context.WithValue(ctx, contextKeyConversationHistory, history)
}

// ConversationHistoryFromContext returns the conversation history from the context, or nil.
func ConversationHistoryFromContext(ctx context.Context) []*models.ConversationMessage {
	value, _ := ctx.Value(contextKeyConversationHistory).([]*models.ConversationMessage)
	return value
}
