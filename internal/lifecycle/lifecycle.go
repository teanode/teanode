// Package lifecycle provides gateway lifecycle control via context propagation.
package lifecycle

import (
	"context"
	"sync"
)

// Action identifies a gateway lifecycle request.
type Action int

const (
	Shutdown Action = iota
	Restart
)

// Lifecycle manages gateway lifecycle transitions (restart, shutdown).
type Lifecycle interface {
	// RequestLifecycle sends a lifecycle action immediately (non-blocking).
	RequestLifecycle(action Action)

	// ScheduleLifecycle stores a pending action that fires after the current
	// agent run completes (via FirePendingLifecycle).
	ScheduleLifecycle(action Action)

	// FirePendingLifecycle delivers any previously scheduled action and clears it.
	FirePendingLifecycle()

	// Channel returns the channel on which lifecycle actions are delivered.
	Channel() <-chan Action
}

// New creates a new Lifecycle manager.
func New() Lifecycle {
	return &manager{
		channel: make(chan Action, 1),
	}
}

type manager struct {
	channel       chan Action
	pendingMutex  sync.Mutex
	pendingAction *Action
}

func (self *manager) RequestLifecycle(action Action) {
	select {
	case self.channel <- action:
	default:
	}
}

func (self *manager) ScheduleLifecycle(action Action) {
	self.pendingMutex.Lock()
	self.pendingAction = &action
	self.pendingMutex.Unlock()
}

func (self *manager) FirePendingLifecycle() {
	self.pendingMutex.Lock()
	action := self.pendingAction
	self.pendingAction = nil
	self.pendingMutex.Unlock()

	if action != nil {
		self.RequestLifecycle(*action)
	}
}

func (self *manager) Channel() <-chan Action {
	return self.channel
}

type contextKey int

const contextKeyLifecycle contextKey = 0

// ContextWithLifecycle returns a context enriched with a Lifecycle.
func ContextWithLifecycle(ctx context.Context, lifecycle Lifecycle) context.Context {
	return context.WithValue(ctx, contextKeyLifecycle, lifecycle)
}

// LifecycleFromContext returns the Lifecycle from the context, or nil.
func LifecycleFromContext(ctx context.Context) Lifecycle {
	value, _ := ctx.Value(contextKeyLifecycle).(Lifecycle)
	return value
}
