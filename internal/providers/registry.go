package providers

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// ProviderRegistry holds named provider clients and resolves qualified model IDs.
type ProviderRegistry struct {
	clients               map[string]Provider
	defaultProvider       string
	defaultQualifiedModel string
}

// NewProviderRegistry creates a provider registry from the given models configuration.
// If modelsConfiguration is nil, sensible defaults are applied (openai:gpt-5.2
// with the OPENAI_API_KEY environment variable).
func NewProviderRegistry(modelsConfiguration *models.ModelsConfiguration) *ProviderRegistry {
	if modelsConfiguration == nil {
		modelsConfiguration = &models.ModelsConfiguration{}
	}
	if modelsConfiguration.GetDefault() == "" {
		modelsConfiguration.Default = ptrto.Value("openai:gpt-5.2")
	}
	if modelsConfiguration.Providers == nil || len(*modelsConfiguration.Providers) == 0 {
		defaultProviders := []*models.ProviderConfiguration{
			{
				Name:    ptrto.Value("openai"),
				BaseURL: ptrto.Value("https://api.openai.com/v1"),
				APIKey:  ptrto.Value(os.Getenv("OPENAI_API_KEY")),
			},
		}
		modelsConfiguration.Providers = &defaultProviders
	}

	defaultProviderName, _ := ParseQualifiedModel(modelsConfiguration.GetDefault(), "openai")

	providerRegistry := &ProviderRegistry{
		clients:               make(map[string]Provider),
		defaultProvider:       defaultProviderName,
		defaultQualifiedModel: modelsConfiguration.GetDefault(),
	}

	for _, providerConfiguration := range *modelsConfiguration.Providers {
		name := providerConfiguration.GetName()
		if name == "" {
			continue
		}
		providerRegistry.Register(name, NewProvider(
			name,
			providerConfiguration.GetBaseURL(),
			providerConfiguration.GetAPIKey(),
		))
	}

	hasKey := false
	for _, providerConfiguration := range *modelsConfiguration.Providers {
		if providerConfiguration.GetAPIKey() != "" {
			hasKey = true
			break
		}
	}
	if !hasKey {
		log.Warning("no API key configured (set OPENAI_API_KEY or models.apiKey in config)")
	}

	return providerRegistry
}

// NewEmptyProviderRegistry creates a provider registry with no providers or
// defaults. Useful in tests that verify "no model configured" behaviour.
func NewEmptyProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		clients: make(map[string]Provider),
	}
}

// Register adds a named provider client.
func (self *ProviderRegistry) Register(name string, client Provider) {
	self.clients[name] = client
}

// Resolve splits a qualified model ID ("provider:model") and returns the
// corresponding client and bare model name. If the model has no provider
// prefix, the default provider is used.
func (self *ProviderRegistry) ResolveProviderAndModel(qualifiedModel string) (Provider, string, string, error) {
	if qualifiedModel == "" {
		qualifiedModel = self.defaultQualifiedModel
	}
	if qualifiedModel == "" {
		return nil, "", "", fmt.Errorf("no model configured")
	}
	providerName, modelName := ParseQualifiedModel(qualifiedModel, self.defaultProvider)
	client, ok := self.clients[providerName]
	if !ok {
		return nil, "", "", fmt.Errorf("unknown provider: %q", providerName)
	}
	return client, providerName, modelName, nil
}

// ProviderNames returns sorted provider names.
func (self *ProviderRegistry) ProviderNames() []string {
	names := make([]string, 0, len(self.clients))
	for name := range self.clients {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DefaultProvider returns the default provider name.
func (self *ProviderRegistry) DefaultProvider() string {
	return self.defaultProvider
}

// DefaultModel returns the default qualified model string.
func (self *ProviderRegistry) DefaultModel() string {
	return self.defaultQualifiedModel
}

// ParseQualifiedModel splits "provider:model" on the first colon.
// If there is no colon, defaultProvider is used.
func ParseQualifiedModel(qualified, defaultProvider string) (string, string) {
	if index := strings.IndexByte(qualified, ':'); index >= 0 {
		return qualified[:index], qualified[index+1:]
	}
	return defaultProvider, qualified
}

// QualifyModel joins a provider name and model into "provider:model".
func QualifyModel(providerName, modelName string) string {
	return providerName + ":" + modelName
}

// FindTranscriber returns the first registered provider that implements AudioTranscriber.
func (self *ProviderRegistry) FindTranscriber() (AudioTranscriber, string, bool) {
	for name, client := range self.clients {
		if transcriber, ok := client.(AudioTranscriber); ok {
			return transcriber, name, true
		}
	}
	return nil, "", false
}

// FindSynthesizer returns the first registered provider that implements AudioSynthesizer.
func (self *ProviderRegistry) FindSynthesizer() (AudioSynthesizer, string, bool) {
	for name, client := range self.clients {
		if synthesizer, ok := client.(AudioSynthesizer); ok {
			return synthesizer, name, true
		}
	}
	return nil, "", false
}
