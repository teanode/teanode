package pubsub

import "context"

type contextKeyPubSub struct{}

// ContextWithPubSub enriches a context with a PubSub instance.
func ContextWithPubSub(parent context.Context, ps *PubSub) context.Context {
	return context.WithValue(parent, contextKeyPubSub{}, ps)
}

// PubSubFromContext returns the PubSub from the context, or nil if not present.
func PubSubFromContext(ctx context.Context) *PubSub {
	value := ctx.Value(contextKeyPubSub{})
	ps, _ := value.(*PubSub)
	return ps
}
