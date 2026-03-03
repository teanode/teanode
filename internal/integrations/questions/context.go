package questions

import "context"

type contextKeyQuestionBroker struct{}

// ContextWithQuestionBroker enriches a context with a QuestionBroker.
func ContextWithQuestionBroker(ctx context.Context, broker *QuestionBroker) context.Context {
	return context.WithValue(ctx, contextKeyQuestionBroker{}, broker)
}

// QuestionBrokerFromContext returns the QuestionBroker from the context, or nil.
func QuestionBrokerFromContext(ctx context.Context) *QuestionBroker {
	value, _ := ctx.Value(contextKeyQuestionBroker{}).(*QuestionBroker)
	return value
}
