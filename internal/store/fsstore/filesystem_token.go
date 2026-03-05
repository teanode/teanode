package fsstore

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
	"gopkg.in/yaml.v3"
)

type fileSystemTokenRecord struct {
	ID            string     `yaml:"id"`
	UserID        string     `yaml:"userId"`
	Token         string     `yaml:"token"`
	CreatedAt     time.Time  `yaml:"createdAt"`
	LastUsedAt    *time.Time `yaml:"lastUsedAt,omitempty"`
	RemoteAddress string     `yaml:"remoteAddress,omitempty"`
	UserAgent     string     `yaml:"userAgent,omitempty"`
}

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
	entries, readError := os.ReadDir(self.userTokensDirectory(userId))
	if os.IsNotExist(readError) {
		return []*models.Token{}, nil
	}
	if readError != nil {
		return nil, readError
	}
	tokens := make([]*models.Token, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		tokenId := strings.TrimSuffix(entry.Name(), ".yaml")
		record, loadError := self.readTokenRecord(userId, tokenId)
		if loadError != nil {
			continue
		}
		token := tokenRecordToModel(record)
		tokens = append(tokens, &token)
	}
	return applyOffsetLimit(tokens, options), nil
}

func (self *fileSystemTransaction) createToken(token *models.Token, options *store.Option) (*models.Token, error) {
	if token == nil || token.UserID == nil {
		return nil, fmt.Errorf("token userId is required")
	}
	userId := *token.UserID
	tokenId := token.ID
	if tokenId == "" {
		tokenId = security.NewULID()
	}
	tokenValue := token.GetToken()
	if tokenValue == "" {
		tokenValue = security.GenerateRandomString(48, security.LowerAlphaNumeric)
	}
	now := time.Now()
	record := fileSystemTokenRecord{
		ID:            tokenId,
		UserID:        userId,
		Token:         tokenValue,
		CreatedAt:     now,
		LastUsedAt:    token.LastUsedAt,
		RemoteAddress: token.GetRemoteAddress(),
		UserAgent:     token.GetUserAgent(),
	}
	if err := self.writeTokenRecord(userId, record); err != nil {
		return nil, err
	}
	result := tokenRecordToModel(record)
	return &result, nil
}

func (self *fileSystemTransaction) getToken(tokenId string, options *store.Option) (*models.Token, error) {
	userRecords, err := self.listUserRecords()
	if err != nil {
		return nil, err
	}
	for _, userRecord := range userRecords {
		record, readError := self.readTokenRecord(userRecord.ID, tokenId)
		if readError != nil {
			continue
		}
		result := tokenRecordToModel(record)
		return &result, nil
	}
	return nil, store.ErrNotFound
}

func (self *fileSystemTransaction) getTokenByToken(tokenValue string, options *store.Option) (*models.Token, error) {
	userRecords, err := self.listUserRecords()
	if err != nil {
		return nil, err
	}
	for _, userRecord := range userRecords {
		entries, readError := os.ReadDir(self.userTokensDirectory(userRecord.ID))
		if readError != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			tokenId := strings.TrimSuffix(entry.Name(), ".yaml")
			record, loadError := self.readTokenRecord(userRecord.ID, tokenId)
			if loadError != nil {
				continue
			}
			if record.Token == tokenValue {
				result := tokenRecordToModel(record)
				return &result, nil
			}
		}
	}
	return nil, store.ErrNotFound
}

func (self *fileSystemTransaction) modifyToken(ctx context.Context, tokenId string, modifier func(*models.Token) error, options *store.Option) (*models.Token, error) {
	token, err := self.GetToken(ctx, tokenId, options)
	if err != nil {
		return nil, err
	}
	if err := modifier(token); err != nil {
		return nil, err
	}
	userId := token.GetUserID()
	record, err := self.readTokenRecord(userId, tokenId)
	if err != nil {
		return nil, err
	}
	record.Token = token.GetToken()
	record.LastUsedAt = token.LastUsedAt
	record.RemoteAddress = token.GetRemoteAddress()
	record.UserAgent = token.GetUserAgent()
	if err := self.writeTokenRecord(userId, record); err != nil {
		return nil, err
	}
	result := tokenRecordToModel(record)
	return &result, nil
}

func (self *fileSystemTransaction) deleteToken(tokenId string, options *store.Option) error {
	userRecords, err := self.listUserRecords()
	if err != nil {
		return err
	}
	for _, userRecord := range userRecords {
		tokenPath := self.userTokenFilename(userRecord.ID, tokenId)
		if _, statError := os.Stat(tokenPath); statError == nil {
			return self.moveTokenToTrash(userRecord.ID, tokenId)
		}
	}
	return store.ErrNotFound
}

func (self *fileSystemTransaction) readTokenRecord(userId, tokenId string) (fileSystemTokenRecord, error) {
	data, readError := os.ReadFile(self.userTokenFilename(userId, tokenId))
	if readError != nil {
		return fileSystemTokenRecord{}, readError
	}
	record := fileSystemTokenRecord{}
	if unmarshalError := yaml.Unmarshal(data, &record); unmarshalError != nil {
		return fileSystemTokenRecord{}, unmarshalError
	}
	return record, nil
}

func (self *fileSystemTransaction) writeTokenRecord(userId string, record fileSystemTokenRecord) error {
	if record.ID == "" {
		return fmt.Errorf("token ID is required")
	}
	directory := self.userTokensDirectory(userId)
	if makeDirectoryError := os.MkdirAll(directory, 0755); makeDirectoryError != nil {
		return makeDirectoryError
	}
	return writeYAMLFile(self.userTokenFilename(userId, record.ID), record)
}

func (self *fileSystemTransaction) moveTokenToTrash(userId, tokenId string) error {
	tokenPath := self.userTokenFilename(userId, tokenId)
	return trash.Move(tokenPath, self.trashDirectory())
}

func tokenRecordToModel(record fileSystemTokenRecord) models.Token {
	modifiedAt := record.CreatedAt
	if record.LastUsedAt != nil {
		modifiedAt = *record.LastUsedAt
	}
	return models.Token{
		ID:            record.ID,
		UserID:        ptrto.TrimmedString(record.UserID),
		Token:         ptrto.TrimmedString(record.Token),
		CreatedAt:     &record.CreatedAt,
		LastUsedAt:    record.LastUsedAt,
		ModifiedAt:    &modifiedAt,
		RemoteAddress: ptrto.TrimmedString(record.RemoteAddress),
		UserAgent:     ptrto.TrimmedString(record.UserAgent),
	}
}
