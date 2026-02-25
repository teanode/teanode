package db

import (
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

type databaseTokenRecord struct {
	ID         string     `gorm:"column:id;type:varchar(32);primaryKey"`
	UserID     *string    `gorm:"column:user_id;type:varchar(32)"`
	Token      *string    `gorm:"column:token;type:varchar(128)"`
	LastUsedAt *time.Time `gorm:"column:last_used_at"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null"`
	ModifiedAt time.Time  `gorm:"column:modified_at;not null"`
}

func (databaseTokenRecord) TableName() string {
	return "tokens"
}

func (self *databaseTransaction) ListTokens(userId string, options *store.Option) ([]models.Token, error) {
	query := self.database.Model(&databaseTokenRecord{})
	if strings.TrimSpace(userId) != "" {
		query = query.Where("user_id = ?", strings.TrimSpace(userId))
	}
	query = applyOption(query.Order("id ASC"), options)
	records := make([]databaseTokenRecord, 0)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	tokens := make([]models.Token, 0, len(records))
	for _, record := range records {
		tokens = append(tokens, *tokenRecordToModel(&record))
	}
	return tokens, nil
}

func (self *databaseTransaction) CreateToken(token *models.Token, options *store.Option) (*models.Token, error) {
	if token == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToTokenRecord(token)
	if strings.TrimSpace(record.ID) == "" {
		record.ID = security.NewULID()
	}
	record.CreatedAt = valueOrTime(token.CreatedAt)
	record.ModifiedAt = valueOrTime(token.ModifiedAt)
	createError := self.database.Create(record).Error
	if createError != nil {
		return nil, databaseError(createError)
	}
	return self.GetToken(record.ID, options)
}

func (self *databaseTransaction) GetToken(tokenId string, options *store.Option) (*models.Token, error) {
	record := &databaseTokenRecord{}
	getError := self.database.Where("id = ?", strings.TrimSpace(tokenId)).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return tokenRecordToModel(record), nil
}

func (self *databaseTransaction) GetTokenByToken(tokenValue string, options *store.Option) (string, *models.Token, bool) {
	record := &databaseTokenRecord{}
	getError := self.database.Where("token = ?", strings.TrimSpace(tokenValue)).Take(record).Error
	if getError != nil {
		return "", nil, false
	}
	token := tokenRecordToModel(record)
	return record.ID, token, true
}

func (self *databaseTransaction) ModifyToken(tokenId string, modifier func(*models.Token) error, options *store.Option) (*models.Token, error) {
	token, getError := self.GetToken(tokenId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(token); modifierError != nil {
		return nil, modifierError
	}
	record := modelToTokenRecord(token)
	record.ID = strings.TrimSpace(tokenId)
	record.ModifiedAt = time.Now().UTC()
	if token.CreatedAt != nil {
		record.CreatedAt = token.CreatedAt.UTC()
	}
	updateError := self.database.Model(&databaseTokenRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"user_id":      record.UserID,
		"token":        record.Token,
		"last_used_at": record.LastUsedAt,
		"modified_at":  record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetToken(record.ID, options)
}

func (self *databaseTransaction) DeleteToken(tokenId string, options *store.Option) error {
	result := self.database.Where("id = ?", strings.TrimSpace(tokenId)).Delete(&databaseTokenRecord{})
	if result.Error != nil {
		return databaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func modelToTokenRecord(token *models.Token) *databaseTokenRecord {
	var lastUsedAt *time.Time
	if token.LastUsedAt != nil {
		lastUsedAtValue := token.LastUsedAt.UTC()
		lastUsedAt = &lastUsedAtValue
	}
	return &databaseTokenRecord{
		ID:         strings.TrimSpace(token.ID),
		UserID:     ptrto.TrimmedString(valueOrEmptyString(token.UserID)),
		Token:      ptrto.TrimmedString(valueOrEmptyString(token.Token)),
		LastUsedAt: lastUsedAt,
	}
}

func tokenRecordToModel(record *databaseTokenRecord) *models.Token {
	return &models.Token{
		ID:         record.ID,
		UserID:     ptrto.TrimmedString(valueOrEmptyString(record.UserID)),
		Token:      ptrto.TrimmedString(valueOrEmptyString(record.Token)),
		LastUsedAt: record.LastUsedAt,
		CreatedAt:  &record.CreatedAt,
		ModifiedAt: &record.ModifiedAt,
	}
}
