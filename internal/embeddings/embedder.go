package embeddings

import (
	"context"
	"errors"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// ErrEmbeddingDisabled is returned when no embedding provider model is
// configured and no embedding-capable provider is registered.
var ErrEmbeddingDisabled = errors.New("embeddings: embedding is disabled (no embedding provider configured)")

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
	vector, usage, embedError := provider.Embed(ctx, modelName, inputText)
	if usage != nil {
		self.recordUsage(ctx, providerName, modelName, usage)
	}
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
	vectors, usage, batchError := provider.EmbedMany(ctx, modelName, inputTexts)
	if batchError == nil {
		if usage != nil {
			self.recordUsage(ctx, providerName, modelName, usage)
		}
		return vectors, providerModelName, nil
	}

	// Batch failed — fall back to individual requests.
	vectors = make([][]float64, len(inputTexts))
	var lastError error
	for index, text := range inputTexts {
		vector, singleUsage, embedError := provider.Embed(ctx, modelName, text)
		if singleUsage != nil {
			self.recordUsage(ctx, providerName, modelName, singleUsage)
		}
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

// recordUsage accumulates embedding usage into the store.
func (self *Embedder) recordUsage(ctx context.Context, providerName, modelName string, usage *providers.UsageInformation) {
	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return
	}
	user := models.UserFromContext(ctx)
	var userId string
	if user != nil {
		userId = user.ID
	}
	err := dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.AccumulateUsage(ctx, &models.Usage{
			UserID:       ptrto.Value(userId),
			ProviderName: ptrto.Value(providerName),
			ModelName:    ptrto.Value(modelName),
			PromptTokens: ptrto.Value(uint64(usage.PromptTokens)),
			TotalTokens:  ptrto.Value(uint64(usage.TotalTokens)),
			RequestCount: ptrto.Value(uint64(1)),
		}, nil)
	})
	if err != nil {
		log.Warningf("failed to record embedding usage: %v", err)
	}
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
