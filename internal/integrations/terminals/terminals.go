// Package terminals provides a WebSocket relay for PTY-backed terminal connections.
package terminals

import (
	"context"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("terminals")

type terminalContextKey struct{}

// ContextWithTerminal returns a new context with the given Relay attached.
func ContextWithTerminal(ctx context.Context, relay *Relay) context.Context {
	return context.WithValue(ctx, terminalContextKey{}, relay)
}

// TerminalFromContext returns the Relay stored in ctx, or nil.
func TerminalFromContext(ctx context.Context) *Relay {
	relay, _ := ctx.Value(terminalContextKey{}).(*Relay)
	return relay
}
