package fs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
)

func (self *transaction) ListUsers(options *store.Option) ([]models.User, error) {
	return self.listUsers(options)
}

func (self *transaction) CreateUser(user *models.User, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.User, error) {
	return self.createUser(user, seedWorkspaceFiles, options)
}

func (self *transaction) GetUser(userId string, options *store.Option) (*models.User, error) {
	return self.getUser(userId, options)
}

func (self *transaction) GetUserByUsername(username string, options *store.Option) (string, *models.User, bool) {
	return self.getUserByUsername(username, options)
}

func (self *transaction) GetUserByTelegramChatID(telegramChatId int64, options *store.Option) (string, *models.User, bool) {
	return self.getUserByTelegramChatId(telegramChatId, options)
}

func (self *transaction) GetUserByDiscordUserID(discordUserId string, options *store.Option) (string, *models.User, bool) {
	return self.getUserByDiscordUserId(discordUserId, options)
}

func (self *transaction) ModifyUser(userId string, modifier func(*models.User) error, options *store.Option) (*models.User, error) {
	return self.modifyUser(userId, modifier, options)
}

func (self *transaction) DeleteUser(userId string, options *store.Option) error {
	return self.deleteUser(userId, options)
}
func (self *transaction) listUsers(options *store.Option) ([]models.User, error) {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return nil, err
	}
	userIDs := make([]string, 0, len(securityConfiguration.Users))
	for userId := range securityConfiguration.Users {
		userIDs = append(userIDs, userId)
	}
	sort.Strings(userIDs)
	filteredUserIDs := applyOffsetLimitString(userIDs, options)

	users := make([]models.User, 0, len(filteredUserIDs))
	for _, userId := range filteredUserIDs {
		user, err := self.GetUser(userId, options)
		if err != nil {
			continue
		}
		users = append(users, *user)
	}
	return users, nil
}

func (self *transaction) createUser(user *models.User, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.User, error) {
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
	userId := strings.TrimSpace(user.ID)
	if userId == "" {
		userId = security.NewULID()
	}
	if _, exists := securityConfiguration.Users[userId]; exists {
		return nil, store.ErrAlreadyExists
	}
	if strings.TrimSpace(valueOrEmpty(user.Username)) != "" {
		if _, _, found := securityConfiguration.FindUserByUsername(valueOrEmpty(user.Username)); found {
			return nil, store.ErrAlreadyExists
		}
	}
	securityUser := storeSecurityUserRecord{
		Username:       strings.TrimSpace(valueOrEmpty(user.Username)),
		PasswordHash:   strings.TrimSpace(valueOrEmpty(user.Password)),
		Admin:          boolValue(user.Admin),
		DefaultAgentID: strings.TrimSpace(valueOrEmpty(user.DefaultAgentID)),
	}
	securityConfiguration.Users[userId] = securityUser
	if err := self.saveSecurityRecord(securityConfiguration); err != nil {
		return nil, err
	}
	profile := &storeUserRecord{
		ID:            userId,
		Name:          strings.TrimSpace(valueOrEmpty(user.Username)),
		Description:   strings.TrimSpace(valueOrEmpty(user.Description)),
		AvatarMediaID: strings.TrimSpace(valueOrEmpty(user.AvatarMediaID)),
	}
	if profile.Name == "" {
		profile.Name = processUsername()
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
		if _, err := self.CreateWorkspaceFile(&copyFile, options); err != nil {
			return nil, err
		}
	}
	return self.GetUser(userId, options)
}

func (self *transaction) getUser(userId string, options *store.Option) (*models.User, error) {
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
	result.Username = ptrto.TrimmedString(strings.TrimSpace(securityUser.Username))
	result.Password = ptrto.TrimmedString(strings.TrimSpace(securityUser.PasswordHash))
	result.Admin = ptrto.Value(securityUser.Admin)
	result.DefaultAgentID = ptrto.TrimmedString(strings.TrimSpace(securityUser.DefaultAgentID))
	result.AvatarMediaID = ptrto.TrimmedString(strings.TrimSpace(profile.AvatarMediaID))
	result.Description = ptrto.TrimmedString(strings.TrimSpace(profile.Description))
	result.TelegramChatID = findTelegramChatIdByUserId(securityConfiguration.ChannelLinks.Telegram, userId)
	result.DiscordUserID = findDiscordUserIdByUserId(securityConfiguration.ChannelLinks.Discord, userId)
	return &result, nil
}

func (self *transaction) getUserByUsername(username string, options *store.Option) (string, *models.User, bool) {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return "", nil, false
	}
	userId, _, found := securityConfiguration.FindUserByUsername(username)
	if !found {
		return "", nil, false
	}
	user, err := self.GetUser(userId, options)
	if err != nil {
		return "", nil, false
	}
	return userId, user, true
}

func (self *transaction) getUserByTelegramChatId(telegramChatId int64, options *store.Option) (string, *models.User, bool) {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return "", nil, false
	}
	userId := strings.TrimSpace(securityConfiguration.ChannelLinks.Telegram[fmt.Sprintf("%d", telegramChatId)])
	if userId == "" {
		return "", nil, false
	}
	user, err := self.GetUser(userId, options)
	if err != nil {
		return "", nil, false
	}
	return userId, user, true
}

func (self *transaction) getUserByDiscordUserId(discordUserId string, options *store.Option) (string, *models.User, bool) {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return "", nil, false
	}
	userId := strings.TrimSpace(securityConfiguration.ChannelLinks.Discord[discordUserId])
	if userId == "" {
		return "", nil, false
	}
	user, err := self.GetUser(userId, options)
	if err != nil {
		return "", nil, false
	}
	return userId, user, true
}

func (self *transaction) modifyUser(userId string, modifier func(*models.User) error, options *store.Option) (*models.User, error) {
	user, err := self.GetUser(userId, options)
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
		securityUser.Username = strings.TrimSpace(*user.Username)
	}
	if user.Password != nil {
		securityUser.PasswordHash = strings.TrimSpace(*user.Password)
	}
	if user.Admin != nil {
		securityUser.Admin = *user.Admin
	}
	if user.DefaultAgentID != nil {
		securityUser.DefaultAgentID = strings.TrimSpace(*user.DefaultAgentID)
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
	if user.Username != nil {
		profile.Name = strings.TrimSpace(*user.Username)
	}
	if user.Description != nil {
		profile.Description = strings.TrimSpace(*user.Description)
	}
	if user.AvatarMediaID != nil {
		profile.AvatarMediaID = strings.TrimSpace(*user.AvatarMediaID)
	}
	if err := self.saveUserRecord(userId, profile); err != nil {
		return nil, err
	}
	return self.GetUser(userId, options)
}

func (self *transaction) deleteUser(userId string, options *store.Option) error {
	securityConfiguration, err := self.loadSecurityRecord()
	if err != nil {
		return err
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

func applyOffsetLimitString(values []string, options *store.Option) []string {
	if options == nil {
		return values
	}
	offset := uint64(0)
	if options.Offset != nil {
		offset = *options.Offset
	}
	if offset >= uint64(len(values)) {
		return []string{}
	}
	values = values[offset:]
	if options.Limit != nil && *options.Limit < uint64(len(values)) {
		values = values[:*options.Limit]
	}
	return values
}
