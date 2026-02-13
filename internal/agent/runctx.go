package agent

import "context"

type runContextKey string

const (
	contextKeySessionKey    runContextKey = "sessionKey"
	contextKeyTitleCallback runContextKey = "titleCallback"
)

// ContextWithRun enriches a context with the current session key and optional title callback.
func ContextWithRun(ctx context.Context, sessionKey string, onTitleUpdate func(string)) context.Context {
	ctx = context.WithValue(ctx, contextKeySessionKey, sessionKey)
	if onTitleUpdate != nil {
		ctx = context.WithValue(ctx, contextKeyTitleCallback, onTitleUpdate)
	}
	return ctx
}

// SessionKeyFromContext returns the current session key, or "".
func SessionKeyFromContext(ctx context.Context) string {
	value, _ := ctx.Value(contextKeySessionKey).(string)
	return value
}

// TitleCallbackFromContext returns the title update callback, or nil.
func TitleCallbackFromContext(ctx context.Context) func(string) {
	value, _ := ctx.Value(contextKeyTitleCallback).(func(string))
	return value
}
