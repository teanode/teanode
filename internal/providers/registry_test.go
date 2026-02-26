package providers

import (
	"context"
	"sort"
	"testing"

	"github.com/teanode/teanode/internal/models"
)

// mockProvider implements Provider for testing the providerRegistry.
type mockProvider struct {
	name string
}

func (self *mockProvider) ChatCompletion(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{Model: self.name}, nil
}

func (self *mockProvider) ChatCompletionStream(ctx context.Context, request ChatRequest) (<-chan StreamEvent, error) {
	return nil, nil
}

func (self *mockProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return nil, nil
}

func TestNewRegistryNilConfig(t *testing.T) {
	providerRegistry := NewProviderRegistry(nil)
	if providerRegistry.DefaultProvider() != "openai" {
		t.Errorf("DefaultProvider() = %q, want %q", providerRegistry.DefaultProvider(), "openai")
	}
	if providerRegistry.DefaultModel() != "openai:gpt-5.2" {
		t.Errorf("DefaultModel() = %q, want %q", providerRegistry.DefaultModel(), "openai:gpt-5.2")
	}
	// Should have registered the default openai provider.
	if len(providerRegistry.ProviderNames()) != 1 {
		t.Errorf("expected 1 provider, got %v", providerRegistry.ProviderNames())
	}
}

func TestNewRegistryWithConfig(t *testing.T) {
	defaultModel := "anthropic:claude-sonnet-4-20250514"
	providerName := "anthropic"
	providerBaseURL := "https://api.anthropic.com"
	providerKey := "test-key"
	providerRegistry := NewProviderRegistry(&models.ModelsConfiguration{
		Default: &defaultModel,
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
	if providerRegistry.DefaultModel() != defaultModel {
		t.Errorf("DefaultModel() = %q, want %q", providerRegistry.DefaultModel(), defaultModel)
	}
}

func TestRegistryRegisterAndResolve(t *testing.T) {
	providerRegistry := NewProviderRegistry(nil)
	openaiProvider := &mockProvider{name: "openai"}
	anthropicProvider := &mockProvider{name: "anthropic"}

	providerRegistry.Register("openai", openaiProvider)
	providerRegistry.Register("anthropic", anthropicProvider)

	// Resolve with explicit provider prefix.
	client, _, model, err := providerRegistry.ResolveProviderAndModel("anthropic:claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if model != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want %q", model, "claude-sonnet-4-20250514")
	}
	response, _ := client.ChatCompletion(context.Background(), ChatRequest{})
	if response.Model != "anthropic" {
		t.Errorf("resolved to wrong provider: %q", response.Model)
	}

	// Resolve without prefix should use default provider.
	client, _, model, err = providerRegistry.ResolveProviderAndModel("gpt-4o")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if model != "gpt-4o" {
		t.Errorf("model = %q, want %q", model, "gpt-4o")
	}
	response, _ = client.ChatCompletion(context.Background(), ChatRequest{})
	if response.Model != "openai" {
		t.Errorf("resolved to wrong provider: %q", response.Model)
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
	defaultModel := "missing:model"
	providerRegistry := NewProviderRegistry(&models.ModelsConfiguration{
		Default:   &defaultModel,
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
	providerRegistry := NewProviderRegistry(nil)
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

func TestParseQualifiedModel(t *testing.T) {
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
		providerName, model := ParseQualifiedModel(testCase.input, testCase.defaultProvider)
		if providerName != testCase.wantProvider {
			t.Errorf("ParseQualifiedModel(%q, %q) provider = %q, want %q",
				testCase.input, testCase.defaultProvider, providerName, testCase.wantProvider)
		}
		if model != testCase.wantModel {
			t.Errorf("ParseQualifiedModel(%q, %q) model = %q, want %q",
				testCase.input, testCase.defaultProvider, model, testCase.wantModel)
		}
	}
}

func TestQualifyModel(t *testing.T) {
	cases := []struct {
		provider string
		model    string
		expected string
	}{
		{"anthropic", "claude-sonnet-4-20250514", "anthropic:claude-sonnet-4-20250514"},
		{"openai", "gpt-4o", "openai:gpt-4o"},
		{"", "model", ":model"},
	}
	for _, testCase := range cases {
		result := QualifyModel(testCase.provider, testCase.model)
		if result != testCase.expected {
			t.Errorf("QualifyModel(%q, %q) = %q, want %q",
				testCase.provider, testCase.model, result, testCase.expected)
		}
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
	if response.Model != "second" {
		t.Errorf("expected second provider after overwrite, got %q", response.Model)
	}
}
