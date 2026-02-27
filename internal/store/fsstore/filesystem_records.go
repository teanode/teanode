package fsstore

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/timeutil"
	"gopkg.in/yaml.v3"
)

type storeGatewayRecord struct {
	Port      int    `json:"port,omitempty" yaml:"port,omitempty"`
	Bind      string `json:"bind,omitempty" yaml:"bind,omitempty"`
	PublicURL string `json:"publicUrl,omitempty" yaml:"publicUrl,omitempty"`
}

type storeProviderRecord struct {
	Name    string `json:"name,omitempty" yaml:"name,omitempty"`
	BaseURL string `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
	APIKey  string `json:"apiKey,omitempty" yaml:"apiKey,omitempty"`
}

type storeModelsRecord struct {
	Default         string                `json:"default,omitempty" yaml:"default,omitempty"`
	SummarizerModel string                `json:"summarizerModel,omitempty" yaml:"summarizerModel,omitempty"`
	ContextWindow   int                   `json:"contextWindow,omitempty" yaml:"contextWindow,omitempty"`
	Providers       []storeProviderRecord `json:"providers,omitempty" yaml:"providers,omitempty"`
}

type storeBrowserRecord struct {
	CDPEndpoint string `json:"cdpEndpoint,omitempty" yaml:"cdpEndpoint,omitempty"`
}

type storeIntegrationsRecord struct {
	Browser *storeBrowserRecord `json:"browser,omitempty" yaml:"browser,omitempty"`
}

type storeDiscordRecord struct {
	Token string `json:"token,omitempty" yaml:"token,omitempty"`
}

type storeTelegramRecord struct {
	Token string `json:"token,omitempty" yaml:"token,omitempty"`
}

type storeChannelsRecord struct {
	Discord  *storeDiscordRecord  `json:"discord,omitempty" yaml:"discord,omitempty"`
	Telegram *storeTelegramRecord `json:"telegram,omitempty" yaml:"telegram,omitempty"`
}

type storeSkillRegistryRecord struct {
	ID               string   `json:"id,omitempty" yaml:"id,omitempty"`
	Publisher        string   `json:"publisher,omitempty" yaml:"publisher,omitempty"`
	IndexURL         string   `json:"indexUrl,omitempty" yaml:"indexUrl,omitempty"`
	PublicKeys       []string `json:"publicKeys,omitempty" yaml:"publicKeys,omitempty"`
	IgnoreSignatures bool     `json:"ignoreSignatures,omitempty" yaml:"ignoreSignatures,omitempty"`
	IgnoreUpdates    bool     `json:"ignoreUpdates,omitempty" yaml:"ignoreUpdates,omitempty"`
}

type storeToolsRecord struct {
	BraveAPIKey   string                     `json:"braveApiKey,omitempty" yaml:"braveApiKey,omitempty"`
	Google        *storeGoogleToolRecord     `json:"google,omitempty" yaml:"google,omitempty"`
	GitHub        *storeGitHubToolRecord     `json:"gitHub,omitempty" yaml:"gitHub,omitempty"`
	GitLab        *storeGitLabToolRecord     `json:"gitLab,omitempty" yaml:"gitLab,omitempty"`
	ClaudeCode    *storeClaudeCodeToolRecord `json:"claudeCode,omitempty" yaml:"claudeCode,omitempty"`
	Codex         *storeCodexToolRecord      `json:"codex,omitempty" yaml:"codex,omitempty"`
	HomeAssistant *storeHomeAssistantRecord  `json:"homeAssistant,omitempty" yaml:"homeAssistant,omitempty"`
	UniFiProtect  *storeUniFiProtectRecord   `json:"unifiProtect,omitempty" yaml:"unifiProtect,omitempty"`
}

type storeGoogleToolRecord struct {
	BinaryPath string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	Account    string   `json:"account,omitempty" yaml:"account,omitempty"`
	Services   []string `json:"services,omitempty" yaml:"services,omitempty"`
}

type storeGitHubToolRecord struct {
	BinaryPath string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	Services   []string `json:"services,omitempty" yaml:"services,omitempty"`
}

type storeGitLabToolRecord struct {
	BinaryPath string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	Services   []string `json:"services,omitempty" yaml:"services,omitempty"`
}

type storeClaudeCodeToolRecord struct {
	BinaryPath            string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	AllowedTools          []string `json:"allowedTools,omitempty" yaml:"allowedTools,omitempty"`
	Model                 string   `json:"model,omitempty" yaml:"model,omitempty"`
	MaxTurnTimeoutSeconds int      `json:"maxTurnTimeoutSeconds,omitempty" yaml:"maxTurnTimeoutSeconds,omitempty"`
}

type storeCodexToolRecord struct {
	BinaryPath            string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	AllowedTools          []string `json:"allowedTools,omitempty" yaml:"allowedTools,omitempty"`
	Model                 string   `json:"model,omitempty" yaml:"model,omitempty"`
	ExtraArguments        []string `json:"extraArgs,omitempty" yaml:"extraArgs,omitempty"`
	MaxTurnTimeoutSeconds int      `json:"maxTurnTimeoutSeconds,omitempty" yaml:"maxTurnTimeoutSeconds,omitempty"`
}

type storeHomeAssistantRecord struct {
	BaseURL         string   `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
	Token           string   `json:"token,omitempty" yaml:"token,omitempty"`
	ReadOnly        bool     `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
	AllowedDomains  []string `json:"allowedDomains,omitempty" yaml:"allowedDomains,omitempty"`
	BlockedDomains  []string `json:"blockedDomains,omitempty" yaml:"blockedDomains,omitempty"`
	AllowedEntities []string `json:"allowedEntities,omitempty" yaml:"allowedEntities,omitempty"`
	TimeoutSeconds  int      `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
}

type storeUniFiProtectRecord struct {
	BaseURL               string   `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
	APIKey                string   `json:"apiKey,omitempty" yaml:"apiKey,omitempty"`
	Username              string   `json:"username,omitempty" yaml:"username,omitempty"`
	Password              string   `json:"password,omitempty" yaml:"password,omitempty"`
	VerifyTLS             bool     `json:"verifyTls,omitempty" yaml:"verifyTls,omitempty"`
	ReadOnly              bool     `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
	AllowedCameras        []string `json:"allowedCameras,omitempty" yaml:"allowedCameras,omitempty"`
	AllowDangerousActions []string `json:"allowDangerousActions,omitempty" yaml:"allowDangerousActions,omitempty"`
	TimeoutSeconds        int      `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
}

type storeConfigurationRecord struct {
	Gateway          storeGatewayRecord         `json:"gateway,omitempty" yaml:"gateway,omitempty"`
	Models           storeModelsRecord          `json:"models,omitempty" yaml:"models,omitempty"`
	Tools            storeToolsRecord           `json:"tools,omitempty" yaml:"tools,omitempty"`
	Integrations     storeIntegrationsRecord    `json:"integrations,omitempty" yaml:"integrations,omitempty"`
	Channels         storeChannelsRecord        `json:"channels,omitempty" yaml:"channels,omitempty"`
	Secrets          map[string]string          `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	SkillsRegistries []storeSkillRegistryRecord `json:"skillsRegistries,omitempty" yaml:"skillsRegistries,omitempty"`
}

type storeAgentRecord struct {
	ID            string             `json:"id" yaml:"id"`
	Name          string             `json:"name,omitempty" yaml:"name,omitempty"`
	Model         string             `json:"model,omitempty" yaml:"model,omitempty"`
	Skills        []string           `json:"skills,omitempty" yaml:"skills,omitempty"`
	Tools         []string           `json:"tools,omitempty" yaml:"tools,omitempty"`
	Description   string             `json:"description,omitempty" yaml:"description,omitempty"`
	SummarizedAt  timeutil.Timestamp `json:"summarizedAt,omitempty" yaml:"summarizedAt,omitempty"`
	AvatarMediaID string             `json:"avatarMediaId,omitempty" yaml:"avatarMediaId,omitempty"`
}

type storeUserRecord struct {
	ID            string             `json:"id" yaml:"id"`
	Name          string             `json:"name" yaml:"name"`
	Description   string             `json:"description,omitempty" yaml:"description,omitempty"`
	SummarizedAt  timeutil.Timestamp `json:"summarizedAt,omitempty" yaml:"summarizedAt,omitempty"`
	AvatarMediaID string             `json:"avatarMediaId,omitempty" yaml:"avatarMediaId,omitempty"`
}

type storeProjectRecord struct {
	ID           string             `json:"id" yaml:"id"`
	Name         string             `json:"name" yaml:"name"`
	Description  string             `json:"description" yaml:"description"`
	SummarizedAt timeutil.Timestamp `json:"summarizedAt,omitempty" yaml:"summarizedAt,omitempty"`
	UpdatedAt    timeutil.Timestamp `json:"updatedAt" yaml:"updatedAt"`
}

type storeSecurityTokenRecord struct {
	ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
	Token      string     `json:"token,omitempty" yaml:"token,omitempty"`
	CreatedAt  time.Time  `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty" yaml:"lastUsedAt,omitempty"`
}

type storeSecurityUserRecord struct {
	Username       string                     `json:"username,omitempty" yaml:"username,omitempty"`
	Admin          bool                       `json:"admin,omitempty" yaml:"admin,omitempty"`
	PasswordHash   string                     `json:"passwordHash,omitempty" yaml:"passwordHash,omitempty"`
	DefaultAgentID string                     `json:"defaultAgentId,omitempty" yaml:"defaultAgentId,omitempty"`
	Tokens         []storeSecurityTokenRecord `json:"tokens,omitempty" yaml:"tokens,omitempty"`
}

type storeChannelLinksRecord struct {
	Telegram map[string]string `json:"telegram,omitempty" yaml:"telegram,omitempty"`
	Discord  map[string]string `json:"discord,omitempty" yaml:"discord,omitempty"`
}

type storeSecurityRecord struct {
	Users        map[string]storeSecurityUserRecord `json:"users,omitempty" yaml:"users,omitempty"`
	ChannelLinks storeChannelLinksRecord            `json:"channelLinks,omitempty" yaml:"channelLinks,omitempty"`
}

func readYAMLFileOrDefault[T any](filename string, result *T) error {
	fileContent, readError := os.ReadFile(filename)
	if readError != nil {
		if os.IsNotExist(readError) {
			return nil
		}
		return readError
	}
	return yaml.Unmarshal(fileContent, result)
}

func writeYAMLFile(filename string, value any) error {
	directory := filepath.Dir(filename)
	if makeDirectoryError := os.MkdirAll(directory, 0755); makeDirectoryError != nil {
		return makeDirectoryError
	}
	encoded, marshalError := yaml.Marshal(value)
	if marshalError != nil {
		return marshalError
	}
	return atomicfile.WriteFile(filename, encoded)
}

func writeYAMLFileMode(filename string, value any, mode os.FileMode) error {
	directory := filepath.Dir(filename)
	if makeDirectoryError := os.MkdirAll(directory, 0755); makeDirectoryError != nil {
		return makeDirectoryError
	}
	encoded, marshalError := yaml.Marshal(value)
	if marshalError != nil {
		return marshalError
	}
	return atomicfile.WriteFileWithMode(filename, encoded, mode)
}

func normalizeSecurityUsernames(users map[string]storeSecurityUserRecord) {
	if len(users) == 0 {
		return
	}
	usedUsernames := map[string]struct{}{}
	for _, user := range users {
		username := strings.ToLower(user.Username)
		if username != "" {
			usedUsernames[username] = struct{}{}
		}
	}

	userIds := make([]string, 0, len(users))
	for userId := range users {
		userIds = append(userIds, userId)
	}
	sort.Strings(userIds)
	nextIndex := 1
	for _, userId := range userIds {
		user := users[userId]
		if user.Username != "" {
			continue
		}
		for {
			candidateUsername := "user"
			if nextIndex > 1 {
				candidateUsername = "user-" + strconv.Itoa(nextIndex)
			}
			nextIndex++
			candidateLower := strings.ToLower(candidateUsername)
			if _, exists := usedUsernames[candidateLower]; exists {
				continue
			}
			user.Username = candidateUsername
			users[userId] = user
			usedUsernames[candidateLower] = struct{}{}
			break
		}
	}
}

func (self *storeSecurityRecord) FindUserByUsername(username string) (string, storeSecurityUserRecord, bool) {
	if self == nil {
		return "", storeSecurityUserRecord{}, false
	}
	needle := strings.ToLower(username)
	if needle == "" {
		return "", storeSecurityUserRecord{}, false
	}
	for userId, userRecord := range self.Users {
		if strings.ToLower(userRecord.Username) == needle {
			return userId, userRecord, true
		}
	}
	return "", storeSecurityUserRecord{}, false
}

func (self *storeSecurityRecord) FindUserByToken(token string) (string, storeSecurityUserRecord, int, bool) {
	if self == nil {
		return "", storeSecurityUserRecord{}, -1, false
	}
	needle := token
	if needle == "" {
		return "", storeSecurityUserRecord{}, -1, false
	}
	for userId, userRecord := range self.Users {
		for tokenIndex, tokenRecord := range userRecord.Tokens {
			if tokenRecord.Token == needle {
				return userId, userRecord, tokenIndex, true
			}
		}
	}
	return "", storeSecurityUserRecord{}, -1, false
}
