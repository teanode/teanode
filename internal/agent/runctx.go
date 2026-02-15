package agent

import "context"

type runContextKey string

const contextKeySessionKey runContextKey = "sessionKey"
const contextKeySpawnDepth runContextKey = "spawnDepth"

// DefaultMaxSpawnDepth is the maximum recursion depth for subagent spawning.
const DefaultMaxSpawnDepth = 5

// ContextWithRun enriches a context with the current session key.
func ContextWithRun(ctx context.Context, sessionKey string) context.Context {
	return context.WithValue(ctx, contextKeySessionKey, sessionKey)
}

// SessionKeyFromContext returns the current session key, or "".
func SessionKeyFromContext(ctx context.Context) string {
	value, _ := ctx.Value(contextKeySessionKey).(string)
	return value
}

// ContextWithSpawnDepth returns a context with the given spawn depth.
func ContextWithSpawnDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, contextKeySpawnDepth, depth)
}

// SpawnDepthFromContext returns the current spawn depth, or 0 if unset.
func SpawnDepthFromContext(ctx context.Context) int {
	value, _ := ctx.Value(contextKeySpawnDepth).(int)
	return value
}
