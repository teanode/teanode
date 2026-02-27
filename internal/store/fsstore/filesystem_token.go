package fsstore

import (
	"context"
	"fmt"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

func (self *fileSystemTransaction) ListTokens(ctx context.Context, userId string, options *store.Option) ([]*models.Token, error) {
	return self.listTokens(userId, options)
}

func (self *fileSystemTransaction) CreateToken(ctx context.Context, token *models.Token, options *store.Option) (*models.Token, error) {
	return self.createToken(token, options)
}

func (self *fileSystemTransaction) GetToken(ctx context.Context, tokenId string, options *store.Option) (*models.Token, error) {
	return self.getToken(tokenId, options)
}

func (self *fileSystemTransaction) GetTokenByToken(ctx context.Context, tokenValue string, options *store.Option) (*models.Token, error) {
	return self.getTokenByToken(tokenValue, options)
}

func (self *fileSystemTransaction) ModifyToken(ctx context.Context, tokenId string, modifier func(*models.Token) error, options *store.Option) (*models.Token, error) {
	return self.modifyToken(ctx, tokenId, modifier, options)
}

func (self *fileSystemTransaction) DeleteToken(ctx context.Context, tokenId string, options *store.Option) error {
	return self.deleteToken(tokenId, options)
}
func (self *fileSystemTransaction) listTokens(userId string, options *store.Option) ([]*models.Token, error) {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	user, ok := securityConfiguration.Users[userId]
	if !ok {
		return []*models.Token{}, nil
	}
	tokens := make([]*models.Token, 0, len(user.Tokens))
	for _, token := range user.Tokens {
		tokenModel := securityTokenToModel(userId, token)
		tokens = append(tokens, &tokenModel)
	}
	tokens = applyOffsetLimit(tokens, options)
	return tokens, nil
}

func (self *fileSystemTransaction) createToken(token *models.Token, options *store.Option) (*models.Token, error) {
	if token == nil || token.UserID == nil {
		return nil, fmt.Errorf("token userId is required")
	}
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	user, ok := securityConfiguration.Users[*token.UserID]
	if !ok {
		return nil, store.ErrNotFound
	}
	tokenId := token.ID
	if tokenId == "" {
		tokenId = security.NewULID()
	}
	tokenValue := token.GetToken()
	if tokenValue == "" {
		tokenValue = security.GenerateRandomString(48, security.LowerAlphaNumeric)
	}
	createdAt := time.Now()
	securityToken := storeSecurityTokenRecord{
		ID:         tokenId,
		Token:      tokenValue,
		CreatedAt:  createdAt,
		LastUsedAt: token.LastUsedAt,
	}
	user.Tokens = append(user.Tokens, securityToken)
	securityConfiguration.Users[*token.UserID] = user
	if err := self.saveSecurityRecord(securityConfiguration); err != nil {
		return nil, err
	}
	result := securityTokenToModel(*token.UserID, securityToken)
	return &result, nil
}

func (self *fileSystemTransaction) getToken(tokenId string, options *store.Option) (*models.Token, error) {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	for userId, user := range securityConfiguration.Users {
		for _, token := range user.Tokens {
			if token.ID == tokenId {
				result := securityTokenToModel(userId, token)
				return &result, nil
			}
		}
	}
	return nil, store.ErrNotFound
}

func (self *fileSystemTransaction) getTokenByToken(tokenValue string, options *store.Option) (*models.Token, error) {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	userId, user, index, found := securityConfiguration.FindUserByToken(tokenValue)
	if !found || index < 0 || index >= len(user.Tokens) {
		return nil, store.ErrNotFound
	}
	result := securityTokenToModel(userId, user.Tokens[index])
	return &result, nil
}

func (self *fileSystemTransaction) modifyToken(ctx context.Context, tokenId string, modifier func(*models.Token) error, options *store.Option) (*models.Token, error) {
	token, err := self.GetToken(ctx, tokenId, options)
	if err != nil {
		return nil, err
	}
	if err := modifier(token); err != nil {
		return nil, err
	}
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	for userId, user := range securityConfiguration.Users {
		for index, existingToken := range user.Tokens {
			if existingToken.ID != tokenId {
				continue
			}
			existingToken.Token = token.GetToken()
			existingToken.LastUsedAt = token.LastUsedAt
			user.Tokens[index] = existingToken
			securityConfiguration.Users[userId] = user
			if err := self.saveSecurityRecord(securityConfiguration); err != nil {
				return nil, err
			}
			result := securityTokenToModel(userId, existingToken)
			return &result, nil
		}
	}
	return nil, store.ErrNotFound
}

func (self *fileSystemTransaction) deleteToken(tokenId string, options *store.Option) error {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return err
	}
	for userId, user := range securityConfiguration.Users {
		filteredTokens := make([]storeSecurityTokenRecord, 0, len(user.Tokens))
		removed := false
		for _, token := range user.Tokens {
			if token.ID == tokenId {
				removed = true
				continue
			}
			filteredTokens = append(filteredTokens, token)
		}
		if removed {
			user.Tokens = filteredTokens
			securityConfiguration.Users[userId] = user
			return self.saveSecurityRecord(securityConfiguration)
		}
	}
	return store.ErrNotFound
}

func securityTokenToModel(userId string, token storeSecurityTokenRecord) models.Token {
	modifiedAt := token.CreatedAt
	if token.LastUsedAt != nil {
		modifiedAt = *token.LastUsedAt
	}
	return models.Token{
		ID:         token.ID,
		UserID:     &userId,
		Token:      ptrto.TrimmedString(token.Token),
		CreatedAt:  &token.CreatedAt,
		LastUsedAt: token.LastUsedAt,
		ModifiedAt: &modifiedAt,
	}
}
