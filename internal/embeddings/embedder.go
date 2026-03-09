package embeddings

import (
	"context"
	"errors"

	"github.com/teanode/teanode/internal/providers"
)

// ErrEmbeddingDisabled is returned when no embedding provider model is
// configured and no embedding-capable provider is registered.
var ErrEmbeddingDisabled = errors.New("embedding is disabled (no embedding provider configured)")

// Embedder resolves an embedding provider and model from the provider registry
// and computes vector embeddings.
type Embedder struct {
	providerRegistry *providers.ProviderRegistry
}

// NewEmbedder creates a new Embedder backed by the given provider registry.
func NewEmbedder(providerRegistry *providers.ProviderRegistry) *Embedder {
	return &Embedder{providerRegistry: providerRegistry}
}

// Embed computes a vector embedding for the given input text. It resolves the
// embedding provider and model from the provider registry's configuration.
// Returns ErrEmbeddingDisabled when no embedding provider model is configured.
func (self *Embedder) Embed(ctx context.Context, inputText string) ([]float64, string, error) {
	provider, providerName, modelName, err := self.resolveEmbeddingProvider()
	if err != nil {
		return nil, "", err
	}
	providerModelName := providers.FormatProviderModelName(providerName, modelName)
	vector, embedError := provider.Embed(ctx, modelName, inputText)
	if embedError != nil {
		return nil, providerModelName, embedError
	}
	return vector, providerModelName, nil
}

// EmbedMany computes vector embeddings for multiple input texts in a single
// batch API call. On batch failure, it falls back to embedding each input
// individually. Returns ErrEmbeddingDisabled when no embedding provider is
// configured.
func (self *Embedder) EmbedMany(ctx context.Context, inputTexts []string) ([][]float64, string, error) {
	if len(inputTexts) == 0 {
		return nil, "", nil
	}

	provider, providerName, modelName, err := self.resolveEmbeddingProvider()
	if err != nil {
		return nil, "", err
	}
	providerModelName := providers.FormatProviderModelName(providerName, modelName)

	// Try batch request.
	vectors, batchError := provider.EmbedMany(ctx, modelName, inputTexts)
	if batchError == nil {
		return vectors, providerModelName, nil
	}

	// Batch failed — fall back to individual requests.
	vectors = make([][]float64, len(inputTexts))
	var lastError error
	for index, text := range inputTexts {
		vector, embedError := provider.Embed(ctx, modelName, text)
		if embedError != nil {
			lastError = embedError
			continue
		}
		vectors[index] = vector
	}
	if lastError != nil {
		return vectors, providerModelName, lastError
	}
	return vectors, providerModelName, nil
}

// resolveEmbeddingProvider resolves the embedding provider, provider name, and
// model name from the provider registry's configuration.
func (self *Embedder) resolveEmbeddingProvider() (providers.EmbeddingProvider, string, string, error) {
	providerModelName := self.providerRegistry.GetEmbeddingProviderModelName()
	if providerModelName == "" {
		return nil, "", "", ErrEmbeddingDisabled
	}

	providerName, modelName := providers.ParseProviderModelName(providerModelName, "openai")

	embedder, ok := self.providerRegistry.FindEmbedderByName(providerName)
	if !ok {
		return nil, "", "", ErrEmbeddingDisabled
	}

	return embedder, providerName, modelName, nil
}
