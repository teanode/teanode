package gw

import (
	"context"

	"github.com/teanode/teanode/internal/models"
)

type userContextKey string

const contextKeyUser userContextKey = "userContext"
const contextKeySession userContextKey = "sessionContext"

func ContextWithUserAndSession(ctx context.Context, user *models.User, session *models.Session) context.Context {
	withUser := context.WithValue(ctx, contextKeyUser, user)
	return context.WithValue(withUser, contextKeySession, session)
}

func UserFromContext(ctx context.Context) *models.User {
	value, _ := ctx.Value(contextKeyUser).(*models.User)
	return value
}

func SessionFromContext(ctx context.Context) *models.Session {
	value, _ := ctx.Value(contextKeySession).(*models.Session)
	return value
}
