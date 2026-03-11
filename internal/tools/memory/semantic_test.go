package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/tools"
)

// stubEmbeddingsProvider returns deterministic embeddings based on content keywords.
// Each keyword maps to a dimension in the vector; if the text contains the keyword,
// that dimension is set to 1.0, producing predictable cosine similarity.
type stubEmbeddingsProvider struct {
	providers.BaseProvider
	keywords []string
	calls    int
}

func newStubEmbeddingsProvider(keywords ...string) *stubEmbeddingsProvider {
	return &stubEmbeddingsProvider{keywords: keywords}
}

func (self *stubEmbeddingsProvider) Embed(_ context.Context, _ string, inputText string) ([]float64, *providers.UsageInformation, error) {
	self.calls++
	vector := make([]float64, len(self.keywords))
	for index, keyword := range self.keywords {
		if containsWord(inputText, keyword) {
			vector[index] = 1.0
		}
	}
	return vector, nil, nil
}

func (self *stubEmbeddingsProvider) EmbedMany(_ context.Context, _ string, inputTexts []string) ([][]float64, *providers.UsageInformation, error) {
	vectors := make([][]float64, len(inputTexts))
	for index, text := range inputTexts {
		self.calls++
		vector := make([]float64, len(self.keywords))
		for keywordIndex, keyword := range self.keywords {
			if containsWord(text, keyword) {
				vector[keywordIndex] = 1.0
			}
		}
		vectors[index] = vector
	}
	return vectors, nil, nil
}

func containsWord(text, word string) bool {
	// Simple substring check for test purposes.
	for _, field := range splitWords(text) {
		if field == word {
			return true
		}
	}
	return false
}

func splitWords(text string) []string {
	var words []string
	current := ""
	for _, char := range text {
		if char == ' ' || char == '\n' || char == '\t' || char == ',' || char == '.' {
			if current != "" {
				words = append(words, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}
	if current != "" {
		words = append(words, current)
	}
	return words
}

// failingEmbeddingsProvider always returns an error.
type failingEmbeddingsProvider struct {
	providers.BaseProvider
}

func (self *failingEmbeddingsProvider) Embed(_ context.Context, _ string, _ string) ([]float64, *providers.UsageInformation, error) {
	return nil, nil, fmt.Errorf("embedding service unavailable")
}

func (self *failingEmbeddingsProvider) EmbedMany(_ context.Context, _ string, _ []string) ([][]float64, *providers.UsageInformation, error) {
	return nil, nil, fmt.Errorf("embedding service unavailable")
}

// buildEmbeddingRegistry creates a provider registry with the given stub
// registered as "openai" and embedding enabled.
func buildEmbeddingRegistry(stub providers.Provider) *providers.ProviderRegistry {
	registry := providers.NewEmptyProviderRegistry()
	registry.Register("openai", stub)
	registry.SetEmbeddingProviderModelName("openai:text-embedding-3-small")
	return registry
}

func setupFSMemoryStore(t *testing.T, providerRegistry *providers.ProviderRegistry) context.Context {
	t.Helper()
	openedStore, openError := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("open fsstore: %v", openError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	ctx := store.ContextWithStore(context.Background(), openedStore)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-semantic", "", providerRegistry, models.Agent{}))
	return ctx
}

func addMemoryItem(t *testing.T, ctx context.Context, tool tools.Tool, title, content string) string {
	t.Helper()
	args, _ := json.Marshal(map[string]interface{}{
		"action": "batch",
		"items": []map[string]interface{}{
			{"op": "add", "title": title, "content": content},
		},
	})
	result, err := tool.Execute(ctx, string(args))
	if err != nil {
		t.Fatalf("batch add: %v", err)
	}
	var response memoryBatchResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal retrieve response: %v", err)
	}
	if response.Summary.Succeeded != 1 {
		t.Fatalf("expected 1 success, got %+v, error: %s", response.Summary, response.Results[0].Error)
	}
	return response.Results[0].Item["id"].(string)
}

func TestSemanticRetrieveOrdering(t *testing.T) {
	// Keyword space: cat, dog, fish, bird
	stub := newStubEmbeddingsProvider("cat", "dog", "fish", "bird")
	ctx := setupFSMemoryStore(t, buildEmbeddingRegistry(stub))

	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}
	tool := registry.Get("agent_memory")

	// Add items; embedding provider will be called for each.
	addMemoryItem(t, ctx, tool, "Cat facts", "cat lovers unite")
	addMemoryItem(t, ctx, tool, "Dog facts", "dog walking tips")
	addMemoryItem(t, ctx, tool, "Fish facts", "fish tank maintenance")

	// Retrieve with query "cat" - should rank Cat facts first.
	result, err := tool.Execute(ctx, `{"action":"retrieve","query":"cat"}`)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	var response struct {
		Action   string `json:"action"`
		Method   string `json:"method"`
		Snippets []struct {
			ItemID string  `json:"itemId"`
			Title  string  `json:"title"`
			Score  float64 `json:"score"`
		} `json:"snippets"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal retrieve response: %v", err)
	}

	if response.Method != "semantic" {
		t.Errorf("expected method=semantic, got %q", response.Method)
	}
	if len(response.Snippets) == 0 {
		t.Fatal("expected at least 1 snippet")
	}
	if response.Snippets[0].Title != "Cat facts" {
		t.Errorf("expected first result to be 'Cat facts', got %q", response.Snippets[0].Title)
	}
	if response.Snippets[0].Score <= 0 {
		t.Errorf("expected positive score, got %f", response.Snippets[0].Score)
	}
}

func TestSemanticRetrieveFallbackWhenNoEmbeddings(t *testing.T) {
	// Empty provider registry (no embedding providers registered).
	ctx := setupFSMemoryStore(t, providers.NewEmptyProviderRegistry())

	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}
	tool := registry.Get("agent_memory")

	addMemoryItem(t, ctx, tool, "Golang notes", "goroutines for concurrency")

	// Should fall back to keyword retrieval.
	result, err := tool.Execute(ctx, `{"action":"retrieve","query":"goroutines"}`)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	var response struct {
		Method   string `json:"method"`
		Snippets []struct {
			Title string `json:"title"`
		} `json:"snippets"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal retrieve response: %v", err)
	}

	// No method field means keyword fallback.
	if response.Method == "semantic" {
		t.Error("expected keyword fallback, got semantic method")
	}
	if len(response.Snippets) == 0 {
		t.Fatal("expected at least 1 snippet from keyword fallback")
	}
}

func TestSemanticRetrieveFallbackOnEmbeddingError(t *testing.T) {
	// Use failing embedder. Items added with failing embedder will have no
	// embeddings, so retrieve should fall back to keyword search.
	ctx := setupFSMemoryStore(t, buildEmbeddingRegistry(&failingEmbeddingsProvider{}))

	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}
	tool := registry.Get("agent_memory")

	addMemoryItem(t, ctx, tool, "Test item", "keyword searching works")

	result, err := tool.Execute(ctx, `{"action":"retrieve","query":"keyword searching"}`)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	var response struct {
		Method   string `json:"method"`
		Snippets []struct {
			Title string `json:"title"`
		} `json:"snippets"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal retrieve response: %v", err)
	}

	// Should have fallen back to keyword.
	if response.Method == "semantic" {
		t.Error("expected keyword fallback when embeddings fail, got semantic")
	}
	if len(response.Snippets) == 0 {
		t.Fatal("expected keyword results even when embedding fails")
	}
}

func TestEmbeddingPersistedOnAdd(t *testing.T) {
	stub := newStubEmbeddingsProvider("hello", "world")
	ctx := setupFSMemoryStore(t, buildEmbeddingRegistry(stub))

	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}
	tool := registry.Get("agent_memory")

	itemId := addMemoryItem(t, ctx, tool, "Greeting", "hello world")

	// Verify embedding was persisted by getting the item.
	result, err := tool.Execute(ctx, `{"action":"get","id":"`+itemId+`"}`)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// The output doesn't expose embedding directly, so verify via store.
	var item *models.MemoryItem
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		var getError error
		item, getError = tx.GetMemoryItem(ctx, itemId, nil)
		return getError
	}); err != nil {
		t.Fatalf("load memory item: %v", err)
	}

	if item.Embedding == nil || len(*item.Embedding) == 0 {
		t.Fatal("expected embedding to be persisted on item")
	}
	if item.EmbeddingProviderModelName == nil || *item.EmbeddingProviderModelName != "openai:text-embedding-3-small" {
		t.Errorf("expected embeddingProviderModelName=openai:text-embedding-3-small, got %v", item.EmbeddingProviderModelName)
	}
	if item.EmbeddedAt == nil {
		t.Error("expected embeddedAt to be set")
	}

	// Verify the embedding values are correct: "hello" and "world" should both be 1.0
	embedding := *item.Embedding
	if len(embedding) != 2 {
		t.Fatalf("expected 2-dimensional embedding, got %d", len(embedding))
	}
	if embedding[0] != 1.0 || embedding[1] != 1.0 {
		t.Errorf("expected [1.0, 1.0], got %v", embedding)
	}

	_ = result
}

func TestDedupeWarningOnSimilarAdd(t *testing.T) {
	// Use keywords that will produce identical embeddings for near-duplicate content.
	stub := newStubEmbeddingsProvider("cat", "dog", "preference")
	ctx := setupFSMemoryStore(t, buildEmbeddingRegistry(stub))

	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}
	tool := registry.Get("agent_memory")

	// Add first item about cat preference.
	addMemoryItem(t, ctx, tool, "Cat pref", "cat preference noted")

	// Add a near-duplicate (same keywords → identical embedding → similarity 1.0).
	args, _ := json.Marshal(map[string]interface{}{
		"action": "batch",
		"items": []map[string]interface{}{
			{"op": "add", "title": "Cat update", "content": "cat preference updated"},
		},
	})
	result, err := tool.Execute(ctx, string(args))
	if err != nil {
		t.Fatalf("batch add: %v", err)
	}

	var response memoryBatchResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal retrieve response: %v", err)
	}
	if response.Summary.Succeeded != 1 {
		t.Fatalf("add should still succeed, got %+v", response.Summary)
	}
	if response.Results[0].Warning == "" {
		t.Error("expected dedupe warning for near-duplicate item")
	}
}

func TestNoDedupeWarningForDifferentItems(t *testing.T) {
	stub := newStubEmbeddingsProvider("cat", "dog", "fish")
	ctx := setupFSMemoryStore(t, buildEmbeddingRegistry(stub))

	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}
	tool := registry.Get("agent_memory")

	// Add item about cats.
	addMemoryItem(t, ctx, tool, "Cat info", "cat is great")

	// Add item about dogs (different embedding).
	args, _ := json.Marshal(map[string]interface{}{
		"action": "batch",
		"items": []map[string]interface{}{
			{"op": "add", "title": "Dog info", "content": "dog is friendly"},
		},
	})
	result, err := tool.Execute(ctx, string(args))
	if err != nil {
		t.Fatalf("batch add: %v", err)
	}

	var response memoryBatchResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal retrieve response: %v", err)
	}
	if response.Results[0].Warning != "" {
		t.Errorf("expected no dedupe warning for different items, got %q", response.Results[0].Warning)
	}
}

func TestAddWithoutEmbeddingsProvider(t *testing.T) {
	// Empty provider registry (no embedding providers registered).
	ctx := setupFSMemoryStore(t, providers.NewEmptyProviderRegistry())

	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}
	tool := registry.Get("agent_memory")

	// Should work without embeddings, no warning.
	args, _ := json.Marshal(map[string]interface{}{
		"action": "batch",
		"items": []map[string]interface{}{
			{"op": "add", "title": "Plain item", "content": "no embeddings here"},
		},
	})
	result, err := tool.Execute(ctx, string(args))
	if err != nil {
		t.Fatalf("batch add: %v", err)
	}

	var response memoryBatchResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal retrieve response: %v", err)
	}
	if response.Summary.Succeeded != 1 {
		t.Fatalf("expected success, got %+v", response.Summary)
	}
	if response.Results[0].Warning != "" {
		t.Errorf("expected no warning without embeddings, got %q", response.Results[0].Warning)
	}
}
