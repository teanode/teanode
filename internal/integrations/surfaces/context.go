package surfaces

import "context"

type contextKeySurfaceBroker struct{}

// ContextWithSurfaceBroker enriches a context with a SurfaceBroker.
func ContextWithSurfaceBroker(ctx context.Context, broker *SurfaceBroker) context.Context {
	return context.WithValue(ctx, contextKeySurfaceBroker{}, broker)
}

// SurfaceBrokerFromContext returns the SurfaceBroker from the context, or nil.
func SurfaceBrokerFromContext(ctx context.Context) *SurfaceBroker {
	broker, _ := ctx.Value(contextKeySurfaceBroker{}).(*SurfaceBroker)
	return broker
}
