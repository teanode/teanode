package fs

import (
	"fmt"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

func (self *transaction) ListTokens(userId string, options *store.Option) ([]models.Token, error) {
	return self.listTokens(userId, options)
}

func (self *transaction) CreateToken(token *models.Token, options *store.Option) (*models.Token, error) {
	return self.createToken(token, options)
}

func (self *transaction) GetToken(tokenId string, options *store.Option) (*models.Token, error) {
	return self.getToken(tokenId, options)
}

func (self *transaction) GetTokenByToken(tokenValue string, options *store.Option) (string, *models.Token, bool) {
	return self.getTokenByToken(tokenValue, options)
}

func (self *transaction) ModifyToken(tokenId string, modifier func(*models.Token) error, options *store.Option) (*models.Token, error) {
	return self.modifyToken(tokenId, modifier, options)
}

func (self *transaction) DeleteToken(tokenId string, options *store.Option) error {
	return self.deleteToken(tokenId, options)
}
func (self *transaction) listTokens(userId string, options *store.Option) ([]models.Token, error) {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	user, ok := securityConfiguration.Users[userId]
	if !ok {
		return []models.Token{}, nil
	}
	tokens := make([]models.Token, 0, len(user.Tokens))
	for _, token := range user.Tokens {
		tokens = append(tokens, securityTokenToModel(userId, token))
	}
	tokens = applyOffsetLimitTokens(tokens, options)
	return tokens, nil
}

func (self *transaction) createToken(token *models.Token, options *store.Option) (*models.Token, error) {
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
	tokenId := strings.TrimSpace(token.ID)
	if tokenId == "" {
		tokenId = security.NewULID()
	}
	tokenValue := strings.TrimSpace(valueOrEmpty(token.Token))
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

func (self *transaction) getToken(tokenId string, options *store.Option) (*models.Token, error) {
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

func (self *transaction) getTokenByToken(tokenValue string, options *store.Option) (string, *models.Token, bool) {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return "", nil, false
	}
	userId, user, index, found := securityConfiguration.FindUserByToken(tokenValue)
	if !found || index < 0 || index >= len(user.Tokens) {
		return "", nil, false
	}
	result := securityTokenToModel(userId, user.Tokens[index])
	return userId, &result, true
}

func (self *transaction) modifyToken(tokenId string, modifier func(*models.Token) error, options *store.Option) (*models.Token, error) {
	token, err := self.GetToken(tokenId, options)
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
			existingToken.Token = valueOrEmpty(token.Token)
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

func (self *transaction) deleteToken(tokenId string, options *store.Option) error {
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
		ID:         strings.TrimSpace(token.ID),
		UserID:     &userId,
		Token:      ptrto.TrimmedString(token.Token),
		CreatedAt:  &token.CreatedAt,
		LastUsedAt: token.LastUsedAt,
		ModifiedAt: &modifiedAt,
	}
}

func applyOffsetLimitTokens(values []models.Token, options *store.Option) []models.Token {
	if options == nil {
		return values
	}
	offset := int(uint64Value(options.Offset))
	if offset >= len(values) {
		return []models.Token{}
	}
	values = values[offset:]
	limit := int(uint64Value(options.Limit))
	if limit > 0 && limit < len(values) {
		values = values[:limit]
	}
	return values
}
