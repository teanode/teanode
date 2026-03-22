package api

import (
	"context"
	"errors"
	"sort"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/onboarding"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/web"
)

type usersListItem struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Admin         bool   `json:"admin"`
	HasPassword   bool   `json:"hasPassword"`
	Name          string `json:"name,omitempty"`
	Description   string `json:"description,omitempty"`
	AvatarMediaID string `json:"avatarMediaId,omitempty"`
}

func (self *webSocketConnection) handleUsersList(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	items := make([]usersListItem, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		users, err := transaction.ListUsers(ctx, nil)
		if err != nil {
			return err
		}
		sort.Slice(users, func(leftIndex, rightIndex int) bool {
			return users[leftIndex].ID < users[rightIndex].ID
		})
		for _, user := range users {
			userId := user.ID
			item := usersListItem{
				ID:          userId,
				Username:    user.GetUsername(),
				Admin:       user.Admin != nil && *user.Admin,
				HasPassword: user.GetPassword() != "",
				Name:        user.GetUsername(),
				Description: user.GetDescription(),
			}
			item.AvatarMediaID = user.GetAvatarMediaID()
			items = append(items, item)
		}
		return nil
	}); err != nil {
		return nil, rpcError(500, "listing users: "+err.Error())
	}
	return map[string]interface{}{
		"users": items,
	}, nil
}

type usersCreateParameters struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

func (self *webSocketConnection) handleUsersCreate(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	parameters, err := unmarshalParams[usersCreateParameters](frame)
	if err != nil {
		return nil, err
	}
	username := parameters.Username
	if username == "" {
		return nil, rpcError(400, "username is required")
	}
	if len(parameters.Password) < 8 {
		return nil, rpcError(400, "password must be at least 8 characters")
	}
	hash, err := security.HashPassword(parameters.Password)
	if err != nil {
		return nil, rpcError(500, "failed to hash password")
	}
	var createdUserId string
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		if existingUser, _ := transaction.GetUserByUsername(ctx, username, nil); existingUser != nil {
			return store.ErrAlreadyExists
		}
		createdUser, err := onboarding.CreateUser(ctx, transaction, &models.User{
			ID:          security.NewULID(),
			Username:    &username,
			Password:    ptrto.TrimmedString(string(hash)),
			Admin:       ptrto.Value(false),
			Description: ptrto.TrimmedString(parameters.Description),
		})
		if err != nil {
			return err
		}
		createdUserId = createdUser.ID
		return nil
	}); err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			return nil, rpcError(409, "username already exists")
		}
		return nil, rpcError(500, "failed to create user")
	}
	return map[string]interface{}{
		"user": usersListItem{
			ID:          createdUserId,
			Username:    username,
			Admin:       false,
			HasPassword: true,
			Name:        username,
		},
	}, nil
}

type usersDeleteParameters struct {
	UserID string `json:"userId"`
}

func (self *webSocketConnection) handleUsersDelete(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	parameters, err := unmarshalParams[usersDeleteParameters](frame)
	if err != nil {
		return nil, err
	}
	userId := parameters.UserID
	if userId == "" {
		return nil, rpcError(400, "userId is required")
	}
	if userId == self.userId() {
		return nil, rpcError(400, "cannot delete the current user")
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		sessionList, listError := transaction.ListSessions(ctx, nil)
		if listError == nil {
			for _, session := range sessionList {
				if session.GetUserID() == userId {
					_ = transaction.DeleteSession(ctx, session.ID, nil)
				}
			}
		}
		return transaction.DeleteUser(ctx, userId, nil)
	}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, rpcError(404, "user not found")
		}
		return nil, rpcError(500, "deleting user: "+err.Error())
	}
	return map[string]interface{}{
		"deleted": true,
	}, nil
}

type usersChangePasswordParameters struct {
	UserID      string `json:"userId"`
	NewPassword string `json:"newPassword"`
}

func (self *webSocketConnection) handleUsersChangePassword(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	parameters, err := unmarshalParams[usersChangePasswordParameters](frame)
	if err != nil {
		return nil, err
	}
	userId := parameters.UserID
	if userId == "" {
		return nil, rpcError(400, "userId is required")
	}
	if userId == self.userId() {
		return nil, rpcError(400, "use auth.changePassword for current user")
	}
	if len(parameters.NewPassword) < 8 {
		return nil, rpcError(400, "new password must be at least 8 characters")
	}
	hash, err := security.HashPassword(parameters.NewPassword)
	if err != nil {
		return nil, rpcError(500, "failed to hash password")
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyUser(ctx, userId, func(user *models.User) error {
			user.Password = ptrto.TrimmedString(string(hash))
			return nil
		}, nil)
		return err
	}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, rpcError(404, "user not found")
		}
		return nil, rpcError(500, "failed to update password")
	}
	return map[string]interface{}{
		"ok": true,
	}, nil
}

type usersUpdateParameters struct {
	UserID      string  `json:"userId"`
	Username    *string `json:"username,omitempty"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	NewPassword *string `json:"newPassword,omitempty"`
}

func (self *webSocketConnection) handleUsersUpdate(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	parameters, err := unmarshalParams[usersUpdateParameters](frame)
	if err != nil {
		return nil, err
	}
	userId := parameters.UserID
	if userId == "" {
		return nil, rpcError(400, "userId is required")
	}
	if userId == self.userId() {
		return nil, rpcError(400, "cannot update the current user from users list")
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyUser(ctx, userId, func(user *models.User) error {
			if parameters.Username != nil {
				nextUsername := *parameters.Username
				if nextUsername == "" {
					return web.Error(400, "username is required")
				}
				user.Username = &nextUsername
			}
			if parameters.NewPassword != nil && *parameters.NewPassword != "" {
				if len(*parameters.NewPassword) < 8 {
					return web.Error(400, "new password must be at least 8 characters")
				}
				hash, err := security.HashPassword(*parameters.NewPassword)
				if err != nil {
					return err
				}
				user.Password = ptrto.TrimmedString(string(hash))
			}
			if parameters.Name != nil {
				nextName := *parameters.Name
				if nextName != "" {
					user.Name = &nextName
				}
			}
			if parameters.Description != nil {
				nextDescription := *parameters.Description
				user.Description = &nextDescription
			}
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
		return nil, rpcError(500, "updating user failed")
	}
	return map[string]interface{}{
		"ok": true,
	}, nil
}

type usersSetRoleParameters struct {
	UserID string `json:"userId"`
	Admin  bool   `json:"admin"`
}

func (self *webSocketConnection) handleUsersSetRole(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	parameters, err := unmarshalParams[usersSetRoleParameters](frame)
	if err != nil {
		return nil, err
	}
	userId := parameters.UserID
	if userId == "" {
		return nil, rpcError(400, "userId is required")
	}
	if userId == self.userId() {
		return nil, rpcError(400, "cannot change the current user's role")
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyUser(ctx, userId, func(user *models.User) error {
			user.Admin = &parameters.Admin
			return nil
		}, nil)
		return err
	}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, rpcError(404, "user not found")
		}
		return nil, rpcError(500, "failed to update role")
	}
	return map[string]interface{}{
		"ok":     true,
		"userId": userId,
		"admin":  parameters.Admin,
	}, nil
}
