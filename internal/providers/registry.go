package providers

import (
	"fmt"
	"strings"
)

// Registry holds named provider clients and resolves qualified model IDs.
type Registry struct {
	clients         map[string]Provider
	defaultProvider string
	transcriberOrder []string
	synthesizerOrder []string
}

// NewRegistry creates a provider registry with the given default provider name.
func NewRegistry(defaultProvider string) *Registry {
	return &Registry{
		clients:         make(map[string]Provider),
		defaultProvider: defaultProvider,
		transcriberOrder: make([]string, 0),
		synthesizerOrder: make([]string, 0),
	}
}

// Register adds a named provider client.
func (r *Registry) Register(name string, client Provider) {
	_, existed := r.clients[name]
	r.clients[name] = client
	if !existed {
		if _, ok := client.(AudioTranscriber); ok {
			r.transcriberOrder = append(r.transcriberOrder, name)
		}
		if _, ok := client.(AudioSynthesizer); ok {
			r.synthesizerOrder = append(r.synthesizerOrder, name)
		}
	}
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
	for _, name := range r.transcriberOrder {
		client, exists := r.clients[name]
		if !exists {
			continue
		}
		if transcriber, ok := client.(AudioTranscriber); ok {
			return transcriber, name, true
		}
	}
	return nil, "", false
}

// FindSynthesizer returns the first registered provider that implements AudioSynthesizer.
func (r *Registry) FindSynthesizer() (AudioSynthesizer, string, bool) {
	for _, name := range r.synthesizerOrder {
		client, exists := r.clients[name]
		if !exists {
			continue
		}
		if synthesizer, ok := client.(AudioSynthesizer); ok {
			return synthesizer, name, true
		}
	}
	return nil, "", false
}

// FindTranscriberByName resolves a named provider and returns it only when the
// provider supports AudioTranscriber.
func (r *Registry) FindTranscriberByName(name string) (AudioTranscriber, bool) {
	client, ok := r.clients[name]
	if !ok {
		return nil, false
	}
	transcriber, ok := client.(AudioTranscriber)
	if !ok {
		return nil, false
	}
	return transcriber, true
}

// FindSynthesizerByName resolves a named provider and returns it only when the
// provider supports AudioSynthesizer.
func (r *Registry) FindSynthesizerByName(name string) (AudioSynthesizer, bool) {
	client, ok := r.clients[name]
	if !ok {
		return nil, false
	}
	synthesizer, ok := client.(AudioSynthesizer)
	if !ok {
		return nil, false
	}
	return synthesizer, true
}
