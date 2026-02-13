package agent

import (
	"context"

	"github.com/ziyan/teanode/internal/provider"
)

// Tool is something the LLM can invoke during a conversation.
type Tool interface {
	Definition() provider.ToolDef
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

// Definitions returns all tool definitions for the chat request.
func (self *ToolRegistry) Definitions() []provider.ToolDef {
	definitions := make([]provider.ToolDef, 0, len(self.tools))
	for _, tool := range self.tools {
		definitions = append(definitions, tool.Definition())
	}
	return definitions
}
