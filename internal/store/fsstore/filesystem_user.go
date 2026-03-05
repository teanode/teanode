package fsstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/timeutil"
	"github.com/teanode/teanode/internal/util/trash"
)

func (self *fileSystemTransaction) ListUsers(ctx context.Context, options *store.Option) ([]*models.User, error) {
	return self.listUsers(ctx, options)
}

func (self *fileSystemTransaction) CreateUser(ctx context.Context, user *models.User, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.User, error) {
	return self.createUser(ctx, user, seedWorkspaceFiles, options)
}

func (self *fileSystemTransaction) GetUser(ctx context.Context, userId string, options *store.Option) (*models.User, error) {
	return self.getUser(userId, options)
}

func (self *fileSystemTransaction) GetUserByUsername(ctx context.Context, username string, options *store.Option) (*models.User, error) {
	return self.getUserByUsername(ctx, username, options)
}

func (self *fileSystemTransaction) GetUserByTelegramChatID(ctx context.Context, telegramChatId int64, options *store.Option) (*models.User, error) {
	return self.getUserByTelegramChatId(ctx, telegramChatId, options)
}

func (self *fileSystemTransaction) GetUserByDiscordUserID(ctx context.Context, discordUserId string, options *store.Option) (*models.User, error) {
	return self.getUserByDiscordUserId(ctx, discordUserId, options)
}

func (self *fileSystemTransaction) ModifyUser(ctx context.Context, userId string, modifier func(*models.User) error, options *store.Option) (*models.User, error) {
	return self.modifyUser(ctx, userId, modifier, options)
}

func (self *fileSystemTransaction) DeleteUser(ctx context.Context, userId string, options *store.Option) error {
	return self.deleteUser(userId, options)
}

func (self *fileSystemTransaction) listUsers(ctx context.Context, options *store.Option) ([]*models.User, error) {
	records, err := self.listUserRecords()
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(left, right int) bool {
		return records[left].ID < records[right].ID
	})
	users := make([]*models.User, 0, len(records))
	for _, record := range records {
		users = append(users, userRecordToModel(&record))
	}
	return applyOffsetLimit(users, options), nil
}

func (self *fileSystemTransaction) createUser(ctx context.Context, user *models.User, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.User, error) {
	if user == nil {
		return nil, fmt.Errorf("user is required")
	}
	userId := user.ID
	if userId == "" {
		userId = security.NewULID()
	}

	// Check if user directory already exists.
	if _, err := os.Stat(self.userConfigurationFilename(userId)); err == nil {
		return nil, store.ErrAlreadyExists
	}

	// Check username uniqueness.
	if user.GetUsername() != "" {
		records, err := self.listUserRecords()
		if err != nil {
			return nil, err
		}
		needle := strings.ToLower(user.GetUsername())
		for _, existing := range records {
			if strings.ToLower(existing.Username) == needle {
				return nil, store.ErrAlreadyExists
			}
		}
	}

	profileName := user.GetName()
	if profileName == "" {
		profileName = user.GetUsername()
	}
	if profileName == "" {
		profileName = processUsername()
	}

	record := &storeUserRecord{
		ID:             userId,
		Name:           profileName,
		Username:       user.GetUsername(),
		PasswordHash:   user.GetPassword(),
		Admin:          user.GetAdmin(),
		DefaultAgentID: user.GetDefaultAgentID(),
		TelegramChatID: user.TelegramChatID,
		DiscordUserID:  user.GetDiscordUserID(),
		Description:    user.GetDescription(),
		AvatarMediaID:  user.GetAvatarMediaID(),
	}

	// Auto-assign username if empty.
	if record.Username == "" {
		allRecords, err := self.listUserRecords()
		if err != nil {
			return nil, err
		}
		normalizeUsername(allRecords, record)
	}

	if err := self.saveUserRecord(userId, record); err != nil {
		return nil, err
	}

	for _, file := range seedWorkspaceFiles {
		copyFile := file
		scope := models.ScopeUser
		copyFile.Scope = &scope
		copyFile.ScopeID = &userId
		if _, err := self.CreateWorkspaceFile(ctx, &copyFile, options); err != nil {
			return nil, err
		}
	}

	return self.getUser(userId, options)
}

func (self *fileSystemTransaction) getUser(userId string, options *store.Option) (*models.User, error) {
	filename := self.userConfigurationFilename(userId)
	if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
		return nil, store.ErrNotFound
	}
	record, err := self.loadUserRecord(userId)
	if err != nil {
		return nil, err
	}
	return userRecordToModel(record), nil
}

func (self *fileSystemTransaction) getUserByUsername(ctx context.Context, username string, options *store.Option) (*models.User, error) {
	needle := strings.ToLower(username)
	if needle == "" {
		return nil, store.ErrNotFound
	}
	records, err := self.listUserRecords()
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if strings.ToLower(record.Username) == needle {
			return userRecordToModel(&record), nil
		}
	}
	return nil, store.ErrNotFound
}

func (self *fileSystemTransaction) getUserByTelegramChatId(ctx context.Context, telegramChatId int64, options *store.Option) (*models.User, error) {
	records, err := self.listUserRecords()
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.TelegramChatID != nil && *record.TelegramChatID == telegramChatId {
			return userRecordToModel(&record), nil
		}
	}
	return nil, store.ErrNotFound
}

func (self *fileSystemTransaction) getUserByDiscordUserId(ctx context.Context, discordUserId string, options *store.Option) (*models.User, error) {
	records, err := self.listUserRecords()
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.DiscordUserID == discordUserId {
			return userRecordToModel(&record), nil
		}
	}
	return nil, store.ErrNotFound
}

func (self *fileSystemTransaction) modifyUser(ctx context.Context, userId string, modifier func(*models.User) error, options *store.Option) (*models.User, error) {
	user, err := self.getUser(userId, options)
	if err != nil {
		return nil, err
	}
	if err := modifier(user); err != nil {
		return nil, err
	}

	record, err := self.loadUserRecord(userId)
	if err != nil {
		return nil, err
	}

	if user.Username != nil {
		record.Username = *user.Username
	}
	if user.Password != nil {
		record.PasswordHash = *user.Password
	}
	if user.Admin != nil {
		record.Admin = *user.Admin
	}
	if user.DefaultAgentID != nil {
		record.DefaultAgentID = *user.DefaultAgentID
	}
	if user.Name != nil {
		record.Name = *user.Name
	}
	if user.Description != nil {
		record.Description = *user.Description
	}
	if user.SummarizedAt != nil {
		record.SummarizedAt = timeutil.Timestamp{Time: *user.SummarizedAt}
	}
	if user.AvatarMediaID != nil {
		record.AvatarMediaID = *user.AvatarMediaID
	}
	if user.TelegramChatID != nil {
		record.TelegramChatID = user.TelegramChatID
	}
	if user.DiscordUserID != nil {
		record.DiscordUserID = *user.DiscordUserID
	}

	if err := self.saveUserRecord(userId, record); err != nil {
		return nil, err
	}
	return self.getUser(userId, options)
}

func (self *fileSystemTransaction) deleteUser(userId string, options *store.Option) error {
	filename := self.userConfigurationFilename(userId)
	if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
		return store.ErrNotFound
	}
	userDirectory := self.userDirectory(userId)
	return trash.Move(userDirectory, self.trashDirectory())
}

func userRecordToModel(record *storeUserRecord) *models.User {
	result := &models.User{
		ID:             record.ID,
		Username:       ptrto.TrimmedString(record.Username),
		Name:           ptrto.TrimmedString(record.Name),
		Password:       ptrto.TrimmedString(record.PasswordHash),
		Admin:          ptrto.Value(record.Admin),
		DefaultAgentID: ptrto.TrimmedString(record.DefaultAgentID),
		AvatarMediaID:  ptrto.TrimmedString(record.AvatarMediaID),
		Description:    ptrto.TrimmedString(record.Description),
		TelegramChatID: record.TelegramChatID,
		DiscordUserID:  ptrto.TrimmedString(record.DiscordUserID),
	}
	if !record.SummarizedAt.Time.IsZero() {
		result.SummarizedAt = &record.SummarizedAt.Time
	}
	return result
}
