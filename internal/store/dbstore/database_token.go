package dbstore

import (
	"context"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/valueor"
)

type databaseTokenRecord struct {
	ID            string     `gorm:"column:id;type:varchar(32);primaryKey"`
	UserID        *string    `gorm:"column:user_id;type:varchar(32)"`
	Token         *string    `gorm:"column:token;type:varchar(128)"`
	LastUsedAt    *time.Time `gorm:"column:last_used_at"`
	RemoteAddress *string    `gorm:"column:remote_address;type:varchar(128)"`
	UserAgent     *string    `gorm:"column:user_agent;type:varchar(256)"`
	CreatedAt     time.Time  `gorm:"column:created_at;not null"`
	ModifiedAt    time.Time  `gorm:"column:modified_at;not null"`
}

func (databaseTokenRecord) TableName() string {
	return "tokens"
}

func (self *databaseTransaction) ListTokens(ctx context.Context, userId string, options *store.Option) ([]*models.Token, error) {
	query := self.database.Model(&databaseTokenRecord{})
	if userId != "" {
		query = query.Where("user_id = ?", userId)
	}
	query = applyOption(query.Order("id ASC"), options)
	records := make([]databaseTokenRecord, 0)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	tokens := make([]*models.Token, 0, len(records))
	for _, record := range records {
		tokens = append(tokens, tokenRecordToModel(&record))
	}
	return tokens, nil
}

func (self *databaseTransaction) CreateToken(ctx context.Context, token *models.Token, options *store.Option) (*models.Token, error) {
	if token == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToTokenRecord(token)
	if record.ID == "" {
		record.ID = security.NewULID()
	}
	now := ptrto.TimeNowInLocal()
	record.CreatedAt = *now
	record.ModifiedAt = *now
	createError := self.database.Create(record).Error
	if createError != nil {
		return nil, databaseError(createError)
	}
	return self.GetToken(ctx, record.ID, options)
}

func (self *databaseTransaction) GetToken(ctx context.Context, tokenId string, options *store.Option) (*models.Token, error) {
	record := &databaseTokenRecord{}
	getError := self.database.Where("id = ?", tokenId).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return tokenRecordToModel(record), nil
}

func (self *databaseTransaction) GetTokenByToken(ctx context.Context, tokenValue string, options *store.Option) (*models.Token, error) {
	record := &databaseTokenRecord{}
	getError := self.database.Where("token = ?", tokenValue).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	token := tokenRecordToModel(record)
	return token, nil
}

func (self *databaseTransaction) ModifyToken(ctx context.Context, tokenId string, modifier func(*models.Token) error, options *store.Option) (*models.Token, error) {
	token, getError := self.GetToken(ctx, tokenId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(token); modifierError != nil {
		return nil, modifierError
	}
	record := modelToTokenRecord(token)
	record.ID = tokenId
	record.ModifiedAt = *ptrto.TimeNowInLocal()
	updateError := self.database.Model(&databaseTokenRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"user_id":        record.UserID,
		"token":          record.Token,
		"last_used_at":   record.LastUsedAt,
		"remote_address": record.RemoteAddress,
		"user_agent":     record.UserAgent,
		"modified_at":    record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetToken(ctx, record.ID, options)
}

func (self *databaseTransaction) DeleteToken(ctx context.Context, tokenId string, options *store.Option) error {
	result := self.database.Where("id = ?", tokenId).Delete(&databaseTokenRecord{})
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
		ID:            token.ID,
		UserID:        ptrto.TrimmedString(token.GetUserID()),
		Token:         ptrto.TrimmedString(token.GetToken()),
		LastUsedAt:    lastUsedAt,
		RemoteAddress: ptrto.TrimmedString(token.GetRemoteAddress()),
		UserAgent:     ptrto.TrimmedString(token.GetUserAgent()),
	}
}

func tokenRecordToModel(record *databaseTokenRecord) *models.Token {
	return &models.Token{
		ID:            record.ID,
		UserID:        ptrto.TrimmedString(valueor.Zero(record.UserID)),
		Token:         ptrto.TrimmedString(valueor.Zero(record.Token)),
		LastUsedAt:    record.LastUsedAt,
		RemoteAddress: ptrto.TrimmedString(valueor.Zero(record.RemoteAddress)),
		UserAgent:     ptrto.TrimmedString(valueor.Zero(record.UserAgent)),
		CreatedAt:     &record.CreatedAt,
		ModifiedAt:    &record.ModifiedAt,
	}
}
