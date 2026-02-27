package dbstore

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm/clause"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

type databaseConfigurationRecord struct {
	ID         string    `gorm:"column:id;type:varchar(32);primaryKey"`
	Data       []byte    `gorm:"column:data;type:jsonb;not null"`
	CreatedAt  time.Time `gorm:"column:created_at;not null"`
	ModifiedAt time.Time `gorm:"column:modified_at;not null"`
}

func (databaseConfigurationRecord) TableName() string {
	return "configurations"
}

func (self *databaseTransaction) GetConfiguration(ctx context.Context, options *store.Option) (*models.Configuration, error) {
	record := &databaseConfigurationRecord{}
	getError := self.database.Order("id DESC").Limit(1).Take(record).Error
	if getError != nil {
		if databaseError(getError) == store.ErrNotFound {
			return &models.Configuration{}, nil
		}
		return nil, databaseError(getError)
	}
	configuration := &models.Configuration{}
	if unmarshalError := json.Unmarshal(record.Data, configuration); unmarshalError != nil {
		return nil, unmarshalError
	}
	configuration.CreatedAt = &record.CreatedAt
	configuration.ModifiedAt = &record.ModifiedAt
	return configuration, nil
}

func (self *databaseTransaction) ModifyConfiguration(ctx context.Context, modifier func(*models.Configuration) error, options *store.Option) (*models.Configuration, error) {
	configuration, getError := self.GetConfiguration(ctx, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(configuration); modifierError != nil {
		return nil, modifierError
	}
	now := *ptrto.TimeNowInLocal()
	createdAt := now
	if configuration.CreatedAt != nil {
		createdAt = *configuration.CreatedAt
	}
	configuration.CreatedAt = &createdAt
	configuration.ModifiedAt = &now
	data, marshalError := json.Marshal(configuration)
	if marshalError != nil {
		return nil, marshalError
	}
	configurationId := security.NewULID()
	existingRecord := &databaseConfigurationRecord{}
	getExistingError := self.database.Order("id DESC").Limit(1).Take(existingRecord).Error
	if getExistingError == nil {
		configurationId = existingRecord.ID
	} else if databaseError(getExistingError) != store.ErrNotFound {
		return nil, databaseError(getExistingError)
	}
	record := &databaseConfigurationRecord{
		ID:         configurationId,
		Data:       data,
		CreatedAt:  createdAt,
		ModifiedAt: now,
	}
	saveError := self.database.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"data", "modified_at"}),
	}).Create(record).Error
	if saveError != nil {
		return nil, databaseError(saveError)
	}
	return configuration, nil
}
