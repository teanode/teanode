package db

import (
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

type databaseSessionRecord struct {
	ID            string     `gorm:"column:id;type:varchar(32);primaryKey"`
	UserID        *string    `gorm:"column:user_id;type:varchar(32)"`
	UserAgent     *string    `gorm:"column:user_agent;type:varchar(256)"`
	RemoteAddress *string    `gorm:"column:remote_address;type:varchar(128)"`
	ExpiresAt     *time.Time `gorm:"column:expires_at"`
	CreatedAt     time.Time  `gorm:"column:created_at;not null"`
	ModifiedAt    time.Time  `gorm:"column:modified_at;not null"`
}

func (databaseSessionRecord) TableName() string {
	return "sessions"
}

func (self *databaseTransaction) ListSessions(options *store.Option) ([]models.Session, error) {
	records := make([]databaseSessionRecord, 0)
	query := applyOption(self.database.Model(&databaseSessionRecord{}).Order("id ASC"), options)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	sessions := make([]models.Session, 0, len(records))
	for _, record := range records {
		sessions = append(sessions, *sessionRecordToModel(&record))
	}
	return sessions, nil
}

func (self *databaseTransaction) CreateSession(session *models.Session, options *store.Option) (*models.Session, error) {
	if session == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToSessionRecord(session)
	if strings.TrimSpace(record.ID) == "" {
		record.ID = security.NewULID()
	}
	record.CreatedAt = valueOrTime(session.CreatedAt)
	record.ModifiedAt = valueOrTime(session.ModifiedAt)
	createError := self.database.Create(record).Error
	if createError != nil {
		return nil, databaseError(createError)
	}
	return self.GetSession(record.ID, options)
}

func (self *databaseTransaction) GetSession(sessionId string, options *store.Option) (*models.Session, error) {
	record := &databaseSessionRecord{}
	getError := self.database.Where("id = ?", strings.TrimSpace(sessionId)).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return sessionRecordToModel(record), nil
}

func (self *databaseTransaction) ModifySession(sessionId string, modifier func(*models.Session) error, options *store.Option) (*models.Session, error) {
	session, getError := self.GetSession(sessionId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(session); modifierError != nil {
		return nil, modifierError
	}
	record := modelToSessionRecord(session)
	record.ID = strings.TrimSpace(sessionId)
	record.ModifiedAt = time.Now().UTC()
	if session.CreatedAt != nil {
		record.CreatedAt = session.CreatedAt.UTC()
	}
	updateError := self.database.Model(&databaseSessionRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"user_id":        record.UserID,
		"user_agent":     record.UserAgent,
		"remote_address": record.RemoteAddress,
		"expires_at":     record.ExpiresAt,
		"modified_at":    record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetSession(record.ID, options)
}

func (self *databaseTransaction) DeleteSession(sessionId string, options *store.Option) error {
	result := self.database.Where("id = ?", strings.TrimSpace(sessionId)).Delete(&databaseSessionRecord{})
	if result.Error != nil {
		return databaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func modelToSessionRecord(session *models.Session) *databaseSessionRecord {
	return &databaseSessionRecord{
		ID:            strings.TrimSpace(session.ID),
		UserID:        ptrto.TrimmedString(valueOrEmptyString(session.UserID)),
		UserAgent:     ptrto.TrimmedString(valueOrEmptyString(session.UserAgent)),
		RemoteAddress: ptrto.TrimmedString(valueOrEmptyString(session.RemoteAddress)),
		ExpiresAt:     session.ExpiresAt,
	}
}

func sessionRecordToModel(record *databaseSessionRecord) *models.Session {
	return &models.Session{
		ID:            record.ID,
		UserID:        ptrto.TrimmedString(valueOrEmptyString(record.UserID)),
		UserAgent:     ptrto.TrimmedString(valueOrEmptyString(record.UserAgent)),
		RemoteAddress: ptrto.TrimmedString(valueOrEmptyString(record.RemoteAddress)),
		ExpiresAt:     record.ExpiresAt,
		CreatedAt:     &record.CreatedAt,
		ModifiedAt:    &record.ModifiedAt,
	}
}
