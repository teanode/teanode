package approvals

import "context"

type contextKey struct{}

func ContextWithApprovalBroker(ctx context.Context, broker *ApprovalBroker) context.Context {
	return context.WithValue(ctx, contextKey{}, broker)
}

func ApprovalBrokerFromContext(ctx context.Context) *ApprovalBroker {
	value := ctx.Value(contextKey{})
	if value == nil {
		return nil
	}
	broker, _ := value.(*ApprovalBroker)
	return broker
}
