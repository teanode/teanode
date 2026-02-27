package models

import "context"

type contextKey int

const (
	contextKeyUser contextKey = iota
	contextKeySession
	contextKeyToken
)

// ContextWithUserSessionToken enriches a context with the authenticated user and session.
func ContextWithUserSessionToken(ctx context.Context, user *User, session *Session, token *Token) context.Context {
	ctx = context.WithValue(ctx, contextKeyUser, user)
	ctx = context.WithValue(ctx, contextKeySession, session)
	ctx = context.WithValue(ctx, contextKeyToken, token)
	return ctx
}

// UserFromContext returns the authenticated user from context, or nil.
func UserFromContext(ctx context.Context) *User {
	value, _ := ctx.Value(contextKeyUser).(*User)
	return value
}

// SessionFromContext returns the session from context, or nil.
func SessionFromContext(ctx context.Context) *Session {
	value, _ := ctx.Value(contextKeySession).(*Session)
	return value
}

// TokenFromContext returns the token from context, or nil.
func TokenFromContext(ctx context.Context) *Token {
	value, _ := ctx.Value(contextKeyToken).(*Token)
	return value
}
