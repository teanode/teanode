package tab

import "context"

type contextKeyTabToolBroker struct{}

// ContextWithTabToolBroker enriches a context with a TabToolBroker.
func ContextWithTabToolBroker(ctx context.Context, broker *TabToolBroker) context.Context {
	return context.WithValue(ctx, contextKeyTabToolBroker{}, broker)
}

// TabToolBrokerFromContext returns the TabToolBroker from the context, or nil.
func TabToolBrokerFromContext(ctx context.Context) *TabToolBroker {
	value, _ := ctx.Value(contextKeyTabToolBroker{}).(*TabToolBroker)
	return value
}
