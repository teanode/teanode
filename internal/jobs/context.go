package jobs

import (
	"context"
	"fmt"
)

type contextKeyScheduler struct{}

func ContextWithScheduler(parent context.Context, scheduler *Scheduler) context.Context {
	return context.WithValue(parent, contextKeyScheduler{}, scheduler)
}

func SchedulerFromContext(ctx context.Context) *Scheduler {
	value := ctx.Value(contextKeyScheduler{})
	scheduler, ok := value.(*Scheduler)
	if !ok || scheduler == nil {
		panic(fmt.Sprintf("scheduler is missing from context: %T", value))
	}
	return scheduler
}
