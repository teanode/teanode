package runners

import "context"

type contextKey int

const (
	contextKeySpawnDepth contextKey = iota
	contextKeyRunner
	contextKeyOrigin
	contextKeyVoiceMode
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

// ContextWithOrigin returns a context annotated with the channel origin (e.g. "webui", "telegram").
func ContextWithOrigin(ctx context.Context, origin string) context.Context {
	return context.WithValue(ctx, contextKeyOrigin, origin)
}

// OriginFromContext returns the channel origin from the context, or "".
func OriginFromContext(ctx context.Context) string {
	value, _ := ctx.Value(contextKeyOrigin).(string)
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
