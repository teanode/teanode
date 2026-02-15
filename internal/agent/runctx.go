package agent

import "context"

type runContextKey string

const contextKeySessionKey runContextKey = "sessionKey"

// ContextWithRun enriches a context with the current session key.
func ContextWithRun(ctx context.Context, sessionKey string) context.Context {
	return context.WithValue(ctx, contextKeySessionKey, sessionKey)
}

// SessionKeyFromContext returns the current session key, or "".
func SessionKeyFromContext(ctx context.Context) string {
	value, _ := ctx.Value(contextKeySessionKey).(string)
	return value
}
