// Package tools defines the builtin tool registry and shared tool interfaces.
package tools

import (
	"context"
	"sort"

	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/allowlist"
)

// Tool is something the LLM can invoke during a conversation.
type Tool interface {
	Definition() providers.ToolDefinition
	Execute(ctx context.Context, arguments string) (string, error)
}

// OverlayBuilder is an optional interface that tools can implement to
// inject late system messages into the LLM prompt. The runner calls
// BuildOverlay after constructing the conversation history. Return "" to
// contribute nothing.
type OverlayBuilder interface {
	BuildOverlay(ctx context.Context) (string, error)
}

// builtinRegistry holds factory functions registered by tool packages via init().
var builtinRegistry []func() []Tool

// RegisterBuiltinTool registers a factory that produces tools at registry
// creation time. Tool packages call this from init() so that importing the
// package is sufficient to make the tools available.
func RegisterBuiltinTool(factory func() []Tool) {
	builtinRegistry = append(builtinRegistry, factory)
}

// ToolRegistry holds named tools available to the agent.
type ToolRegistry struct {
	tools map[string]Tool
}

// NewToolRegistry creates a registry pre-populated with all builtin tools
// registered via RegisterBuiltinTool.
func NewToolRegistry() *ToolRegistry {
	registry := &ToolRegistry{tools: make(map[string]Tool)}
	for _, factory := range builtinRegistry {
		for _, tool := range factory() {
			registry.Register(tool)
		}
	}
	return registry
}

// NewEmptyToolRegistry creates a registry with no tools. Use this in tests
// that need an isolated registry without builtin tools.
func NewEmptyToolRegistry() *ToolRegistry {
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
		if !allowlist.IsAllowed(name, allowed) {
			delete(self.tools, name)
		}
	}
}

// BuildOverlays calls BuildOverlay on every registered tool that implements
// OverlayBuilder, returning results in stable tool-name-sorted order.
// Errors are silently skipped (best-effort).
func (self *ToolRegistry) BuildOverlays(ctx context.Context) []string {
	if self == nil {
		return nil
	}
	var overlays []string
	for _, name := range self.Names() {
		builder, ok := self.tools[name].(OverlayBuilder)
		if !ok {
			continue
		}
		overlay, err := builder.BuildOverlay(ctx)
		if err != nil || overlay == "" {
			continue
		}
		overlays = append(overlays, overlay)
	}
	return overlays
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
