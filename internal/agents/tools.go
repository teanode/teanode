package agents

import (
	"context"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/provider"
)

// Tool is something the LLM can invoke during a conversation.
type Tool interface {
	Definition() provider.ToolDefinition
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

// Names returns all tool names in the registry.
func (self *ToolRegistry) Names() []string {
	names := make([]string, 0, len(self.tools))
	for name := range self.tools {
		names = append(names, name)
	}
	return names
}

// ApplyFilter removes tools not present in the allow list.
// A nil list means all tools are kept.
func (self *ToolRegistry) ApplyFilter(allowed []string) {
	if allowed == nil {
		return
	}
	for name := range self.tools {
		if !configs.IsAllowed(name, allowed) {
			delete(self.tools, name)
		}
	}
}

// Definitions returns all tool definitions for the chat request.
func (self *ToolRegistry) Definitions() []provider.ToolDefinition {
	definitions := make([]provider.ToolDefinition, 0, len(self.tools))
	for _, tool := range self.tools {
		definitions = append(definitions, tool.Definition())
	}
	return definitions
}
