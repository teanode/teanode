package providers

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
)

// mockProvider implements Provider for testing the providerRegistry.
type mockProvider struct {
	name string
}

func (self *mockProvider) ChatCompletion(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{ModelName: self.name}, nil
}

func (self *mockProvider) ChatCompletionStream(ctx context.Context, request ChatRequest) (<-chan StreamEvent, error) {
	return nil, nil
}

func (self *mockProvider) ListModels(ctx context.Context) ([]ModelInformation, error) {
	return nil, nil
}

type mockTranscriberProvider struct {
	mockProvider
}

func (self *mockTranscriberProvider) Transcribe(ctx context.Context, request TranscribeRequest) (*TranscribeResponse, error) {
	return &TranscribeResponse{Text: "ok"}, nil
}

type mockStreamingTranscriberProvider struct {
	mockProvider
}

func (self *mockStreamingTranscriberProvider) OpenTranscribeStream(ctx context.Context, req StreamTranscribeRequest) (TranscribeStream, error) {
	return nil, nil
}

type mockSynthProvider struct {
	mockProvider
}

func (self *mockSynthProvider) Synthesize(ctx context.Context, request SynthesizeRequest) (*SynthesizeResponse, error) {
	return &SynthesizeResponse{
		Audio:       io.NopCloser(strings.NewReader("audio")),
		Format:      "wav",
		ContentType: "audio/wav",
	}, nil
}

func TestNewRegistryNilConfig(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	providerRegistry := NewProviderRegistry(nil)
	if providerRegistry.DefaultProvider() != "openai" {
		t.Errorf("DefaultProvider() = %q, want %q", providerRegistry.DefaultProvider(), "openai")
	}
	if providerRegistry.DefaultProviderModelName() != "openai:gpt-5.2" {
		t.Errorf("DefaultProviderModelName() = %q, want %q", providerRegistry.DefaultProviderModelName(), "openai:gpt-5.2")
	}
	// Should have registered the default openai provider.
	if len(providerRegistry.ProviderNames()) != 1 {
		t.Errorf("expected 1 provider, got %v", providerRegistry.ProviderNames())
	}
}

func TestNewRegistryNilConfigAnthropicKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")
	t.Setenv("OPENROUTER_API_KEY", "")

	providerRegistry := NewProviderRegistry(nil)
	if providerRegistry.DefaultProvider() != "anthropic" {
		t.Errorf("DefaultProvider() = %q, want %q", providerRegistry.DefaultProvider(), "anthropic")
	}
	if providerRegistry.DefaultProviderModelName() != "anthropic:claude-sonnet-4-20250514" {
		t.Errorf("DefaultProviderModelName() = %q, want %q", providerRegistry.DefaultProviderModelName(), "anthropic:claude-sonnet-4-20250514")
	}
	if len(providerRegistry.ProviderNames()) != 1 {
		t.Errorf("expected 1 provider, got %v", providerRegistry.ProviderNames())
	}
}

func TestNewRegistryNilConfigOpenRouterKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "test-openrouter-key")

	providerRegistry := NewProviderRegistry(nil)
	if providerRegistry.DefaultProvider() != "openrouter" {
		t.Errorf("DefaultProvider() = %q, want %q", providerRegistry.DefaultProvider(), "openrouter")
	}
	if providerRegistry.DefaultProviderModelName() != "openrouter:anthropic/claude-sonnet-4-20250514" {
		t.Errorf("DefaultProviderModelName() = %q, want %q", providerRegistry.DefaultProviderModelName(), "openrouter:anthropic/claude-sonnet-4-20250514")
	}
	if len(providerRegistry.ProviderNames()) != 1 {
		t.Errorf("expected 1 provider, got %v", providerRegistry.ProviderNames())
	}
}

func TestNewRegistryNilConfigMultipleKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")
	t.Setenv("OPENROUTER_API_KEY", "test-openrouter-key")

	providerRegistry := NewProviderRegistry(nil)
	// With all keys set, openai should still be the default.
	if providerRegistry.DefaultProvider() != "openai" {
		t.Errorf("DefaultProvider() = %q, want %q", providerRegistry.DefaultProvider(), "openai")
	}
	// All three providers should be registered.
	if len(providerRegistry.ProviderNames()) != 3 {
		t.Errorf("expected 3 providers, got %v", providerRegistry.ProviderNames())
	}
}

func TestNewRegistryNilConfigNoKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	providerRegistry := NewProviderRegistry(nil)
	// Falls back to openai with empty key.
	if providerRegistry.DefaultProvider() != "openai" {
		t.Errorf("DefaultProvider() = %q, want %q", providerRegistry.DefaultProvider(), "openai")
	}
	if len(providerRegistry.ProviderNames()) != 1 {
		t.Errorf("expected 1 provider, got %v", providerRegistry.ProviderNames())
	}
}

func TestNewRegistryWithConfig(t *testing.T) {
	defaultProviderModelName := "anthropic:claude-sonnet-4-20250514"
	providerName := "anthropic"
	providerBaseURL := "https://api.anthropic.com"
	providerKey := "test-key"
	providerRegistry := NewProviderRegistry(&models.ModelsConfiguration{
		Default: &defaultProviderModelName,
		Providers: &[]*models.ProviderConfiguration{
			{
				Name:    &providerName,
				BaseURL: &providerBaseURL,
				APIKey:  &providerKey,
			},
		},
	})
	if providerRegistry.DefaultProvider() != "anthropic" {
		t.Errorf("DefaultProvider() = %q, want %q", providerRegistry.DefaultProvider(), "anthropic")
	}
	if providerRegistry.DefaultProviderModelName() != defaultProviderModelName {
		t.Errorf("DefaultProviderModelName() = %q, want %q", providerRegistry.DefaultProviderModelName(), defaultProviderModelName)
	}
}

func TestRegistryRegisterAndResolve(t *testing.T) {
	providerRegistry := NewProviderRegistry(nil)
	openaiProvider := &mockProvider{name: "openai"}
	anthropicProvider := &mockProvider{name: "anthropic"}

	providerRegistry.Register("openai", openaiProvider)
	providerRegistry.Register("anthropic", anthropicProvider)

	// Resolve with explicit provider prefix.
	client, _, modelName, err := providerRegistry.ResolveProviderAndModel("anthropic:claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if modelName != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want %q", modelName, "claude-sonnet-4-20250514")
	}
	response, _ := client.ChatCompletion(context.Background(), ChatRequest{})
	if response.ModelName != "anthropic" {
		t.Errorf("resolved to wrong provider: %q", response.ModelName)
	}

	// Resolve without prefix should use default provider.
	client, _, modelName, err = providerRegistry.ResolveProviderAndModel("gpt-4o")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if modelName != "gpt-4o" {
		t.Errorf("model = %q, want %q", modelName, "gpt-4o")
	}
	response, _ = client.ChatCompletion(context.Background(), ChatRequest{})
	if response.ModelName != "openai" {
		t.Errorf("resolved to wrong provider: %q", response.ModelName)
	}
}

func TestRegistryResolveUnknownProvider(t *testing.T) {
	providerRegistry := NewProviderRegistry(nil)

	_, _, _, err := providerRegistry.ResolveProviderAndModel("unknown:some-model")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	expected := `unknown provider: "unknown"`
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

func TestRegistryResolveNoDefaultRegistered(t *testing.T) {
	defaultProviderModelName := "missing:model"
	providerRegistry := NewProviderRegistry(&models.ModelsConfiguration{
		Default: &defaultProviderModelName,
		Providers: &[]*models.ProviderConfiguration{
			// Empty providers list so nothing actually registers.
		},
	})

	_, _, _, err := providerRegistry.ResolveProviderAndModel("some-model")
	if err == nil {
		t.Fatal("expected error when default provider is not registered")
	}
}

func TestRegistryProviderNames(t *testing.T) {
	providerRegistry := NewEmptyProviderRegistry()
	providerRegistry.Register("openai", &mockProvider{})
	providerRegistry.Register("anthropic", &mockProvider{})
	providerRegistry.Register("local", &mockProvider{})

	names := providerRegistry.ProviderNames()
	sort.Strings(names)

	expected := []string{"anthropic", "local", "openai"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}
	for index, name := range names {
		if name != expected[index] {
			t.Errorf("names[%d] = %q, want %q", index, name, expected[index])
		}
	}
}

func TestParseProviderModelName(t *testing.T) {
	cases := []struct {
		input           string
		defaultProvider string
		wantProvider    string
		wantModel       string
	}{
		{"anthropic:claude-sonnet-4-20250514", "openai", "anthropic", "claude-sonnet-4-20250514"},
		{"gpt-4o", "openai", "openai", "gpt-4o"},
		{"openai:gpt-4o", "anthropic", "openai", "gpt-4o"},
		{"local:llama3:8b", "openai", "local", "llama3:8b"},
		{":empty-provider", "default", "", "empty-provider"},
		{"standalone", "", "", "standalone"},
	}
	for _, testCase := range cases {
		providerName, modelName := ParseProviderModelName(testCase.input, testCase.defaultProvider)
		if providerName != testCase.wantProvider {
			t.Errorf("ParseProviderModelName(%q, %q) provider = %q, want %q",
				testCase.input, testCase.defaultProvider, providerName, testCase.wantProvider)
		}
		if modelName != testCase.wantModel {
			t.Errorf("ParseProviderModelName(%q, %q) model = %q, want %q",
				testCase.input, testCase.defaultProvider, modelName, testCase.wantModel)
		}
	}
}

func TestFormatProviderModelName(t *testing.T) {
	cases := []struct {
		providerName string
		modelName    string
		expected     string
	}{
		{"anthropic", "claude-sonnet-4-20250514", "anthropic:claude-sonnet-4-20250514"},
		{"openai", "gpt-4o", "openai:gpt-4o"},
		{"", "model", ":model"},
	}
	for _, testCase := range cases {
		result := FormatProviderModelName(testCase.providerName, testCase.modelName)
		if result != testCase.expected {
			t.Errorf("FormatProviderModelName(%q, %q) = %q, want %q",
				testCase.providerName, testCase.modelName, result, testCase.expected)
		}
	}
}

// mockProviderWithModels extends mockProvider with configurable model lists.
type mockProviderWithModels struct {
	mockProvider
	modelList []ModelInformation
	callCount atomic.Int32
	err       error
}

func (self *mockProviderWithModels) ListModels(ctx context.Context) ([]ModelInformation, error) {
	self.callCount.Add(1)
	if self.err != nil {
		return nil, self.err
	}
	return self.modelList, nil
}

func TestListAllModelsBasicAggregation(t *testing.T) {
	providerRegistry := NewEmptyProviderRegistry()
	providerRegistry.Register("openai", &mockProviderWithModels{
		modelList: []ModelInformation{
			{ID: "gpt-4o", ContextLength: 128000},
			{ID: "gpt-5.2"},
		},
	})
	providerRegistry.Register("anthropic", &mockProviderWithModels{
		modelList: []ModelInformation{
			{ID: "claude-sonnet-4-20250514", ContextLength: 200000},
		},
	})

	results := providerRegistry.ListAllModels(context.Background())

	if len(results) != 3 {
		t.Fatalf("expected 3 models, got %d", len(results))
	}

	// Results should be ordered by provider name (alphabetical), then by ListModels order.
	// anthropic first, then openai.
	if results[0].ProviderName != "anthropic" || results[0].ModelName != "claude-sonnet-4-20250514" {
		t.Errorf("results[0] = %+v, want anthropic:claude-sonnet-4-20250514", results[0])
	}
	if results[0].ContextLength != 200000 {
		t.Errorf("results[0].ContextLength = %d, want 200000", results[0].ContextLength)
	}
	if results[1].ProviderName != "openai" || results[1].ModelName != "gpt-4o" {
		t.Errorf("results[1] = %+v, want openai:gpt-4o", results[1])
	}
	if results[2].ProviderName != "openai" || results[2].ModelName != "gpt-5.2" {
		t.Errorf("results[2] = %+v, want openai:gpt-5.2", results[2])
	}
}

func TestListAllModelsCaching(t *testing.T) {
	provider := &mockProviderWithModels{
		modelList: []ModelInformation{{ID: "gpt-4o"}},
	}
	providerRegistry := NewEmptyProviderRegistry()
	providerRegistry.Register("openai", provider)

	// First call should fetch.
	results := providerRegistry.ListAllModels(context.Background())
	if len(results) != 1 {
		t.Fatalf("expected 1 model, got %d", len(results))
	}
	if provider.callCount.Load() != 1 {
		t.Fatalf("expected 1 ListModels call, got %d", provider.callCount.Load())
	}

	// Second call should use cache.
	results = providerRegistry.ListAllModels(context.Background())
	if len(results) != 1 {
		t.Fatalf("expected 1 model on second call, got %d", len(results))
	}
	if provider.callCount.Load() != 1 {
		t.Errorf("expected ListModels still called once (cached), got %d", provider.callCount.Load())
	}
}

func TestListAllModelsCacheExpiry(t *testing.T) {
	provider := &mockProviderWithModels{
		modelList: []ModelInformation{{ID: "gpt-4o"}},
	}
	providerRegistry := NewEmptyProviderRegistry()
	providerRegistry.Register("openai", provider)

	// First call populates cache.
	providerRegistry.ListAllModels(context.Background())
	if provider.callCount.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", provider.callCount.Load())
	}

	// Expire the cache entry manually.
	providerRegistry.modelsCacheMutex.Lock()
	providerRegistry.modelsCache["openai"].expiresAt = time.Now().Add(-time.Second)
	providerRegistry.modelsCacheMutex.Unlock()

	// Next call should re-fetch.
	providerRegistry.ListAllModels(context.Background())
	if provider.callCount.Load() != 2 {
		t.Errorf("expected 2 calls after expiry, got %d", provider.callCount.Load())
	}
}

func TestListAllModelsProviderFailure(t *testing.T) {
	workingProvider := &mockProviderWithModels{
		modelList: []ModelInformation{{ID: "gpt-4o"}},
	}
	failingProvider := &mockProviderWithModels{
		err: fmt.Errorf("network error"),
	}
	providerRegistry := NewEmptyProviderRegistry()
	providerRegistry.Register("openai", workingProvider)
	providerRegistry.Register("broken", failingProvider)

	results := providerRegistry.ListAllModels(context.Background())

	// Should still get the working provider's models.
	if len(results) != 1 {
		t.Fatalf("expected 1 model, got %d: %+v", len(results), results)
	}
	if results[0].ProviderName != "openai" {
		t.Errorf("expected openai provider, got %q", results[0].ProviderName)
	}
}

func TestListAllModelsStaleOnFailure(t *testing.T) {
	provider := &mockProviderWithModels{
		modelList: []ModelInformation{{ID: "gpt-4o"}},
	}
	providerRegistry := NewEmptyProviderRegistry()
	providerRegistry.Register("openai", provider)

	// Populate cache.
	results := providerRegistry.ListAllModels(context.Background())
	if len(results) != 1 {
		t.Fatalf("expected 1 model, got %d", len(results))
	}

	// Expire cache and make provider fail.
	providerRegistry.modelsCacheMutex.Lock()
	providerRegistry.modelsCache["openai"].expiresAt = time.Now().Add(-time.Second)
	providerRegistry.modelsCacheMutex.Unlock()
	provider.err = fmt.Errorf("network error")

	// Should return stale data.
	results = providerRegistry.ListAllModels(context.Background())
	if len(results) != 1 {
		t.Fatalf("expected 1 stale model, got %d", len(results))
	}
	if results[0].ModelName != "gpt-4o" {
		t.Errorf("expected stale gpt-4o, got %q", results[0].ModelName)
	}
}

func TestListAllModelsEmptyRegistry(t *testing.T) {
	providerRegistry := NewEmptyProviderRegistry()
	results := providerRegistry.ListAllModels(context.Background())

	if results == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 models, got %d", len(results))
	}
}

func TestRegistryRegisterOverwrite(t *testing.T) {
	providerRegistry := NewProviderRegistry(nil)
	firstProvider := &mockProvider{name: "first"}
	secondProvider := &mockProvider{name: "second"}

	providerRegistry.Register("openai", firstProvider)
	providerRegistry.Register("openai", secondProvider)

	client, _, _, err := providerRegistry.ResolveProviderAndModel("openai:gpt-4o")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	response, _ := client.ChatCompletion(context.Background(), ChatRequest{})
	if response.ModelName != "second" {
		t.Errorf("expected second provider after overwrite, got %q", response.ModelName)
	}
}

func TestFindTranscriber_Deterministic(t *testing.T) {
	registry := NewRegistry("openai")
	registry.Register("openai", &mockProvider{name: "openai"})
	registry.Register("t1", &mockTranscriberProvider{mockProvider{name: "t1"}})
	registry.Register("t2", &mockTranscriberProvider{mockProvider{name: "t2"}})

	var firstName string
	for i := 0; i < 100; i++ {
		transcriber, name, ok := registry.FindTranscriber()
		if !ok || transcriber == nil {
			t.Fatalf("FindTranscriber returned no transcriber at iteration %d", i)
		}
		if i == 0 {
			firstName = name
		} else if name != firstName {
			t.Fatalf("FindTranscriber changed selection from %q to %q at iteration %d", firstName, name, i)
		}
	}
}

func TestFindStreamingTranscriber_Deterministic(t *testing.T) {
	registry := NewRegistry("openai")
	registry.Register("openai", &mockProvider{name: "openai"})
	registry.Register("stream-a", &mockStreamingTranscriberProvider{mockProvider{name: "stream-a"}})
	registry.Register("stream-b", &mockStreamingTranscriberProvider{mockProvider{name: "stream-b"}})

	var firstName string
	for i := 0; i < 100; i++ {
		transcriber, name, ok := registry.FindStreamingTranscriber()
		if !ok || transcriber == nil {
			t.Fatalf("FindStreamingTranscriber returned no transcriber at iteration %d", i)
		}
		if i == 0 {
			firstName = name
		} else if name != firstName {
			t.Fatalf("FindStreamingTranscriber changed selection from %q to %q at iteration %d", firstName, name, i)
		}
	}
}

func TestFindTranscriberByName_Found(t *testing.T) {
	registry := NewRegistry("openai")
	registry.Register("t1", &mockTranscriberProvider{mockProvider{name: "t1"}})

	transcriber, ok := registry.FindTranscriberByName("t1")
	if !ok || transcriber == nil {
		t.Fatalf("expected named transcriber lookup to succeed")
	}
}

func TestFindTranscriberByName_NotFound(t *testing.T) {
	registry := NewRegistry("openai")
	registry.Register("openai", &mockProvider{name: "openai"})

	transcriber, ok := registry.FindTranscriberByName("missing")
	if ok {
		t.Fatalf("expected lookup miss")
	}
	if transcriber != nil {
		t.Fatalf("expected nil transcriber on miss")
	}
}

func TestFindTranscriberByName_WrongCapability(t *testing.T) {
	registry := NewRegistry("openai")
	registry.Register("openai", &mockProvider{name: "openai"})

	transcriber, ok := registry.FindTranscriberByName("openai")
	if ok {
		t.Fatalf("expected capability mismatch to fail")
	}
	if transcriber != nil {
		t.Fatalf("expected nil transcriber for capability mismatch")
	}
}

func TestFindStreamingTranscriberByName_Found(t *testing.T) {
	registry := NewRegistry("openai")
	registry.Register("stream-a", &mockStreamingTranscriberProvider{mockProvider{name: "stream-a"}})

	transcriber, ok := registry.FindStreamingTranscriberByName("stream-a")
	if !ok || transcriber == nil {
		t.Fatalf("expected named streaming transcriber lookup to succeed")
	}
}

func TestFindStreamingTranscriberByName_NotFound(t *testing.T) {
	registry := NewRegistry("openai")
	registry.Register("openai", &mockProvider{name: "openai"})

	transcriber, ok := registry.FindStreamingTranscriberByName("missing")
	if ok || transcriber != nil {
		t.Fatalf("expected missing streaming transcriber lookup to fail")
	}
}

func TestFindStreamingTranscriberByName_WrongCapability(t *testing.T) {
	registry := NewRegistry("openai")
	registry.Register("openai", &mockProvider{name: "openai"})

	transcriber, ok := registry.FindStreamingTranscriberByName("openai")
	if ok || transcriber != nil {
		t.Fatalf("expected non-streaming provider lookup to fail")
	}
}
