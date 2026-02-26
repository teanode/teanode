package runners

import "context"

type contextKey int

const (
	contextKeySpawnDepth contextKey = iota
	contextKeyRunner
	contextKeyCoordinator
)

// DefaultMaxSpawnDepth is the maximum recursion depth for subagent spawning.
const DefaultMaxSpawnDepth = 5

// RunCoordinator routes messages to runners, creating or reusing runners as needed.
type RunCoordinator interface {
	SendMessage(ctx context.Context, agentId, conversationId string, params RunParams, callbacks *RunCallbacks) (*RunResult, error)
	CompactConversation(ctx context.Context, agentId, conversationId string) (*CompactResult, error)
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

// ContextWithRunner enriches a context with the current runner.
func ContextWithRunner(ctx context.Context, runner *Runner) context.Context {
	return context.WithValue(ctx, contextKeyRunner, runner)
}

// RunnerFromContext returns the Runner from the context, or nil.
func RunnerFromContext(ctx context.Context) *Runner {
	value, _ := ctx.Value(contextKeyRunner).(*Runner)
	return value
}

// ContextWithCoordinator enriches a context with a RunCoordinator.
func ContextWithCoordinator(ctx context.Context, coordinator RunCoordinator) context.Context {
	return context.WithValue(ctx, contextKeyCoordinator, coordinator)
}

// CoordinatorFromContext returns the RunCoordinator from the context, or nil.
func CoordinatorFromContext(ctx context.Context) RunCoordinator {
	value, _ := ctx.Value(contextKeyCoordinator).(RunCoordinator)
	return value
}
