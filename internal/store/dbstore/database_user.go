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

type databaseUserRecord struct {
	ID             string     `gorm:"column:id;type:varchar(32);primaryKey"`
	Username       *string    `gorm:"column:username;type:varchar(128)"`
	Name           *string    `gorm:"column:name;type:varchar(256)"`
	Password       *string    `gorm:"column:password;type:varchar(128)"`
	Admin          *bool      `gorm:"column:admin"`
	DefaultAgentID *string    `gorm:"column:default_agent_id;type:varchar(32)"`
	TelegramChatID *int64     `gorm:"column:telegram_chat_id"`
	DiscordUserID  *string    `gorm:"column:discord_user_id;type:varchar(128)"`
	AvatarMediaID  *string    `gorm:"column:avatar_media_id;type:varchar(32)"`
	Description    *string    `gorm:"column:description"`
	SummarizedAt   *time.Time `gorm:"column:summarized_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null"`
	ModifiedAt     time.Time  `gorm:"column:modified_at;not null"`
}

func (self databaseUserRecord) TableName() string {
	return "users"
}

func (self *databaseTransaction) ListUsers(ctx context.Context, options *store.Option) ([]*models.User, error) {
	records := make([]databaseUserRecord, 0)
	query := applyOption(self.database.Model(&databaseUserRecord{}).Order("id ASC"), options)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	users := make([]*models.User, 0, len(records))
	for _, record := range records {
		user := userRecordToModel(&record)
		users = append(users, user)
	}
	return users, nil
}

func (self *databaseTransaction) CreateUser(ctx context.Context, user *models.User, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.User, error) {
	if user == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToUserRecord(user)
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
	if seedError := self.createSeedWorkspaceFiles(ctx, models.ScopeUser, record.ID, seedWorkspaceFiles, options); seedError != nil {
		return nil, seedError
	}
	return self.GetUser(ctx, record.ID, options)
}

func (self *databaseTransaction) GetUser(ctx context.Context, userId string, options *store.Option) (*models.User, error) {
	record := &databaseUserRecord{}
	getError := self.database.Where("id = ?", userId).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return userRecordToModel(record), nil
}

func (self *databaseTransaction) GetUserByUsername(ctx context.Context, username string, options *store.Option) (*models.User, error) {
	record := &databaseUserRecord{}
	getError := self.database.Where("username = ?", username).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return userRecordToModel(record), nil
}

func (self *databaseTransaction) GetUserByTelegramChatID(ctx context.Context, telegramChatId int64, options *store.Option) (*models.User, error) {
	record := &databaseUserRecord{}
	getError := self.database.Where("telegram_chat_id = ?", telegramChatId).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return userRecordToModel(record), nil
}

func (self *databaseTransaction) GetUserByDiscordUserID(ctx context.Context, discordUserId string, options *store.Option) (*models.User, error) {
	record := &databaseUserRecord{}
	getError := self.database.Where("discord_user_id = ?", discordUserId).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return userRecordToModel(record), nil
}

func (self *databaseTransaction) ModifyUser(ctx context.Context, userId string, modifier func(*models.User) error, options *store.Option) (*models.User, error) {
	user, getError := self.GetUser(ctx, userId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(user); modifierError != nil {
		return nil, modifierError
	}
	record := modelToUserRecord(user)
	record.ID = userId
	record.ModifiedAt = *ptrto.TimeNowInLocal()
	updateError := self.database.Model(&databaseUserRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"username":         record.Username,
		"name":             record.Name,
		"password":         record.Password,
		"admin":            record.Admin,
		"default_agent_id": record.DefaultAgentID,
		"telegram_chat_id": record.TelegramChatID,
		"discord_user_id":  record.DiscordUserID,
		"avatar_media_id":  record.AvatarMediaID,
		"description":      record.Description,
		"summarized_at":    record.SummarizedAt,
		"modified_at":      record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetUser(ctx, record.ID, options)
}

func (self *databaseTransaction) DeleteUser(ctx context.Context, userId string, options *store.Option) error {
	deleteWorkspaceFileError := self.database.Where("scope = ? AND scope_id = ?", string(models.ScopeUser), userId).Delete(&databaseWorkspaceFileRecord{}).Error
	if deleteWorkspaceFileError != nil {
		return databaseError(deleteWorkspaceFileError)
	}
	result := self.database.Where("id = ?", userId).Delete(&databaseUserRecord{})
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
		ID:             user.ID,
		Username:       ptrto.TrimmedString(user.GetUsername()),
		Name:           ptrto.TrimmedString(user.GetName()),
		Password:       ptrto.TrimmedString(user.GetPassword()),
		Admin:          user.Admin,
		DefaultAgentID: ptrto.TrimmedString(user.GetDefaultAgentID()),
		TelegramChatID: user.TelegramChatID,
		DiscordUserID:  ptrto.TrimmedString(user.GetDiscordUserID()),
		AvatarMediaID:  ptrto.TrimmedString(user.GetAvatarMediaID()),
		Description:    ptrto.TrimmedString(user.GetDescription()),
		SummarizedAt:   user.SummarizedAt,
	}
}

func userRecordToModel(record *databaseUserRecord) *models.User {
	return &models.User{
		ID:             record.ID,
		Username:       ptrto.TrimmedString(valueor.Zero(record.Username)),
		Name:           ptrto.TrimmedString(valueor.Zero(record.Name)),
		Password:       ptrto.TrimmedString(valueor.Zero(record.Password)),
		Admin:          record.Admin,
		DefaultAgentID: ptrto.TrimmedString(valueor.Zero(record.DefaultAgentID)),
		TelegramChatID: record.TelegramChatID,
		DiscordUserID:  ptrto.TrimmedString(valueor.Zero(record.DiscordUserID)),
		AvatarMediaID:  ptrto.TrimmedString(valueor.Zero(record.AvatarMediaID)),
		Description:    ptrto.TrimmedString(valueor.Zero(record.Description)),
		SummarizedAt:   record.SummarizedAt,
		CreatedAt:      &record.CreatedAt,
		ModifiedAt:     &record.ModifiedAt,
	}
}
