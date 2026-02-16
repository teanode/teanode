package agents

import "context"

type runContextKey string

const contextKeyConversationId runContextKey = "conversationId"
const contextKeySpawnDepth runContextKey = "spawnDepth"

// DefaultMaxSpawnDepth is the maximum recursion depth for subagent spawning.
const DefaultMaxSpawnDepth = 5

// ContextWithRun enriches a context with the current conversation id.
func ContextWithRun(ctx context.Context, conversationId string) context.Context {
	return context.WithValue(ctx, contextKeyConversationId, conversationId)
}

// ConversationIDFromContext returns the current conversation id, or "".
func ConversationIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(contextKeyConversationId).(string)
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
