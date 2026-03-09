package providers

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/util/ptrto"
)

const modelsCacheTTL = 5 * time.Minute

// cachedModelList holds cached model results for a single provider.
type cachedModelList struct {
	models    []ModelInformation
	expiresAt time.Time
}

// ProviderModelEntry is a model tagged with its provider name, for API consumers.
type ProviderModelEntry struct {
	ProviderName  string `json:"provider"`
	ModelName     string `json:"id"`
	ContextLength int    `json:"context_length,omitempty"`
}

// ProviderRegistry holds named provider clients and resolves provider model names.
type ProviderRegistry struct {
	clients                     map[string]Provider
	defaultProvider             string
	defaultProviderModelName    string
	embeddingProviderModelName  string
	summarizerProviderModelName string
	modelsCacheMutex            sync.Mutex
	modelsCache                 map[string]*cachedModelList
}

// NewProviderRegistry creates a provider registry from the given models configuration.
// If modelsConfiguration is nil, sensible defaults are applied (openai:gpt-5.2
// with the OPENAI_API_KEY environment variable).
func NewProviderRegistry(modelsConfiguration *models.ModelsConfiguration) *ProviderRegistry {
	if modelsConfiguration == nil {
		modelsConfiguration = &models.ModelsConfiguration{}
	}
	if modelsConfiguration.Providers == nil || len(*modelsConfiguration.Providers) == 0 {
		var defaultProviders []*models.ProviderConfiguration
		if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
			defaultProviders = append(defaultProviders, &models.ProviderConfiguration{
				Name:    ptrto.Value("openai"),
				BaseURL: ptrto.Value("https://api.openai.com/v1"),
				APIKey:  ptrto.Value(apiKey),
			})
		}
		if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
			defaultProviders = append(defaultProviders, &models.ProviderConfiguration{
				Name:    ptrto.Value("anthropic"),
				BaseURL: ptrto.Value("https://api.anthropic.com"),
				APIKey:  ptrto.Value(apiKey),
			})
		}
		if apiKey := os.Getenv("OPENROUTER_API_KEY"); apiKey != "" {
			defaultProviders = append(defaultProviders, &models.ProviderConfiguration{
				Name:    ptrto.Value("openrouter"),
				BaseURL: ptrto.Value("https://openrouter.ai/api/v1"),
				APIKey:  ptrto.Value(apiKey),
			})
		}
		if apiKey := os.Getenv("DEEPGRAM_API_KEY"); apiKey != "" {
			defaultProviders = append(defaultProviders, &models.ProviderConfiguration{
				Name:    ptrto.Value("deepgram"),
				BaseURL: ptrto.Value("https://api.deepgram.com"),
				APIKey:  ptrto.Value(apiKey),
			})
		}
		if apiKey := os.Getenv("ELEVENLABS_API_KEY"); apiKey != "" {
			defaultProviders = append(defaultProviders, &models.ProviderConfiguration{
				Name:    ptrto.Value("elevenlabs"),
				BaseURL: ptrto.Value("https://api.elevenlabs.io"),
				APIKey:  ptrto.Value(apiKey),
			})
		}
		if len(defaultProviders) == 0 {
			defaultProviders = append(defaultProviders, &models.ProviderConfiguration{
				Name:    ptrto.Value("openai"),
				BaseURL: ptrto.Value("https://api.openai.com/v1"),
				APIKey:  ptrto.Value(""),
			})
		}
		modelsConfiguration.Providers = &defaultProviders
	}
	if modelsConfiguration.GetDefault() == "" {
		modelsConfiguration.Default = ptrto.Value(defaultProviderModelName(modelsConfiguration))
	}

	defaultProviderName, _ := ParseProviderModelName(modelsConfiguration.GetDefault(), "openai")

	providerRegistry := &ProviderRegistry{
		clients:                     make(map[string]Provider),
		defaultProvider:             defaultProviderName,
		defaultProviderModelName:    modelsConfiguration.GetDefault(),
		embeddingProviderModelName:  modelsConfiguration.GetEmbeddingProviderModelName(),
		summarizerProviderModelName: modelsConfiguration.GetSummarizerProviderModelName(),
		modelsCache:                 make(map[string]*cachedModelList),
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

	// Default embedding model to openai:text-embedding-3-small when no
	// explicit embedding model is configured and the openai provider exists.
	if providerRegistry.embeddingProviderModelName == "" {
		if _, ok := providerRegistry.clients["openai"]; ok {
			providerRegistry.embeddingProviderModelName = "openai:text-embedding-3-small"
		}
	}

	hasKey := false
	for _, providerConfiguration := range *modelsConfiguration.Providers {
		if providerConfiguration.GetAPIKey() != "" {
			hasKey = true
			break
		}
	}
	if !hasKey {
		log.Warning("no API key configured (set OPENAI_API_KEY, ANTHROPIC_API_KEY, OPENROUTER_API_KEY, DEEPGRAM_API_KEY, ELEVENLABS_API_KEY, or models.apiKey in config)")
	}

	return providerRegistry
}

// NewEmptyProviderRegistry creates a provider registry with no providers or
// defaults. Useful in tests that verify "no model configured" behaviour.
func NewEmptyProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		clients:     make(map[string]Provider),
		modelsCache: make(map[string]*cachedModelList),
	}
}

// Register adds a named provider client.
func (self *ProviderRegistry) Register(name string, client Provider) {
	self.clients[name] = client
}

// GetEmbeddingProviderModelName returns the configured embedding provider model
// name (e.g. "openai:text-embedding-3-small"), or "" if not configured.
func (self *ProviderRegistry) GetEmbeddingProviderModelName() string {
	return self.embeddingProviderModelName
}

// SetEmbeddingProviderModelName sets the embedding provider model name.
func (self *ProviderRegistry) SetEmbeddingProviderModelName(name string) {
	self.embeddingProviderModelName = name
}

// GetSummarizerProviderModelName returns the configured summarizer provider model
// name, or "" if not configured.
func (self *ProviderRegistry) GetSummarizerProviderModelName() string {
	return self.summarizerProviderModelName
}

// ResolveProviderAndModel splits a provider model name ("provider:model") and returns the
// corresponding client and bare model name. If the model has no provider
// prefix, the default provider is used.
func (self *ProviderRegistry) ResolveProviderAndModel(providerModelName string) (Provider, string, string, error) {
	if providerModelName == "" {
		providerModelName = self.defaultProviderModelName
	}
	if providerModelName == "" {
		return nil, "", "", fmt.Errorf("no model configured")
	}
	providerName, modelName := ParseProviderModelName(providerModelName, self.defaultProvider)
	client, ok := self.clients[providerName]
	if !ok {
		return nil, "", "", fmt.Errorf("unknown provider: %q", providerName)
	}
	return client, providerName, modelName, nil
}

// ClientByName returns the provider client registered under the given name.
func (self *ProviderRegistry) ClientByName(name string) (Provider, bool) {
	client, ok := self.clients[name]
	return client, ok
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

// DefaultProviderModelName returns the default provider model name ("provider:model").
func (self *ProviderRegistry) DefaultProviderModelName() string {
	return self.defaultProviderModelName
}

// ParseProviderModelName splits "provider:model" on the first colon.
// If there is no colon, defaultProvider is used.
func ParseProviderModelName(providerModelName, defaultProvider string) (string, string) {
	if index := strings.IndexByte(providerModelName, ':'); index >= 0 {
		return providerModelName[:index], providerModelName[index+1:]
	}
	return defaultProvider, providerModelName
}

// FormatProviderModelName joins a provider name and model into "provider:model".
func FormatProviderModelName(providerName, modelName string) string {
	return providerName + ":" + modelName
}

// defaultProviderModelName picks a sensible default model from the configured providers.
func defaultProviderModelName(configuration *models.ModelsConfiguration) string {
	if configuration.Providers == nil {
		return "openai:gpt-5.2"
	}
	providerNames := make(map[string]bool)
	for _, provider := range *configuration.Providers {
		providerNames[provider.GetName()] = true
	}
	if providerNames["openai"] {
		return "openai:gpt-5.2"
	}
	if providerNames["anthropic"] {
		return "anthropic:claude-sonnet-4-20250514"
	}
	if providerNames["openrouter"] {
		return "openrouter:anthropic/claude-sonnet-4-20250514"
	}
	return "openai:gpt-5.2"
}

// ListAllModels returns models from all registered providers, using a per-provider cache.
func (self *ProviderRegistry) ListAllModels(ctx context.Context) []ProviderModelEntry {
	now := time.Now()
	var allEntries []ProviderModelEntry
	for _, providerName := range self.ProviderNames() {
		providerModels := self.cachedModelsForProvider(ctx, providerName, now)
		for _, entry := range providerModels {
			allEntries = append(allEntries, ProviderModelEntry{
				ProviderName:  providerName,
				ModelName:     entry.ID,
				ContextLength: entry.ContextLength,
			})
		}
	}
	if allEntries == nil {
		allEntries = []ProviderModelEntry{}
	}
	return allEntries
}

// cachedModelsForProvider returns models for a single provider, using the cache.
// On fetch failure, stale cached data is returned if available.
func (self *ProviderRegistry) cachedModelsForProvider(ctx context.Context, providerName string, now time.Time) []ModelInformation {
	self.modelsCacheMutex.Lock()
	cached, hasCached := self.modelsCache[providerName]
	if hasCached && now.Before(cached.expiresAt) {
		result := cached.models
		self.modelsCacheMutex.Unlock()
		return result
	}
	self.modelsCacheMutex.Unlock()

	client, ok := self.clients[providerName]
	if !ok {
		return nil
	}
	chatProvider, ok := client.(ChatProvider)
	if !ok {
		return nil
	}

	fetched, err := chatProvider.ListModels(ctx)
	if err != nil {
		log.Warningf("listing models for provider %q: %v", providerName, err)
		// Return stale data if available.
		if hasCached {
			return cached.models
		}
		return nil
	}

	self.modelsCacheMutex.Lock()
	self.modelsCache[providerName] = &cachedModelList{
		models:    fetched,
		expiresAt: now.Add(modelsCacheTTL),
	}
	self.modelsCacheMutex.Unlock()

	return fetched
}

// FindTranscriber returns the first registered provider that implements TranscribeProvider.
func (self *ProviderRegistry) FindTranscriber() (TranscribeProvider, string, bool) {
	for _, name := range self.ProviderNames() {
		if transcriber, ok := self.clients[name].(TranscribeProvider); ok {
			return transcriber, name, true
		}
	}
	return nil, "", false
}

// FindStreamingTranscriber returns the first registered provider that implements StreamingTranscribeProvider.
func (self *ProviderRegistry) FindStreamingTranscriber() (StreamingTranscribeProvider, string, bool) {
	for _, name := range self.ProviderNames() {
		if transcriber, ok := self.clients[name].(StreamingTranscribeProvider); ok {
			return transcriber, name, true
		}
	}
	return nil, "", false
}

// FindSynthesizer returns the first registered provider that implements SynthesizeProvider.
func (self *ProviderRegistry) FindSynthesizer() (SynthesizeProvider, string, bool) {
	for _, name := range self.ProviderNames() {
		if synthesizer, ok := self.clients[name].(SynthesizeProvider); ok {
			return synthesizer, name, true
		}
	}
	return nil, "", false
}

// FindTranscriberByName resolves a named provider and returns it only when the
// provider supports TranscribeProvider.
func (self *ProviderRegistry) FindTranscriberByName(name string) (TranscribeProvider, bool) {
	client, ok := self.clients[name]
	if !ok {
		return nil, false
	}
	transcriber, ok := client.(TranscribeProvider)
	if !ok {
		return nil, false
	}
	return transcriber, true
}

// FindStreamingTranscriberByName resolves a named provider and returns it only
// when the provider supports StreamingTranscribeProvider.
func (self *ProviderRegistry) FindStreamingTranscriberByName(name string) (StreamingTranscribeProvider, bool) {
	client, ok := self.clients[name]
	if !ok {
		return nil, false
	}
	transcriber, ok := client.(StreamingTranscribeProvider)
	if !ok {
		return nil, false
	}
	return transcriber, true
}

// FindSynthesizerByName resolves a named provider and returns it only when the
// provider supports SynthesizeProvider.
func (self *ProviderRegistry) FindSynthesizerByName(name string) (SynthesizeProvider, bool) {
	client, ok := self.clients[name]
	if !ok {
		return nil, false
	}
	synthesizer, ok := client.(SynthesizeProvider)
	if !ok {
		return nil, false
	}
	return synthesizer, true
}

// FindEmbedder returns the first registered provider that implements EmbeddingProvider.
func (self *ProviderRegistry) FindEmbedder() (EmbeddingProvider, string, bool) {
	for _, name := range self.ProviderNames() {
		if embedder, ok := self.clients[name].(EmbeddingProvider); ok {
			return embedder, name, true
		}
	}
	return nil, "", false
}

// FindEmbedderByName resolves a named provider and returns it only when the
// provider supports EmbeddingProvider.
func (self *ProviderRegistry) FindEmbedderByName(name string) (EmbeddingProvider, bool) {
	client, ok := self.clients[name]
	if !ok {
		return nil, false
	}
	embedder, ok := client.(EmbeddingProvider)
	if !ok {
		return nil, false
	}
	return embedder, true
}
