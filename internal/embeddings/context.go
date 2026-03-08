package embeddings

import "context"

type contextKeyEmbeddings struct{}

// ContextWithProvider enriches a context with an embeddings provider and model name.
func ContextWithProvider(parent context.Context, provider Provider, model string) context.Context {
	return context.WithValue(parent, contextKeyEmbeddings{}, &contextValue{provider: provider, model: model})
}

// ProviderFromContext returns the embeddings provider and model from context, or nil/"" if absent.
func ProviderFromContext(ctx context.Context) (Provider, string) {
	value, ok := ctx.Value(contextKeyEmbeddings{}).(*contextValue)
	if !ok || value == nil {
		return nil, ""
	}
	return value.provider, value.model
}

type contextValue struct {
	provider Provider
	model    string
}
