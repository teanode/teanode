package v1api

import (
	"context"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func (self *webSocketConnection) profileToRpcPayload(profile *models.User) map[string]interface{} {
	payload := map[string]interface{}{
		"name": profile.GetName(),
	}
	if description := profile.GetDescription(); description != "" {
		payload["description"] = description
	}
	if avatarMediaId := profile.GetAvatarMediaID(); avatarMediaId != "" {
		payload["avatarMediaId"] = avatarMediaId
	}
	return payload
}

func (self *webSocketConnection) handleProfileGet(frame requestFrame) (interface{}, error) {
	var profile *models.User
	err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		user, getError := transaction.GetUser(ctx, self.userId(), nil)
		if getError != nil {
			return getError
		}
		profile = user
		return nil
	})
	if err != nil {
		return nil, rpcError(500, "failed to load profile")
	}
	return self.profileToRpcPayload(profile), nil
}

type profileUpdateParameters struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	AvatarMediaID *string `json:"avatarMediaId,omitempty"`
}

func (self *webSocketConnection) handleProfileUpdate(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParams[profileUpdateParameters](frame)
	if err != nil {
		return nil, err
	}

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyUser(ctx, self.userId(), func(user *models.User) error {
			if parameters.Name != nil {
				user.Name = ptrto.TrimmedString(*parameters.Name)
			}
			if parameters.Description != nil {
				user.Description = ptrto.TrimmedString(*parameters.Description)
			}
			if parameters.AvatarMediaID != nil {
				avatarMediaId := *parameters.AvatarMediaID
				user.AvatarMediaID = &avatarMediaId
			}
			return nil
		}, nil)
		return err
	}); err != nil {
		return nil, rpcError(500, "failed to save profile")
	}
	var persisted *models.User
	err = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		user, getError := transaction.GetUser(ctx, self.userId(), nil)
		if getError != nil {
			return getError
		}
		persisted = user
		return nil
	})
	if err != nil {
		return nil, rpcError(500, "failed to load profile")
	}

	return self.profileToRpcPayload(persisted), nil
}

func (self *webSocketConnection) handleProfileAvatarRemove(frame requestFrame) (interface{}, error) {
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyUser(ctx, self.userId(), func(user *models.User) error {
			emptyValue := ""
			user.AvatarMediaID = &emptyValue
			return nil
		}, nil)
		return err
	}); err != nil {
		return nil, rpcError(500, "failed to save profile")
	}
	var persisted *models.User
	err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		user, getError := transaction.GetUser(ctx, self.userId(), nil)
		if getError != nil {
			return getError
		}
		persisted = user
		return nil
	})
	if err != nil {
		return nil, rpcError(500, "failed to load profile")
	}
	return self.profileToRpcPayload(persisted), nil
}
