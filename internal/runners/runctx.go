package runners

import "context"

type contextKey int

const (
	contextKeySpawnDepth contextKey = iota
	contextKeyRunner
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
