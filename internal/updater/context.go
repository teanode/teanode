package updater

import "context"

type contextKey int

const contextKeyUpdater contextKey = 0

// ContextWithUpdater returns a context enriched with an Updater.
func ContextWithUpdater(ctx context.Context, updater *Updater) context.Context {
	return context.WithValue(ctx, contextKeyUpdater, updater)
}

// UpdaterFromContext returns the Updater from the context, or nil.
func UpdaterFromContext(ctx context.Context) *Updater {
	value, _ := ctx.Value(contextKeyUpdater).(*Updater)
	return value
}
