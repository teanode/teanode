package coordinators

import (
	"context"

	"github.com/teanode/teanode/internal/runners"
)

type contextKey int

const (
	contextKeyCoordinator contextKey = iota
)

// RunCoordinator routes messages to runners, creating or reusing runners as needed.
type RunCoordinator interface {
	Run(ctx context.Context, parameters RunParameters, callbacks *runners.RunCallbacks) (*RunHandle, error)
	CompactConversation(ctx context.Context, agentId, conversationId string) (*runners.CompactResult, error)
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
