package embeddings

import (
	"context"
	"fmt"

	"github.com/teanode/teanode/internal/providers"
)

const defaultEmbeddingProviderModelName = "openai:text-embedding-3-small"

// Embedder resolves an embedding provider and model from the provider registry
// and computes vector embeddings. The embedding model name is read from the
// registry's configuration (set at registry construction time), falling back
// to the default (text-embedding-3-small).
type Embedder struct {
	providerRegistry *providers.ProviderRegistry
}

// NewEmbedder creates a new Embedder backed by the given provider registry.
func NewEmbedder(providerRegistry *providers.ProviderRegistry) *Embedder {
	return &Embedder{providerRegistry: providerRegistry}
}

// Embed computes a vector embedding for the given input text. It resolves the
// embedding provider and model from the provider registry's configuration.
// Returns the embedding vector, the canonical provider model name
// ("provider:model"), and any error.
func (self *Embedder) Embed(ctx context.Context, inputText string) ([]float64, string, error) {
	provider, providerName, modelName, err := self.resolveEmbeddingProvider()
	if err != nil {
		return nil, "", err
	}
	canonicalName := providers.FormatProviderModelName(providerName, modelName)
	vector, embedError := provider.Embed(ctx, modelName, inputText)
	if embedError != nil {
		return nil, canonicalName, embedError
	}
	return vector, canonicalName, nil
}

// resolveEmbeddingProvider resolves the embedding provider, provider name, and
// model name from the provider registry's configuration.
func (self *Embedder) resolveEmbeddingProvider() (providers.EmbeddingProvider, string, string, error) {
	providerModelName := self.providerRegistry.GetEmbeddingProviderModelName()
	if providerModelName == "" {
		providerModelName = defaultEmbeddingProviderModelName
	}

	providerName, modelName := providers.ParseProviderModelName(providerModelName, "openai")

	// Try the configured provider first.
	embedder, ok := self.providerRegistry.FindEmbedderByName(providerName)
	if !ok {
		// Fall back to first registered provider with embedding support.
		var fallbackName string
		embedder, fallbackName, ok = self.providerRegistry.FindEmbedder()
		if !ok {
			return nil, "", "", fmt.Errorf("no embedding provider available")
		}
		providerName = fallbackName
	}

	return embedder, providerName, modelName, nil
}
