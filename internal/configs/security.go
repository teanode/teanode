package configs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/util/atomicfile"
	"gopkg.in/yaml.v3"
)

// SecurityConfig holds auth data in ~/.teanode/security.yaml.
type SecurityConfig struct {
	mutex        sync.RWMutex            `json:"-" yaml:"-"`
	Users        map[string]SecurityUser `json:"users,omitempty" yaml:"users,omitempty"`
	ChannelLinks ChannelLinks            `json:"channelLinks,omitempty" yaml:"channelLinks,omitempty"`
}

type SecurityUser struct {
	Username     string          `json:"username,omitempty" yaml:"username,omitempty"`
	Admin        bool            `json:"admin,omitempty" yaml:"admin,omitempty"`
	PasswordHash string          `json:"passwordHash,omitempty" yaml:"passwordHash,omitempty"`
	Tokens       []SecurityToken `json:"tokens,omitempty" yaml:"tokens,omitempty"`
}

type SecurityToken struct {
	ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
	Token      string     `json:"token,omitempty" yaml:"token,omitempty"`
	CreatedAt  time.Time  `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty" yaml:"lastUsedAt,omitempty"`
}

type ChannelLinks struct {
	Telegram map[string]string `json:"telegram,omitempty" yaml:"telegram,omitempty"` // chatId -> userId
	Discord  map[string]string `json:"discord,omitempty" yaml:"discord,omitempty"`   // platform userId -> userId
}

func (self *SecurityConfig) Lock() {
	if self == nil {
		return
	}
	self.mutex.Lock()
}

func (self *SecurityConfig) Unlock() {
	if self == nil {
		return
	}
	self.mutex.Unlock()
}

func (self *SecurityConfig) RLock() {
	if self == nil {
		return
	}
	self.mutex.RLock()
}

func (self *SecurityConfig) RUnlock() {
	if self == nil {
		return
	}
	self.mutex.RUnlock()
}

// SecurityFile returns the path to ~/.teanode/security.yaml.
func SecurityFile() (string, error) {
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "security.yaml"), nil
}

// LoadSecurity reads and unmarshals security.yaml. Returns an empty config if
// the file does not exist.
func LoadSecurity() (*SecurityConfig, error) {
	securityFile, err := SecurityFile()
	if err != nil {
		return nil, err
	}

	config := &SecurityConfig{}
	data, err := os.ReadFile(securityFile)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return nil, fmt.Errorf("reading security config: %w", err)
	}
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("parsing security config: %w", err)
	}
	if config.Users == nil {
		config.Users = map[string]SecurityUser{}
	}
	if config.ChannelLinks.Telegram == nil {
		config.ChannelLinks.Telegram = map[string]string{}
	}
	if config.ChannelLinks.Discord == nil {
		config.ChannelLinks.Discord = map[string]string{}
	}
	normalizeSecurityUsernames(config.Users)
	return config, nil
}

// SaveSecurity writes the security config to ~/.teanode/security.yaml atomically.
func SaveSecurity(config *SecurityConfig) error {
	if config == nil {
		config = &SecurityConfig{}
	}
	if config.Users == nil {
		config.Users = map[string]SecurityUser{}
	}
	if config.ChannelLinks.Telegram == nil {
		config.ChannelLinks.Telegram = map[string]string{}
	}
	if config.ChannelLinks.Discord == nil {
		config.ChannelLinks.Discord = map[string]string{}
	}
	normalizeSecurityUsernames(config.Users)
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshalling security config: %w", err)
	}
	securityFile, err := SecurityFile()
	if err != nil {
		return err
	}
	return atomicfile.WriteFileWithMode(securityFile, data, 0600)
}

func (self *SecurityConfig) FindUserByUsername(username string) (string, SecurityUser, bool) {
	if self == nil {
		return "", SecurityUser{}, false
	}
	needle := strings.ToLower(strings.TrimSpace(username))
	if needle == "" {
		return "", SecurityUser{}, false
	}
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	for userId, user := range self.Users {
		if strings.ToLower(strings.TrimSpace(user.Username)) == needle {
			return userId, user, true
		}
	}
	return "", SecurityUser{}, false
}

func (self *SecurityConfig) FindUserByToken(token string) (string, SecurityUser, int, bool) {
	if self == nil {
		return "", SecurityUser{}, -1, false
	}
	if strings.TrimSpace(token) == "" {
		return "", SecurityUser{}, -1, false
	}
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	for userId, user := range self.Users {
		for index, tokenEntry := range user.Tokens {
			if tokenEntry.Token == token {
				return userId, user, index, true
			}
		}
	}
	return "", SecurityUser{}, -1, false
}

// LatestToken returns the newest available token from users in stable user-id order.
func (self *SecurityConfig) LatestToken() string {
	if self == nil || len(self.Users) == 0 {
		return ""
	}
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	userIds := make([]string, 0, len(self.Users))
	for userId := range self.Users {
		userIds = append(userIds, userId)
	}
	sort.Strings(userIds)
	for _, userId := range userIds {
		user := self.Users[userId]
		if len(user.Tokens) == 0 {
			continue
		}
		token := strings.TrimSpace(user.Tokens[len(user.Tokens)-1].Token)
		if token != "" {
			return token
		}
	}
	return ""
}

func (self *SecurityConfig) HasPasswordConfigured() bool {
	if self == nil {
		return false
	}
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	for _, user := range self.Users {
		if strings.TrimSpace(user.PasswordHash) != "" {
			return true
		}
	}
	return false
}

func (self *SecurityConfig) IsAdmin(userId string) bool {
	if self == nil {
		return false
	}
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	user, ok := self.Users[userId]
	if !ok {
		return false
	}
	return user.Admin
}

func normalizeSecurityUsernames(users map[string]SecurityUser) {
	if len(users) == 0 {
		return
	}

	used := map[string]struct{}{}
	for _, user := range users {
		username := strings.ToLower(strings.TrimSpace(user.Username))
		if username != "" {
			used[username] = struct{}{}
		}
	}

	userIds := make([]string, 0, len(users))
	for userId := range users {
		userIds = append(userIds, userId)
	}
	sort.Strings(userIds)

	makeDefaultUsername := func(index int) string {
		if index == 1 {
			return "user"
		}
		return fmt.Sprintf("user-%d", index)
	}

	next := 1
	for _, userId := range userIds {
		user := users[userId]
		if strings.TrimSpace(user.Username) != "" {
			continue
		}
		for {
			candidate := makeDefaultUsername(next)
			next++
			lower := strings.ToLower(candidate)
			if _, exists := used[lower]; exists {
				continue
			}
			user.Username = candidate
			users[userId] = user
			used[lower] = struct{}{}
			break
		}
	}
}
