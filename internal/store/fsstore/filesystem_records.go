package fsstore

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/timeutil"
	"gopkg.in/yaml.v3"
)

// encodeEmbeddingBase64 encodes a float64 slice as little-endian packed binary,
// then base64 standard-encodes it.
func encodeEmbeddingBase64(values []float64) string {
	buf := make([]byte, len(values)*8)
	for index, value := range values {
		binary.LittleEndian.PutUint64(buf[index*8:], math.Float64bits(value))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// decodeEmbeddingBase64 decodes a base64-encoded, little-endian packed float64 slice.
func decodeEmbeddingBase64(encoded string) ([]float64, error) {
	raw, decodeError := base64.StdEncoding.DecodeString(encoded)
	if decodeError != nil {
		return nil, decodeError
	}
	if len(raw)%8 != 0 {
		return nil, errInvalidEmbeddingLength
	}
	values := make([]float64, len(raw)/8)
	for index := range values {
		values[index] = math.Float64frombits(binary.LittleEndian.Uint64(raw[index*8:]))
	}
	return values, nil
}

var errInvalidEmbeddingLength = errors.New("embedding binary length is not a multiple of 8")

type storeGatewayRecord struct {
	Port      int    `json:"port,omitempty" yaml:"port,omitempty"`
	Bind      string `json:"bind,omitempty" yaml:"bind,omitempty"`
	PublicURL string `json:"publicUrl,omitempty" yaml:"publicUrl,omitempty"`
	TLS       bool   `json:"tls,omitempty" yaml:"tls,omitempty"`
}

type storeCertificateRecord struct {
	ACMEEmail      string `json:"acmeEmail,omitempty" yaml:"acmeEmail,omitempty"`
	ACMEAccountKey string `json:"acmeAccountKey,omitempty" yaml:"acmeAccountKey,omitempty"`
	Domain         string `json:"domain,omitempty" yaml:"domain,omitempty"`
	Certificate    string `json:"certificate,omitempty" yaml:"certificate,omitempty"`
	PrivateKey     string `json:"privateKey,omitempty" yaml:"privateKey,omitempty"`
	IssuedAt       string `json:"issuedAt,omitempty" yaml:"issuedAt,omitempty"`
	ExpiresAt      string `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`
}

type storeProviderRecord struct {
	Name    string `json:"name,omitempty" yaml:"name,omitempty"`
	BaseURL string `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
	APIKey  string `json:"apiKey,omitempty" yaml:"apiKey,omitempty"`
}

type storeModelsRecord struct {
	Default                     string                `json:"default,omitempty" yaml:"default,omitempty"`
	SummarizerProviderModelName string                `json:"summarizerModel,omitempty" yaml:"summarizerModel,omitempty"`
	EmbeddingProviderModelName  string                `json:"embeddingProviderModelName,omitempty" yaml:"embeddingProviderModelName,omitempty"`
	ContextWindow               int                   `json:"contextWindow,omitempty" yaml:"contextWindow,omitempty"`
	Providers                   []storeProviderRecord `json:"providers,omitempty" yaml:"providers,omitempty"`
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
	ModelName             string   `json:"model,omitempty" yaml:"model,omitempty"`
	MaxTurnTimeoutSeconds int      `json:"maxTurnTimeoutSeconds,omitempty" yaml:"maxTurnTimeoutSeconds,omitempty"`
}

type storeCodexToolRecord struct {
	BinaryPath            string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	AllowedTools          []string `json:"allowedTools,omitempty" yaml:"allowedTools,omitempty"`
	ModelName             string   `json:"model,omitempty" yaml:"model,omitempty"`
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
	Certificate      *storeCertificateRecord    `json:"certificate,omitempty" yaml:"certificate,omitempty"`
	Models           storeModelsRecord          `json:"models,omitempty" yaml:"models,omitempty"`
	Tools            storeToolsRecord           `json:"tools,omitempty" yaml:"tools,omitempty"`
	Integrations     storeIntegrationsRecord    `json:"integrations,omitempty" yaml:"integrations,omitempty"`
	Channels         storeChannelsRecord        `json:"channels,omitempty" yaml:"channels,omitempty"`
	Secrets          map[string]string          `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	SkillsRegistries []storeSkillRegistryRecord `json:"skillsRegistries,omitempty" yaml:"skillsRegistries,omitempty"`
}

type storeAgentRecord struct {
	ID                string             `json:"id" yaml:"id"`
	Name              string             `json:"name,omitempty" yaml:"name,omitempty"`
	ProviderModelName string             `json:"model,omitempty" yaml:"model,omitempty"`
	Skills            []string           `json:"skills,omitempty" yaml:"skills,omitempty"`
	Tools             []string           `json:"tools,omitempty" yaml:"tools,omitempty"`
	Description       string             `json:"description,omitempty" yaml:"description,omitempty"`
	SummarizedAt      timeutil.Timestamp `json:"summarizedAt,omitempty" yaml:"summarizedAt,omitempty"`
	AvatarMediaID     string             `json:"avatarMediaId,omitempty" yaml:"avatarMediaId,omitempty"`
}

type storeUserRecord struct {
	ID                     string             `yaml:"id"`
	Name                   string             `yaml:"name"`
	Username               string             `yaml:"username,omitempty"`
	PasswordHash           string             `yaml:"passwordHash,omitempty"`
	Admin                  bool               `yaml:"admin,omitempty"`
	DefaultAgentID         string             `yaml:"defaultAgentId,omitempty"`
	TelegramChatID         *int64             `yaml:"telegramChatId,omitempty"`
	DiscordUserID          string             `yaml:"discordUserId,omitempty"`
	Description            string             `yaml:"description,omitempty"`
	AvatarMediaID          string             `yaml:"avatarMediaId,omitempty"`
	SummarizedAt           timeutil.Timestamp `yaml:"summarizedAt,omitempty"`
	DefaultConversationIDs map[string]string  `yaml:"defaultConversationIds,omitempty"`
}

type storeProjectRecord struct {
	ID           string             `json:"id" yaml:"id"`
	Name         string             `json:"name" yaml:"name"`
	Description  string             `json:"description" yaml:"description"`
	SummarizedAt timeutil.Timestamp `json:"summarizedAt,omitempty" yaml:"summarizedAt,omitempty"`
	UpdatedAt    timeutil.Timestamp `json:"updatedAt" yaml:"updatedAt"`
}

type storeMemoryItemFrontmatter struct {
	ID                         string             `yaml:"id"`
	Scope                      string             `yaml:"scope"`
	ScopeID                    string             `yaml:"scopeId"`
	Title                      string             `yaml:"title,omitempty"`
	Tags                       []string           `yaml:"tags,omitempty"`
	ArchivedAt                 timeutil.Timestamp `yaml:"archivedAt,omitempty"`
	CreatedAt                  timeutil.Timestamp `yaml:"createdAt"`
	ModifiedAt                 timeutil.Timestamp `yaml:"modifiedAt"`
	EmbeddingProviderModelName string             `yaml:"embeddingProviderModelName,omitempty"`
	Embedding                  string             `yaml:"embedding,omitempty"`
	EmbeddedAt                 timeutil.Timestamp `yaml:"embeddedAt,omitempty"`
}

type storeMemoryItemRecord struct {
	storeMemoryItemFrontmatter `yaml:",inline"`
	Content                    string `yaml:"-"`
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

func normalizeUsername(records []storeUserRecord, record *storeUserRecord) {
	if record.Username != "" {
		return
	}
	usedUsernames := map[string]struct{}{}
	for _, existing := range records {
		if lowered := strings.ToLower(existing.Username); lowered != "" {
			usedUsernames[lowered] = struct{}{}
		}
	}
	nextIndex := 1
	for {
		candidate := "user"
		if nextIndex > 1 {
			candidate = "user-" + strconv.Itoa(nextIndex)
		}
		nextIndex++
		if _, exists := usedUsernames[strings.ToLower(candidate)]; !exists {
			record.Username = candidate
			return
		}
	}
}
