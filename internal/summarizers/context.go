package summarizers

import "context"

type contextKeySummarizer struct{}

// ContextWithSummarizer stores the Summarizer in the context.
func ContextWithSummarizer(ctx context.Context, summarizer *Summarizer) context.Context {
	return context.WithValue(ctx, contextKeySummarizer{}, summarizer)
}

// SummarizerFromContext returns the Summarizer from the context, or nil.
func SummarizerFromContext(ctx context.Context) *Summarizer {
	value, _ := ctx.Value(contextKeySummarizer{}).(*Summarizer)
	return value
}
