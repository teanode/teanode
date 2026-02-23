package gw

import "context"

type AuthMethod string

const (
	AuthMethodSession AuthMethod = "session"
	AuthMethodToken   AuthMethod = "token"
)

type UserContext struct {
	UserID     string
	SessionID  string
	AuthMethod AuthMethod
}

type userContextKey string

const contextKeyUser userContextKey = "userContext"

func ContextWithUser(ctx context.Context, user *UserContext) context.Context {
	return context.WithValue(ctx, contextKeyUser, user)
}

func UserFromContext(ctx context.Context) *UserContext {
	value, _ := ctx.Value(contextKeyUser).(*UserContext)
	return value
}
