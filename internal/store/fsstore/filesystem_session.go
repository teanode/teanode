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

type fileSystemSessionRecord struct {
	ID            string    `yaml:"id"`
	UserID        string    `yaml:"userId"`
	CreatedAt     time.Time `yaml:"createdAt"`
	ExpiresAt     time.Time `yaml:"expiresAt"`
	UserAgent     string    `yaml:"userAgent"`
	RemoteAddress string    `yaml:"remoteAddress"`
	LastSeenAt    time.Time `yaml:"lastSeenAt"`
}

func (self *fileSystemTransaction) ListSessions(ctx context.Context, options *store.Option) ([]*models.Session, error) {
	return self.listSessions(options)
}

func (self *fileSystemTransaction) CreateSession(ctx context.Context, session *models.Session, options *store.Option) (*models.Session, error) {
	return self.createSession(session, options)
}

func (self *fileSystemTransaction) GetSession(ctx context.Context, sessionId string, options *store.Option) (*models.Session, error) {
	return self.getSession(sessionId, options)
}

func (self *fileSystemTransaction) ModifySession(ctx context.Context, sessionId string, modifier func(*models.Session) error, options *store.Option) (*models.Session, error) {
	return self.modifySession(ctx, sessionId, modifier, options)
}

func (self *fileSystemTransaction) DeleteSession(ctx context.Context, sessionId string, options *store.Option) error {
	return self.deleteSession(sessionId, options)
}

func (self *fileSystemTransaction) listSessions(options *store.Option) ([]*models.Session, error) {
	userRecords, err := self.listUserRecords()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	results := make([]*models.Session, 0)
	for _, userRecord := range userRecords {
		entries, readError := os.ReadDir(self.userSessionsDirectory(userRecord.ID))
		if readError != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			sessionId := strings.TrimSuffix(entry.Name(), ".yaml")
			record, loadError := self.readSessionRecord(userRecord.ID, sessionId)
			if loadError != nil {
				continue
			}
			if now.After(record.ExpiresAt) {
				_ = self.moveSessionToTrash(userRecord.ID, sessionId)
				continue
			}
			session := sessionRecordToModel(record)
			results = append(results, &session)
		}
	}
	return applyOffsetLimit(results, options), nil
}

func (self *fileSystemTransaction) createSession(session *models.Session, options *store.Option) (*models.Session, error) {
	if session == nil || session.UserID == nil || *session.UserID == "" {
		return nil, store.ErrInvalidOptions
	}
	userId := *session.UserID
	sessionId := session.ID
	if sessionId == "" {
		sessionId = security.NewULID()
	}
	now := time.Now()
	expiresAt := now.Add(14 * 24 * time.Hour)
	if session.ExpiresAt != nil {
		expiresAt = *session.ExpiresAt
	}
	record := fileSystemSessionRecord{
		ID:            sessionId,
		UserID:        userId,
		CreatedAt:     now,
		ExpiresAt:     expiresAt,
		UserAgent:     session.GetUserAgent(),
		RemoteAddress: session.GetRemoteAddress(),
		LastSeenAt:    now,
	}
	if writeError := self.writeSessionRecord(userId, record); writeError != nil {
		return nil, writeError
	}
	result := sessionRecordToModel(record)
	return &result, nil
}

func (self *fileSystemTransaction) getSession(sessionId string, options *store.Option) (*models.Session, error) {
	userRecords, err := self.listUserRecords()
	if err != nil {
		return nil, err
	}
	for _, userRecord := range userRecords {
		record, readError := self.readSessionRecord(userRecord.ID, sessionId)
		if readError != nil {
			continue
		}
		if time.Now().After(record.ExpiresAt) {
			_ = self.moveSessionToTrash(userRecord.ID, sessionId)
			return nil, store.ErrNotFound
		}
		result := sessionRecordToModel(record)
		return &result, nil
	}
	return nil, store.ErrNotFound
}

func (self *fileSystemTransaction) modifySession(ctx context.Context, sessionId string, modifier func(*models.Session) error, options *store.Option) (*models.Session, error) {
	session, getError := self.GetSession(ctx, sessionId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(session); modifierError != nil {
		return nil, modifierError
	}
	record := modelToSessionRecord(*session)
	record.ID = sessionId
	if record.CreatedAt.IsZero() {
		record.CreatedAt = *ptrto.TimeNowInLocal()
	}
	record.LastSeenAt = *ptrto.TimeNowInLocal()
	userId := record.UserID
	if writeError := self.writeSessionRecord(userId, record); writeError != nil {
		return nil, writeError
	}
	result := sessionRecordToModel(record)
	return &result, nil
}

func (self *fileSystemTransaction) deleteSession(sessionId string, options *store.Option) error {
	userRecords, err := self.listUserRecords()
	if err != nil {
		return err
	}
	for _, userRecord := range userRecords {
		sessionPath := self.userSessionFilename(userRecord.ID, sessionId)
		if _, statError := os.Stat(sessionPath); statError == nil {
			return self.moveSessionToTrash(userRecord.ID, sessionId)
		}
	}
	return store.ErrNotFound
}

func (self *fileSystemTransaction) readSessionRecord(userId, sessionId string) (fileSystemSessionRecord, error) {
	data, readError := os.ReadFile(self.userSessionFilename(userId, sessionId))
	if readError != nil {
		return fileSystemSessionRecord{}, readError
	}
	record := fileSystemSessionRecord{}
	if unmarshalError := yaml.Unmarshal(data, &record); unmarshalError != nil {
		return fileSystemSessionRecord{}, unmarshalError
	}
	return record, nil
}

func (self *fileSystemTransaction) writeSessionRecord(userId string, record fileSystemSessionRecord) error {
	if record.ID == "" {
		return fmt.Errorf("session ID is required")
	}
	directory := self.userSessionsDirectory(userId)
	if makeDirectoryError := os.MkdirAll(directory, 0755); makeDirectoryError != nil {
		return makeDirectoryError
	}
	return writeYAMLFile(self.userSessionFilename(userId, record.ID), record)
}

func (self *fileSystemTransaction) moveSessionToTrash(userId, sessionId string) error {
	sessionPath := self.userSessionFilename(userId, sessionId)
	return trash.Move(sessionPath, self.trashDirectory())
}

func sessionRecordToModel(record fileSystemSessionRecord) models.Session {
	createdAt := record.CreatedAt
	expiresAt := record.ExpiresAt
	lastSeenAt := record.LastSeenAt
	modifiedAt := record.LastSeenAt
	return models.Session{
		ID:            record.ID,
		UserID:        ptrto.TrimmedString(record.UserID),
		UserAgent:     ptrto.TrimmedString(record.UserAgent),
		RemoteAddress: ptrto.TrimmedString(record.RemoteAddress),
		CreatedAt:     &createdAt,
		ExpiresAt:     &expiresAt,
		LastSeenAt:    &lastSeenAt,
		ModifiedAt:    &modifiedAt,
	}
}

func modelToSessionRecord(session models.Session) fileSystemSessionRecord {
	record := fileSystemSessionRecord{
		ID:            session.ID,
		UserID:        session.GetUserID(),
		UserAgent:     session.GetUserAgent(),
		RemoteAddress: session.GetRemoteAddress(),
	}
	if session.CreatedAt != nil {
		record.CreatedAt = *session.CreatedAt
	}
	if session.ExpiresAt != nil {
		record.ExpiresAt = *session.ExpiresAt
	}
	if session.LastSeenAt != nil {
		record.LastSeenAt = *session.LastSeenAt
	} else if session.ModifiedAt != nil {
		record.LastSeenAt = *session.ModifiedAt
	}
	return record
}
