package agents

import "context"

type runContextKey string

const contextKeyConversationId runContextKey = "conversationId"
const contextKeySpawnDepth runContextKey = "spawnDepth"
const contextKeyRunner runContextKey = "runner"
const contextKeyUserId runContextKey = "userId"
const contextKeyAdmin runContextKey = "isAdmin"

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

// contextWithRunner enriches a context with the current runner.
func contextWithRunner(ctx context.Context, runner *Runner) context.Context {
	return context.WithValue(ctx, contextKeyRunner, runner)
}

// RunnerFromContext returns the Runner from the context, or nil.
func RunnerFromContext(ctx context.Context) *Runner {
	value, _ := ctx.Value(contextKeyRunner).(*Runner)
	return value
}

// ContextWithUserID enriches context with the authenticated user id.
func ContextWithUserID(ctx context.Context, userId string) context.Context {
	return context.WithValue(ctx, contextKeyUserId, userId)
}

// UserIDFromContext returns the authenticated user id, or "".
func UserIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(contextKeyUserId).(string)
	return value
}

// ContextWithAdmin enriches context with whether the current user is an admin.
func ContextWithAdmin(ctx context.Context, isAdmin bool) context.Context {
	return context.WithValue(ctx, contextKeyAdmin, isAdmin)
}

// IsAdminFromContext returns whether the current user is an admin.
func IsAdminFromContext(ctx context.Context) bool {
	value, _ := ctx.Value(contextKeyAdmin).(bool)
	return value
}

// AdminFromContext returns admin status and whether it was explicitly set.
func AdminFromContext(ctx context.Context) (bool, bool) {
	value, ok := ctx.Value(contextKeyAdmin).(bool)
	return value, ok
}
