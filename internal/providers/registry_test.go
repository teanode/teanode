package providers

import (
	"context"
	"io"
	"sort"
	"strings"
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
