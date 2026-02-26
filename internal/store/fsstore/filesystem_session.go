package fsstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
	"gopkg.in/yaml.v3"
)

type fileSystemSessionRecord struct {
	ID         string    `yaml:"id"`
	UserID     string    `yaml:"userId"`
	CreatedAt  time.Time `yaml:"createdAt"`
	ExpiresAt  time.Time `yaml:"expiresAt"`
	UserAgent  string    `yaml:"userAgent"`
	RemoteAddr string    `yaml:"remoteAddr"`
	LastSeenAt time.Time `yaml:"lastSeenAt"`
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
	entries, readError := os.ReadDir(self.sessionsDirectory())
	if os.IsNotExist(readError) {
		return []*models.Session{}, nil
	}
	if readError != nil {
		return nil, readError
	}
	now := time.Now()
	results := make([]*models.Session, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		sessionId := strings.TrimSuffix(entry.Name(), ".yaml")
		record, loadError := self.readSessionRecord(sessionId)
		if loadError != nil {
			continue
		}
		if now.After(record.ExpiresAt) {
			_ = self.moveSessionToTrash(sessionId)
			continue
		}
		session := sessionRecordToModel(record)
		results = append(results, &session)
	}
	return applyOffsetLimit(results, options), nil
}

func (self *fileSystemTransaction) createSession(session *models.Session, options *store.Option) (*models.Session, error) {
	if session == nil || session.UserID == nil || *session.UserID == "" {
		return nil, store.ErrInvalidOptions
	}
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
		ID:         sessionId,
		UserID:     *session.UserID,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
		UserAgent:  session.GetUserAgent(),
		RemoteAddr: session.GetRemoteAddress(),
		LastSeenAt: now,
	}
	if writeError := self.writeSessionRecord(record); writeError != nil {
		return nil, writeError
	}
	result := sessionRecordToModel(record)
	return &result, nil
}

func (self *fileSystemTransaction) getSession(sessionId string, options *store.Option) (*models.Session, error) {
	record, readError := self.readSessionRecord(sessionId)
	if readError != nil {
		if os.IsNotExist(readError) {
			return nil, store.ErrNotFound
		}
		return nil, readError
	}
	if time.Now().After(record.ExpiresAt) {
		_ = self.moveSessionToTrash(sessionId)
		return nil, store.ErrNotFound
	}
	result := sessionRecordToModel(record)
	return &result, nil
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
	if writeError := self.writeSessionRecord(record); writeError != nil {
		return nil, writeError
	}
	result := sessionRecordToModel(record)
	return &result, nil
}

func (self *fileSystemTransaction) deleteSession(sessionId string, options *store.Option) error {
	sessionPath := filepath.Join(self.sessionsDirectory(), sessionId+".yaml")
	if _, statError := os.Stat(sessionPath); os.IsNotExist(statError) {
		return store.ErrNotFound
	}
	return self.moveSessionToTrash(sessionId)
}

func (self *fileSystemTransaction) readSessionRecord(sessionId string) (fileSystemSessionRecord, error) {
	data, readError := os.ReadFile(filepath.Join(self.sessionsDirectory(), sessionId+".yaml"))
	if readError != nil {
		return fileSystemSessionRecord{}, readError
	}
	record := fileSystemSessionRecord{}
	if unmarshalError := yaml.Unmarshal(data, &record); unmarshalError != nil {
		return fileSystemSessionRecord{}, unmarshalError
	}
	return record, nil
}

func (self *fileSystemTransaction) writeSessionRecord(record fileSystemSessionRecord) error {
	if record.ID == "" {
		return fmt.Errorf("session ID is required")
	}
	if makeDirectoryError := os.MkdirAll(self.sessionsDirectory(), 0755); makeDirectoryError != nil {
		return makeDirectoryError
	}
	encoded, marshalError := yaml.Marshal(record)
	if marshalError != nil {
		return marshalError
	}
	return atomicfile.WriteFile(filepath.Join(self.sessionsDirectory(), record.ID+".yaml"), encoded)
}

func (self *fileSystemTransaction) moveSessionToTrash(sessionId string) error {
	sessionPath := filepath.Join(self.sessionsDirectory(), sessionId+".yaml")
	return trash.Move(sessionPath, self.trashDirectory())
}

func sessionRecordToModel(record fileSystemSessionRecord) models.Session {
	createdAt := record.CreatedAt
	expiresAt := record.ExpiresAt
	modifiedAt := record.LastSeenAt
	return models.Session{
		ID:            record.ID,
		UserID:        ptrto.TrimmedString(record.UserID),
		UserAgent:     ptrto.TrimmedString(record.UserAgent),
		RemoteAddress: ptrto.TrimmedString(record.RemoteAddr),
		CreatedAt:     &createdAt,
		ExpiresAt:     &expiresAt,
		ModifiedAt:    &modifiedAt,
	}
}

func modelToSessionRecord(session models.Session) fileSystemSessionRecord {
	record := fileSystemSessionRecord{
		ID:         session.ID,
		UserID:     session.GetUserID(),
		UserAgent:  session.GetUserAgent(),
		RemoteAddr: session.GetRemoteAddress(),
	}
	if session.CreatedAt != nil {
		record.CreatedAt = *session.CreatedAt
	}
	if session.ExpiresAt != nil {
		record.ExpiresAt = *session.ExpiresAt
	}
	if session.ModifiedAt != nil {
		record.LastSeenAt = *session.ModifiedAt
	}
	return record
}
