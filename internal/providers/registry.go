package providers

import (
	"fmt"
	"strings"
)

// Registry holds named provider clients and resolves qualified model IDs.
type Registry struct {
	clients         map[string]Provider
	defaultProvider string
}

// NewRegistry creates a provider registry with the given default provider name.
func NewRegistry(defaultProvider string) *Registry {
	return &Registry{
		clients:         make(map[string]Provider),
		defaultProvider: defaultProvider,
	}
}

// Register adds a named provider client.
func (r *Registry) Register(name string, client Provider) {
	r.clients[name] = client
}

// Resolve splits a qualified model ID ("provider:model") and returns the
// corresponding client and bare model name. If the model has no provider
// prefix, the default provider is used.
func (r *Registry) Resolve(qualifiedModel string) (client Provider, bareModel string, err error) {
	providerName, model := ParseQualifiedModel(qualifiedModel, r.defaultProvider)
	client, ok := r.clients[providerName]
	if !ok {
		return nil, "", fmt.Errorf("unknown provider: %q", providerName)
	}
	return client, model, nil
}

// ProviderNames returns sorted provider names.
func (r *Registry) ProviderNames() []string {
	names := make([]string, 0, len(r.clients))
	for name := range r.clients {
		names = append(names, name)
	}
	return names
}

// DefaultProvider returns the default provider name.
func (r *Registry) DefaultProvider() string {
	return r.defaultProvider
}

// ParseQualifiedModel splits "provider:model" on the first colon.
// If there is no colon, defaultProvider is used.
func ParseQualifiedModel(qualified, defaultProvider string) (providerName, model string) {
	if idx := strings.IndexByte(qualified, ':'); idx >= 0 {
		return qualified[:idx], qualified[idx+1:]
	}
	return defaultProvider, qualified
}

// QualifyModel joins a provider name and model into "provider:model".
func QualifyModel(providerName, model string) string {
	return providerName + ":" + model
}

// FindTranscriber returns the first registered provider that implements AudioTranscriber.
func (r *Registry) FindTranscriber() (AudioTranscriber, string, bool) {
	for name, client := range r.clients {
		if transcriber, ok := client.(AudioTranscriber); ok {
			return transcriber, name, true
		}
	}
	return nil, "", false
}

// FindSynthesizer returns the first registered provider that implements AudioSynthesizer.
func (r *Registry) FindSynthesizer() (AudioSynthesizer, string, bool) {
	for name, client := range r.clients {
		if synthesizer, ok := client.(AudioSynthesizer); ok {
			return synthesizer, name, true
		}
	}
	return nil, "", false
}
