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

type databaseSessionRecord struct {
	ID            string     `gorm:"column:id;type:varchar(32);primaryKey"`
	UserID        *string    `gorm:"column:user_id;type:varchar(32)"`
	UserAgent     *string    `gorm:"column:user_agent;type:varchar(256)"`
	RemoteAddress *string    `gorm:"column:remote_address;type:varchar(128)"`
	ExpiresAt     *time.Time `gorm:"column:expires_at"`
	LastSeenAt    *time.Time `gorm:"column:last_seen_at"`
	CreatedAt     time.Time  `gorm:"column:created_at;not null"`
	ModifiedAt    time.Time  `gorm:"column:modified_at;not null"`
}

func (self databaseSessionRecord) TableName() string {
	return "sessions"
}

func (self *databaseTransaction) ListSessions(ctx context.Context, options *store.Option) ([]*models.Session, error) {
	records := make([]databaseSessionRecord, 0)
	query := applyOption(self.database.Model(&databaseSessionRecord{}).Order("id ASC"), options)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	sessions := make([]*models.Session, 0, len(records))
	for _, record := range records {
		sessions = append(sessions, sessionRecordToModel(&record))
	}
	return sessions, nil
}

func (self *databaseTransaction) CreateSession(ctx context.Context, session *models.Session, options *store.Option) (*models.Session, error) {
	if session == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToSessionRecord(session)
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
	return self.GetSession(ctx, record.ID, options)
}

func (self *databaseTransaction) GetSession(ctx context.Context, sessionId string, options *store.Option) (*models.Session, error) {
	record := &databaseSessionRecord{}
	getError := self.database.Where("id = ?", sessionId).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return sessionRecordToModel(record), nil
}

func (self *databaseTransaction) ModifySession(ctx context.Context, sessionId string, modifier func(*models.Session) error, options *store.Option) (*models.Session, error) {
	session, getError := self.GetSession(ctx, sessionId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(session); modifierError != nil {
		return nil, modifierError
	}
	record := modelToSessionRecord(session)
	record.ID = sessionId
	record.ModifiedAt = *ptrto.TimeNowInLocal()
	updateError := self.database.Model(&databaseSessionRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"user_id":        record.UserID,
		"user_agent":     record.UserAgent,
		"remote_address": record.RemoteAddress,
		"expires_at":     record.ExpiresAt,
		"last_seen_at":   record.LastSeenAt,
		"modified_at":    record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetSession(ctx, record.ID, options)
}

func (self *databaseTransaction) DeleteSession(ctx context.Context, sessionId string, options *store.Option) error {
	result := self.database.Where("id = ?", sessionId).Delete(&databaseSessionRecord{})
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
		ID:            session.ID,
		UserID:        ptrto.TrimmedString(session.GetUserID()),
		UserAgent:     ptrto.TrimmedString(session.GetUserAgent()),
		RemoteAddress: ptrto.TrimmedString(session.GetRemoteAddress()),
		ExpiresAt:     session.ExpiresAt,
		LastSeenAt:    session.LastSeenAt,
	}
}

func sessionRecordToModel(record *databaseSessionRecord) *models.Session {
	return &models.Session{
		ID:            record.ID,
		UserID:        ptrto.TrimmedString(valueor.Zero(record.UserID)),
		UserAgent:     ptrto.TrimmedString(valueor.Zero(record.UserAgent)),
		RemoteAddress: ptrto.TrimmedString(valueor.Zero(record.RemoteAddress)),
		ExpiresAt:     record.ExpiresAt,
		LastSeenAt:    record.LastSeenAt,
		CreatedAt:     &record.CreatedAt,
		ModifiedAt:    &record.ModifiedAt,
	}
}
