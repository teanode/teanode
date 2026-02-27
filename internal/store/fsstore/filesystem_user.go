package fsstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

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
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	userIDs := make([]string, 0, len(securityConfiguration.Users))
	for userId := range securityConfiguration.Users {
		userIDs = append(userIDs, userId)
	}
	sort.Strings(userIDs)
	filteredUserIDs := applyOffsetLimit(userIDs, options)

	users := make([]*models.User, 0, len(filteredUserIDs))
	for _, userId := range filteredUserIDs {
		user, err := self.GetUser(ctx, userId, options)
		if err != nil {
			continue
		}
		users = append(users, user)
	}
	return users, nil
}

func (self *fileSystemTransaction) createUser(ctx context.Context, user *models.User, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.User, error) {
	if user == nil {
		return nil, fmt.Errorf("user is required")
	}
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	if securityConfiguration.Users == nil {
		securityConfiguration.Users = map[string]storeSecurityUserRecord{}
	}
	userId := user.ID
	if userId == "" {
		userId = security.NewULID()
	}
	if _, exists := securityConfiguration.Users[userId]; exists {
		return nil, store.ErrAlreadyExists
	}
	if user.GetUsername() != "" {
		if _, _, found := securityConfiguration.FindUserByUsername(user.GetUsername()); found {
			return nil, store.ErrAlreadyExists
		}
	}
	securityUser := storeSecurityUserRecord{
		Username:       user.GetUsername(),
		PasswordHash:   user.GetPassword(),
		Admin:          user.GetAdmin(),
		DefaultAgentID: user.GetDefaultAgentID(),
	}
	securityConfiguration.Users[userId] = securityUser
	if err := self.saveSecurityRecord(securityConfiguration); err != nil {
		return nil, err
	}
	profileName := user.GetName()
	if profileName == "" {
		profileName = user.GetUsername()
	}
	if profileName == "" {
		profileName = processUsername()
	}
	profile := &storeUserRecord{
		ID:            userId,
		Name:          profileName,
		Description:   user.GetDescription(),
		AvatarMediaID: user.GetAvatarMediaID(),
	}
	if err := self.saveUserRecord(userId, profile); err != nil {
		return nil, err
	}
	if securityConfiguration.ChannelLinks.Telegram == nil {
		securityConfiguration.ChannelLinks.Telegram = map[string]string{}
	}
	if securityConfiguration.ChannelLinks.Discord == nil {
		securityConfiguration.ChannelLinks.Discord = map[string]string{}
	}
	if user.TelegramChatID != nil {
		if securityConfiguration.ChannelLinks.Telegram == nil {
			securityConfiguration.ChannelLinks.Telegram = map[string]string{}
		}
		securityConfiguration.ChannelLinks.Telegram[fmt.Sprintf("%d", *user.TelegramChatID)] = userId
	}
	if user.DiscordUserID != nil {
		if securityConfiguration.ChannelLinks.Discord == nil {
			securityConfiguration.ChannelLinks.Discord = map[string]string{}
		}
		securityConfiguration.ChannelLinks.Discord[*user.DiscordUserID] = userId
	}
	if err := self.saveSecurityRecord(securityConfiguration); err != nil {
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
	return self.GetUser(ctx, userId, options)
}

func (self *fileSystemTransaction) getUser(userId string, options *store.Option) (*models.User, error) {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	securityUser, ok := securityConfiguration.Users[userId]
	if !ok {
		return nil, store.ErrNotFound
	}
	profile, err := self.loadUserRecord(userId)
	if err != nil {
		return nil, err
	}
	result := models.User{ID: userId}
	result.Username = ptrto.TrimmedString(securityUser.Username)
	result.Name = ptrto.TrimmedString(profile.Name)
	result.Password = ptrto.TrimmedString(securityUser.PasswordHash)
	result.Admin = ptrto.Value(securityUser.Admin)
	result.DefaultAgentID = ptrto.TrimmedString(securityUser.DefaultAgentID)
	result.AvatarMediaID = ptrto.TrimmedString(profile.AvatarMediaID)
	result.Description = ptrto.TrimmedString(profile.Description)
	if !profile.SummarizedAt.Time.IsZero() {
		result.SummarizedAt = &profile.SummarizedAt.Time
	}
	result.TelegramChatID = findTelegramChatIdByUserId(securityConfiguration.ChannelLinks.Telegram, userId)
	result.DiscordUserID = findDiscordUserIdByUserId(securityConfiguration.ChannelLinks.Discord, userId)
	return &result, nil
}

func (self *fileSystemTransaction) getUserByUsername(ctx context.Context, username string, options *store.Option) (*models.User, error) {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	userId, _, found := securityConfiguration.FindUserByUsername(username)
	if !found {
		return nil, store.ErrNotFound
	}
	user, err := self.GetUser(ctx, userId, options)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (self *fileSystemTransaction) getUserByTelegramChatId(ctx context.Context, telegramChatId int64, options *store.Option) (*models.User, error) {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	userId := securityConfiguration.ChannelLinks.Telegram[fmt.Sprintf("%d", telegramChatId)]
	if userId == "" {
		return nil, store.ErrNotFound
	}
	return self.GetUser(ctx, userId, options)
}

func (self *fileSystemTransaction) getUserByDiscordUserId(ctx context.Context, discordUserId string, options *store.Option) (*models.User, error) {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	userId := securityConfiguration.ChannelLinks.Discord[discordUserId]
	if userId == "" {
		return nil, store.ErrNotFound
	}
	return self.GetUser(ctx, userId, options)
}

func (self *fileSystemTransaction) modifyUser(ctx context.Context, userId string, modifier func(*models.User) error, options *store.Option) (*models.User, error) {
	user, err := self.GetUser(ctx, userId, options)
	if err != nil {
		return nil, err
	}
	if err := modifier(user); err != nil {
		return nil, err
	}
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	securityUser, ok := securityConfiguration.Users[userId]
	if !ok {
		return nil, store.ErrNotFound
	}
	if user.Username != nil {
		securityUser.Username = *user.Username
	}
	if user.Password != nil {
		securityUser.PasswordHash = *user.Password
	}
	if user.Admin != nil {
		securityUser.Admin = *user.Admin
	}
	if user.DefaultAgentID != nil {
		securityUser.DefaultAgentID = *user.DefaultAgentID
	}
	securityConfiguration.Users[userId] = securityUser

	// refresh link maps for this user
	for chatId, linkedUserId := range securityConfiguration.ChannelLinks.Telegram {
		if linkedUserId == userId {
			delete(securityConfiguration.ChannelLinks.Telegram, chatId)
		}
	}
	for discordUserId, linkedUserId := range securityConfiguration.ChannelLinks.Discord {
		if linkedUserId == userId {
			delete(securityConfiguration.ChannelLinks.Discord, discordUserId)
		}
	}
	if user.TelegramChatID != nil {
		securityConfiguration.ChannelLinks.Telegram[fmt.Sprintf("%d", *user.TelegramChatID)] = userId
	}
	if user.DiscordUserID != nil {
		securityConfiguration.ChannelLinks.Discord[*user.DiscordUserID] = userId
	}
	if err := self.saveSecurityRecord(securityConfiguration); err != nil {
		return nil, err
	}
	profile, err := self.loadUserRecord(userId)
	if err != nil {
		profile = &storeUserRecord{ID: userId}
	}
	if user.Name != nil {
		profile.Name = *user.Name
	}
	if user.Description != nil {
		profile.Description = *user.Description
	}
	if user.SummarizedAt != nil {
		profile.SummarizedAt = timeutil.Timestamp{Time: *user.SummarizedAt}
	}
	if user.AvatarMediaID != nil {
		profile.AvatarMediaID = *user.AvatarMediaID
	}
	if err := self.saveUserRecord(userId, profile); err != nil {
		return nil, err
	}
	return self.GetUser(ctx, userId, options)
}

func (self *fileSystemTransaction) deleteUser(userId string, options *store.Option) error {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return err
	}
	if _, exists := securityConfiguration.Users[userId]; !exists {
		return store.ErrNotFound
	}
	delete(securityConfiguration.Users, userId)
	for chatId, linkedUserId := range securityConfiguration.ChannelLinks.Telegram {
		if linkedUserId == userId {
			delete(securityConfiguration.ChannelLinks.Telegram, chatId)
		}
	}
	for discordUserId, linkedUserId := range securityConfiguration.ChannelLinks.Discord {
		if linkedUserId == userId {
			delete(securityConfiguration.ChannelLinks.Discord, discordUserId)
		}
	}
	if err := self.saveSecurityRecord(securityConfiguration); err != nil {
		return err
	}
	userDirectory := self.userDirectory(userId)
	if _, err := os.Stat(userDirectory); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return trash.Move(userDirectory, self.trashDirectory())
}

func findTelegramChatIdByUserId(links map[string]string, userId string) *int64 {
	for key, linkedUserId := range links {
		if linkedUserId != userId {
			continue
		}
		value, err := json.Number(key).Int64()
		if err != nil {
			continue
		}
		return &value
	}
	return nil
}

func findDiscordUserIdByUserId(links map[string]string, userId string) *string {
	for discordUserId, linkedUserId := range links {
		if linkedUserId == userId {
			value := discordUserId
			return &value
		}
	}
	return nil
}
