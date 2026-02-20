package agents

import (
	"context"
	"sort"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/providers"
)

// Tool is something the LLM can invoke during a conversation.
type Tool interface {
	Definition() providers.ToolDefinition
	Execute(ctx context.Context, arguments string) (string, error)
}

// ToolRegistry holds named tools available to the agent.
type ToolRegistry struct {
	tools map[string]Tool
}

// NewToolRegistry creates an empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry.
func (self *ToolRegistry) Register(tool Tool) {
	self.tools[tool.Definition().Function.Name] = tool
}

// Get returns a tool by name, or nil.
func (self *ToolRegistry) Get(name string) Tool {
	return self.tools[name]
}

// Remove deletes a tool from the registry.
func (self *ToolRegistry) Remove(name string) {
	delete(self.tools, name)
}

// Names returns all tool names in the registry in sorted order.
func (self *ToolRegistry) Names() []string {
	names := make([]string, 0, len(self.tools))
	for name := range self.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ApplyFilter removes tools not present in the allow list.
// A nil or empty list means all tools are kept (preserving defaults).
// Only an explicitly populated list restricts the tool set.
func (self *ToolRegistry) ApplyFilter(allowed []string) {
	if len(allowed) == 0 {
		return
	}
	for name := range self.tools {
		if !configs.IsAllowed(name, allowed) {
			delete(self.tools, name)
		}
	}
}

// Definitions returns all tool definitions for the chat request in stable
// sorted order. Stable ordering is important for prompt caching: providers
// like Anthropic cache the request prefix, so tool definitions must appear
// in the same order across requests.
func (self *ToolRegistry) Definitions() []providers.ToolDefinition {
	names := self.Names() // already sorted
	definitions := make([]providers.ToolDefinition, 0, len(names))
	for _, name := range names {
		definitions = append(definitions, self.tools[name].Definition())
	}
	return definitions
}
