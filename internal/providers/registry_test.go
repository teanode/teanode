package providers

import (
	"context"
	"sort"
	"testing"
)

// mockProvider implements Provider for testing the registry.
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

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry("openai")
	if registry.DefaultProvider() != "openai" {
		t.Errorf("DefaultProvider() = %q, want %q", registry.DefaultProvider(), "openai")
	}
	if len(registry.ProviderNames()) != 0 {
		t.Errorf("expected empty provider names, got %v", registry.ProviderNames())
	}
}

func TestRegistryRegisterAndResolve(t *testing.T) {
	registry := NewRegistry("openai")
	openaiProvider := &mockProvider{name: "openai"}
	anthropicProvider := &mockProvider{name: "anthropic"}

	registry.Register("openai", openaiProvider)
	registry.Register("anthropic", anthropicProvider)

	// Resolve with explicit provider prefix.
	client, model, err := registry.Resolve("anthropic:claude-sonnet-4-20250514")
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
	client, model, err = registry.Resolve("gpt-4o")
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
	registry := NewRegistry("openai")
	registry.Register("openai", &mockProvider{name: "openai"})

	_, _, err := registry.Resolve("unknown:some-model")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	expected := `unknown provider: "unknown"`
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

func TestRegistryResolveNoDefaultRegistered(t *testing.T) {
	registry := NewRegistry("missing")

	_, _, err := registry.Resolve("some-model")
	if err == nil {
		t.Fatal("expected error when default provider is not registered")
	}
}

func TestRegistryProviderNames(t *testing.T) {
	registry := NewRegistry("openai")
	registry.Register("openai", &mockProvider{})
	registry.Register("anthropic", &mockProvider{})
	registry.Register("local", &mockProvider{})

	names := registry.ProviderNames()
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
	registry := NewRegistry("openai")
	firstProvider := &mockProvider{name: "first"}
	secondProvider := &mockProvider{name: "second"}

	registry.Register("openai", firstProvider)
	registry.Register("openai", secondProvider)

	client, _, err := registry.Resolve("openai:gpt-4o")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	response, _ := client.ChatCompletion(context.Background(), ChatRequest{})
	if response.Model != "second" {
		t.Errorf("expected second provider after overwrite, got %q", response.Model)
	}
}
