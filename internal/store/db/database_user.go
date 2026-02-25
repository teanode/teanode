package db

import (
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

type databaseUserRecord struct {
	ID             string    `gorm:"column:id;type:varchar(32);primaryKey"`
	Username       *string   `gorm:"column:username;type:varchar(128)"`
	Password       *string   `gorm:"column:password;type:varchar(128)"`
	Admin          *bool     `gorm:"column:admin"`
	DefaultAgentID *string   `gorm:"column:default_agent_id;type:varchar(32)"`
	TelegramChatID *int64    `gorm:"column:telegram_chat_id"`
	DiscordUserID  *string   `gorm:"column:discord_user_id;type:varchar(128)"`
	AvatarMediaID  *string   `gorm:"column:avatar_media_id;type:varchar(32)"`
	Description    *string   `gorm:"column:description"`
	CreatedAt      time.Time `gorm:"column:created_at;not null"`
	ModifiedAt     time.Time `gorm:"column:modified_at;not null"`
}

func (databaseUserRecord) TableName() string {
	return "users"
}

func (self *databaseTransaction) ListUsers(options *store.Option) ([]models.User, error) {
	records := make([]databaseUserRecord, 0)
	query := applyOption(self.database.Model(&databaseUserRecord{}).Order("id ASC"), options)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	users := make([]models.User, 0, len(records))
	for _, record := range records {
		user := userRecordToModel(&record)
		users = append(users, *user)
	}
	return users, nil
}

func (self *databaseTransaction) CreateUser(user *models.User, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.User, error) {
	if user == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToUserRecord(user)
	if strings.TrimSpace(record.ID) == "" {
		record.ID = security.NewULID()
	}
	record.CreatedAt = valueOrTime(user.CreatedAt)
	record.ModifiedAt = valueOrTime(user.ModifiedAt)
	createError := self.database.Create(record).Error
	if createError != nil {
		return nil, databaseError(createError)
	}
	if seedError := self.createSeedWorkspaceFiles(models.ScopeUser, record.ID, seedWorkspaceFiles, options); seedError != nil {
		return nil, seedError
	}
	return self.GetUser(record.ID, options)
}

func (self *databaseTransaction) GetUser(userId string, options *store.Option) (*models.User, error) {
	record := &databaseUserRecord{}
	getError := self.database.Where("id = ?", strings.TrimSpace(userId)).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return userRecordToModel(record), nil
}

func (self *databaseTransaction) GetUserByUsername(username string, options *store.Option) (string, *models.User, bool) {
	record := &databaseUserRecord{}
	getError := self.database.Where("username = ?", strings.TrimSpace(username)).Take(record).Error
	if getError != nil {
		return "", nil, false
	}
	user := userRecordToModel(record)
	return record.ID, user, true
}

func (self *databaseTransaction) GetUserByTelegramChatID(telegramChatId int64, options *store.Option) (string, *models.User, bool) {
	record := &databaseUserRecord{}
	getError := self.database.Where("telegram_chat_id = ?", telegramChatId).Take(record).Error
	if getError != nil {
		return "", nil, false
	}
	user := userRecordToModel(record)
	return record.ID, user, true
}

func (self *databaseTransaction) GetUserByDiscordUserID(discordUserId string, options *store.Option) (string, *models.User, bool) {
	record := &databaseUserRecord{}
	getError := self.database.Where("discord_user_id = ?", strings.TrimSpace(discordUserId)).Take(record).Error
	if getError != nil {
		return "", nil, false
	}
	user := userRecordToModel(record)
	return record.ID, user, true
}

func (self *databaseTransaction) ModifyUser(userId string, modifier func(*models.User) error, options *store.Option) (*models.User, error) {
	user, getError := self.GetUser(userId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(user); modifierError != nil {
		return nil, modifierError
	}
	record := modelToUserRecord(user)
	record.ID = strings.TrimSpace(userId)
	record.ModifiedAt = time.Now().UTC()
	if user.CreatedAt != nil {
		record.CreatedAt = user.CreatedAt.UTC()
	}
	updateError := self.database.Model(&databaseUserRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"username":         record.Username,
		"password":         record.Password,
		"admin":            record.Admin,
		"default_agent_id": record.DefaultAgentID,
		"telegram_chat_id": record.TelegramChatID,
		"discord_user_id":  record.DiscordUserID,
		"avatar_media_id":  record.AvatarMediaID,
		"description":      record.Description,
		"modified_at":      record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetUser(record.ID, options)
}

func (self *databaseTransaction) DeleteUser(userId string, options *store.Option) error {
	trimmedUserId := strings.TrimSpace(userId)
	deleteWorkspaceError := self.database.Where("scope = ? AND scope_id = ?", string(models.ScopeUser), trimmedUserId).Delete(&databaseWorkspaceFileRecord{}).Error
	if deleteWorkspaceError != nil {
		return databaseError(deleteWorkspaceError)
	}
	result := self.database.Where("id = ?", trimmedUserId).Delete(&databaseUserRecord{})
	if result.Error != nil {
		return databaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func modelToUserRecord(user *models.User) *databaseUserRecord {
	return &databaseUserRecord{
		ID:             strings.TrimSpace(user.ID),
		Username:       ptrto.TrimmedString(valueOrEmptyString(user.Username)),
		Password:       ptrto.TrimmedString(valueOrEmptyString(user.Password)),
		Admin:          user.Admin,
		DefaultAgentID: ptrto.TrimmedString(valueOrEmptyString(user.DefaultAgentID)),
		TelegramChatID: user.TelegramChatID,
		DiscordUserID:  ptrto.TrimmedString(valueOrEmptyString(user.DiscordUserID)),
		AvatarMediaID:  ptrto.TrimmedString(valueOrEmptyString(user.AvatarMediaID)),
		Description:    ptrto.TrimmedString(valueOrEmptyString(user.Description)),
	}
}

func userRecordToModel(record *databaseUserRecord) *models.User {
	return &models.User{
		ID:             record.ID,
		Username:       ptrto.TrimmedString(valueOrEmptyString(record.Username)),
		Password:       ptrto.TrimmedString(valueOrEmptyString(record.Password)),
		Admin:          record.Admin,
		DefaultAgentID: ptrto.TrimmedString(valueOrEmptyString(record.DefaultAgentID)),
		TelegramChatID: record.TelegramChatID,
		DiscordUserID:  ptrto.TrimmedString(valueOrEmptyString(record.DiscordUserID)),
		AvatarMediaID:  ptrto.TrimmedString(valueOrEmptyString(record.AvatarMediaID)),
		Description:    ptrto.TrimmedString(valueOrEmptyString(record.Description)),
		CreatedAt:      &record.CreatedAt,
		ModifiedAt:     &record.ModifiedAt,
	}
}
