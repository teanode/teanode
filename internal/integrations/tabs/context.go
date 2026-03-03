package tabs

import "context"

type contextKeyTabBroker struct{}

// ContextWithTabBroker enriches a context with a TabBroker.
func ContextWithTabBroker(ctx context.Context, broker *TabBroker) context.Context {
	return context.WithValue(ctx, contextKeyTabBroker{}, broker)
}

// TabBrokerFromContext returns the TabBroker from the context, or nil.
func TabBrokerFromContext(ctx context.Context) *TabBroker {
	value, _ := ctx.Value(contextKeyTabBroker{}).(*TabBroker)
	return value
}
