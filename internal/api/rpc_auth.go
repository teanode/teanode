package api

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/web"
)

// --- Sessions RPC handlers ---

// handleSessionsList: list all active sessions.
func (self *webSocketConnection) handleSessionsList(frame requestFrame) (interface{}, error) {
	filteredSessions := make([]*models.Session, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		sessionList, err := transaction.ListSessions(ctx, nil)
		if err != nil {
			return err
		}
		for _, session := range sessionList {
			if session.GetUserID() != self.userId() {
				continue
			}
			filteredSessions = append(filteredSessions, session)
		}
		return nil
	}); err != nil {
		return nil, rpcError(500, "listing sessions: "+err.Error())
	}
	return map[string]interface{}{
		"sessions":         filteredSessions,
		"currentSessionId": self.sessionId(),
	}, nil
}

// sessionsRevokeParameters are the parameters for sessions.revoke.
type sessionsRevokeParameters struct {
	SessionID string `json:"sessionId"`
}

// handleSessionsRevoke: revoke (delete) a session.
func (self *webSocketConnection) handleSessionsRevoke(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParams[sessionsRevokeParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.SessionID == "" {
		return nil, rpcError(400, "sessionId is required")
	}
	if parameters.SessionID == self.sessionId() {
		return nil, rpcError(400, "cannot revoke the current session")
	}

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		session, err := transaction.GetSession(ctx, parameters.SessionID, nil)
		if err != nil {
			return err
		}
		if session.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		return transaction.DeleteSession(ctx, parameters.SessionID, nil)
	}); err != nil {
		return nil, rpcError(404, "session not found: "+parameters.SessionID)
	}

	return map[string]interface{}{
		"revoked": true,
	}, nil
}

// --- Auth Token RPC handlers ---

type authTokenListItem struct {
	ID            string     `json:"id"`
	Token         string     `json:"token"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastUsedAt    *time.Time `json:"lastUsedAt,omitempty"`
	RemoteAddress string     `json:"remoteAddress,omitempty"`
	UserAgent     string     `json:"userAgent,omitempty"`
}

func toModelAuthTokenListItems(tokens []*models.Token) []authTokenListItem {
	items := make([]authTokenListItem, 0, len(tokens))
	for _, token := range tokens {
		tokenId := token.ID
		tokenValue := token.GetToken()
		if tokenId == "" || tokenValue == "" {
			continue
		}
		item := authTokenListItem{
			ID:            tokenId,
			Token:         tokenValue,
			RemoteAddress: token.GetRemoteAddress(),
			UserAgent:     token.GetUserAgent(),
		}
		if token.CreatedAt != nil {
			item.CreatedAt = *token.CreatedAt
		}
		if token.LastUsedAt != nil {
			lastUsedAt := *token.LastUsedAt
			item.LastUsedAt = &lastUsedAt
		}
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].CreatedAt.After(items[right].CreatedAt)
	})
	return items
}

func (self *webSocketConnection) handleAuthTokensList(frame requestFrame) (interface{}, error) {
	items := make([]authTokenListItem, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		tokens, err := transaction.ListTokens(ctx, self.userId(), nil)
		if err != nil {
			return err
		}
		items = toModelAuthTokenListItems(tokens)
		return nil
	}); err != nil {
		return nil, rpcError(500, "failed to list tokens")
	}
	return map[string]interface{}{
		"tokens": items,
	}, nil
}

func (self *webSocketConnection) handleAuthTokensCreate(frame requestFrame) (interface{}, error) {
	tokenValue := security.GenerateRandomString(48, security.LowerAlphaNumeric)
	var createdItem authTokenListItem
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		createdToken, err := transaction.CreateToken(ctx, &models.Token{
			ID:     security.NewULID(),
			UserID: ptrto.Value(self.userId()),
			Token:  ptrto.TrimmedString(tokenValue),
		}, nil)
		if err != nil {
			return err
		}
		createdItem = authTokenListItem{
			ID:    createdToken.ID,
			Token: createdToken.GetToken(),
		}
		if createdToken.CreatedAt != nil {
			createdItem.CreatedAt = *createdToken.CreatedAt
		}
		if createdToken.LastUsedAt != nil {
			lastUsedAt := *createdToken.LastUsedAt
			createdItem.LastUsedAt = &lastUsedAt
		}
		return nil
	}); err != nil {
		return nil, rpcError(500, "failed to create token")
	}
	return map[string]interface{}{
		"token": createdItem,
	}, nil
}

type authTokensDeleteParameters struct {
	TokenID string `json:"tokenId"`
}

func (self *webSocketConnection) handleAuthTokensDelete(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParams[authTokensDeleteParameters](frame)
	if err != nil {
		return nil, err
	}
	tokenId := parameters.TokenID
	if tokenId == "" {
		return nil, rpcError(400, "tokenId is required")
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		token, err := transaction.GetToken(ctx, tokenId, nil)
		if err != nil {
			return err
		}
		if token.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		return transaction.DeleteToken(ctx, tokenId, nil)
	}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, rpcError(404, "token not found")
		}
		return nil, rpcError(500, "failed to delete token")
	}
	return map[string]interface{}{
		"deleted": true,
		"tokenId": tokenId,
	}, nil
}

// --- Auth Password RPC handler ---

// authChangePasswordParameters are the parameters for auth.changePassword.
type authChangePasswordParameters struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// handleAuthChangePassword changes the login password given the current password.
func (self *webSocketConnection) handleAuthChangePassword(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParams[authChangePasswordParameters](frame)
	if err != nil {
		return nil, err
	}
	if len(parameters.NewPassword) < 8 {
		return nil, rpcError(400, "new password must be at least 8 characters")
	}
	hash, err := security.HashPassword(parameters.NewPassword)
	if err != nil {
		return nil, rpcError(500, "failed to hash password")
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		user, err := transaction.GetUser(ctx, self.userId(), nil)
		if err != nil {
			return err
		}
		existingPassword := user.GetPassword()
		if existingPassword != "" {
			if parameters.CurrentPassword == "" {
				return web.Error(400, "current password is required")
			}
			match, verifyError := security.VerifyPassword([]byte(existingPassword), parameters.CurrentPassword)
			if verifyError != nil || !match {
				return web.Error(401, "current password is incorrect")
			}
		}
		_, err = transaction.ModifyUser(ctx, self.userId(), func(user *models.User) error {
			user.Password = ptrto.TrimmedString(string(hash))
			return nil
		}, nil)
		return err
	}); err != nil {
		if typedError, ok := err.(*web.HTTPError); ok {
			return nil, rpcError(typedError.StatusCode, typedError.Error())
		}
		if errors.Is(err, store.ErrNotFound) {
			return nil, rpcError(404, "user not found")
		}
		return nil, rpcError(500, "failed to save password")
	}
	return map[string]interface{}{
		"ok": true,
	}, nil
}
