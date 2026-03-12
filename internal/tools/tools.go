// Package tools defines the builtin tool registry and shared tool interfaces.
package tools

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/allowlist"
)

// PolicyAction describes what the runner should do with a tool call.
type PolicyAction string

const (
	// PolicyAllow lets the tool execute immediately.
	PolicyAllow PolicyAction = "allow"
	// PolicyDeny blocks execution and returns the reason as the tool result.
	PolicyDeny PolicyAction = "deny"
	// PolicyRequireApproval pauses execution until a user approves or rejects.
	PolicyRequireApproval PolicyAction = "require_approval"
)

// PolicyDecision is the outcome of a tool's Policy check.
type PolicyDecision struct {
	Action PolicyAction
	Reason string // human-readable explanation shown to the user / LLM
	Risk   string // optional risk label (e.g. "high", "medium")
}

// AllowPolicy returns a PolicyDecision that allows execution unconditionally.
func AllowPolicy() PolicyDecision {
	return PolicyDecision{Action: PolicyAllow}
}

// DenyPolicy returns a PolicyDecision that blocks execution with a reason.
func DenyPolicy(reason string) PolicyDecision {
	return PolicyDecision{Action: PolicyDeny, Reason: reason}
}

// ApprovalPolicy returns a PolicyDecision that requires user approval.
func ApprovalPolicy(reason, risk string) PolicyDecision {
	return PolicyDecision{Action: PolicyRequireApproval, Reason: reason, Risk: risk}
}

// Tool is something the LLM can invoke during a conversation.
type Tool interface {
	Definition() providers.ToolDefinition
	Execute(ctx context.Context, arguments string) (string, error)
	// Policy decides whether a tool call should be allowed, denied, or require
	// approval. The runner calls Policy before Execute.
	Policy(ctx context.Context, arguments string) PolicyDecision
}

// OverlayBuilder is an optional interface that tools can implement to
// inject late system messages into the LLM prompt. The runner calls
// BuildOverlay after constructing the conversation history. Return "" to
// contribute nothing.
type OverlayBuilder interface {
	BuildOverlay(ctx context.Context) (string, error)
}

// ParseAction extracts the "action" field from JSON tool arguments.
// Returns the lowercased action string, or "" if parsing fails.
func ParseAction(arguments string) string {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return ""
	}
	action, _ := payload["action"].(string)
	return strings.ToLower(action)
}

// IsAdmin returns true if the context user has admin privileges.
func IsAdmin(ctx context.Context) bool {
	user := models.UserFromContext(ctx)
	return user != nil && user.GetAdmin()
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
