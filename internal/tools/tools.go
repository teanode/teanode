// Package tools defines the builtin tool registry and shared tool interfaces.
package tools

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

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

// PolicyDecision is the outcome of a tool policy check.
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

// PolicyGroup defines an action group with its default policy level and the
// actions that belong to it. For single-group tools, Actions may be nil.
// When resolving, unmatched actions fall into the last group in the slice.
type PolicyGroup struct {
	Group   models.ToolPolicyGroup
	Default models.ToolPolicyLevel
	Actions []string
}

// Tool is something the LLM can invoke during a conversation.
type Tool interface {
	Definition() providers.ToolDefinition
	Execute(ctx context.Context, arguments string) (string, error)
	// PolicyGroups declares the tool's action groups, their default policy
	// levels, and which actions belong to each group. The runner uses this
	// to resolve access control for each tool call.
	PolicyGroups() []PolicyGroup
}

// ArgumentPolicyProvider is implemented by tools that need argument-specific
// policy decisions in addition to their configured/default tool policy.
// Deny decisions are non-bypassable. Approval decisions escalate an otherwise
// allowed tool call to explicit user approval.
type ArgumentPolicyProvider interface {
	ArgumentPolicy(ctx context.Context, arguments string) PolicyDecision
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

// toolPoliciesKey is the context key for configured tool policies.
type toolPoliciesKey struct{}

// ContextWithToolPolicies returns a context carrying the configured tool policies.
func ContextWithToolPolicies(ctx context.Context, policies []*models.ToolPolicyConfiguration) context.Context {
	return context.WithValue(ctx, toolPoliciesKey{}, policies)
}

// ToolPoliciesFromContext returns the configured tool policies, or nil.
func ToolPoliciesFromContext(ctx context.Context) []*models.ToolPolicyConfiguration {
	policies, _ := ctx.Value(toolPoliciesKey{}).([]*models.ToolPolicyConfiguration)
	return policies
}

// ResolveToolPolicy resolves the effective policy for a tool call by:
//  1. Parsing the action from arguments
//  2. Matching the action to a policy group via PolicyGroups()
//  3. Checking for a configured override in the context
//  4. Falling back to the group's declared default level
func ResolveToolPolicy(ctx context.Context, tool Tool, toolName string, arguments string) PolicyDecision {
	argumentDecision := AllowPolicy()
	if provider, ok := tool.(ArgumentPolicyProvider); ok {
		argumentDecision = provider.ArgumentPolicy(ctx, arguments)
		if argumentDecision.Action == PolicyDeny {
			return argumentDecision
		}
	}

	groups := tool.PolicyGroups()
	action := ParseAction(arguments)
	group := findGroupForAction(groups, action)
	baseDecision := applyPolicyLevel(ctx, group.Default, toolName, string(group.Group))
	if decision, ok := resolveConfiguredPolicy(ctx, toolName, group.Group); ok {
		baseDecision = decision
	}
	if baseDecision.Action != PolicyAllow {
		return baseDecision
	}
	if argumentDecision.Action == PolicyRequireApproval {
		return argumentDecision
	}
	return baseDecision
}

// findGroupForAction returns the PolicyGroup matching the given action.
// If no group's Actions list contains the action, the last group is returned
// as a catch-all.
func findGroupForAction(groups []PolicyGroup, action string) PolicyGroup {
	for _, group := range groups {
		for _, groupAction := range group.Actions {
			if groupAction == action {
				return group
			}
		}
	}
	return groups[len(groups)-1]
}

// resolveConfiguredPolicy checks the context for a configured policy matching
// the given tool name and action group.
func resolveConfiguredPolicy(ctx context.Context, toolName string, group models.ToolPolicyGroup) (PolicyDecision, bool) {
	policies := ToolPoliciesFromContext(ctx)
	if len(policies) == 0 {
		return PolicyDecision{}, false
	}
	var wildcardEntry *models.ToolPolicyConfiguration
	for _, entry := range policies {
		if entry.GetTool() != toolName {
			continue
		}
		entryGroup := entry.GetGroup()
		if entryGroup == group {
			return applyPolicyLevel(ctx, entry.GetLevel(), toolName, string(group)), true
		}
		if entryGroup == models.ToolPolicyGroupAll && wildcardEntry == nil {
			wildcardEntry = entry
		}
	}
	if wildcardEntry != nil {
		return applyPolicyLevel(ctx, wildcardEntry.GetLevel(), toolName, string(group)), true
	}
	return PolicyDecision{}, false
}

// applyPolicyLevel maps a ToolPolicyLevel + admin status to a PolicyDecision.
func applyPolicyLevel(ctx context.Context, level models.ToolPolicyLevel, toolName, group string) PolicyDecision {
	label := toolName
	if group != "*" {
		label = toolName + "." + group
	}
	isAdmin := IsAdmin(ctx)
	switch level {
	case models.ToolPolicyDisabled:
		return DenyPolicy(label + " is disabled by policy")
	case models.ToolPolicyAdminApproval:
		if !isAdmin {
			return DenyPolicy("admin access required for " + label)
		}
		return ApprovalPolicy(label+" requires approval", "high")
	case models.ToolPolicyAdminOnly:
		if !isAdmin {
			return DenyPolicy("admin access required for " + label)
		}
		return AllowPolicy()
	case models.ToolPolicyAnyoneApproval:
		return ApprovalPolicy(label+" requires approval", "medium")
	case models.ToolPolicyAnyone:
		return AllowPolicy()
	default:
		return DenyPolicy("unknown policy level for " + label)
	}
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
//
// A registry belongs to a single run. Tools may be deferred: a deferred tool
// stays callable but its full definition is withheld from the chat request
// until something activates it, which keeps the request small when the model
// has a narrow context window. Deferred tools are advertised to the model as
// one-line catalog entries instead (see DeferredCatalog).
type ToolRegistry struct {
	tools     map[string]Tool
	deferred  map[string]bool
	activated map[string]bool
	mutex     sync.Mutex
}

// NewToolRegistry creates a registry pre-populated with all builtin tools
// registered via RegisterBuiltinTool.
func NewToolRegistry() *ToolRegistry {
	registry := NewEmptyToolRegistry()
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
	return &ToolRegistry{
		tools:     make(map[string]Tool),
		deferred:  make(map[string]bool),
		activated: make(map[string]bool),
	}
}

// Register adds a tool to the registry.
func (self *ToolRegistry) Register(tool Tool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.tools[tool.Definition().Function.Name] = tool
}

// Get returns a tool by name, or nil.
func (self *ToolRegistry) Get(name string) Tool {
	if self == nil {
		return nil
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.tools[name]
}

// Remove deletes a tool from the registry.
func (self *ToolRegistry) Remove(name string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.removeLocked(name)
}

// removeLocked deletes a tool. The caller holds the mutex.
func (self *ToolRegistry) removeLocked(name string) {
	delete(self.tools, name)
	delete(self.deferred, name)
	delete(self.activated, name)
}

// Names returns all tool names in the registry in sorted order.
func (self *ToolRegistry) Names() []string {
	if self == nil {
		return nil
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.namesLocked()
}

// namesLocked returns all tool names in sorted order. The caller holds the
// mutex.
func (self *ToolRegistry) namesLocked() []string {
	names := make([]string, 0, len(self.tools))
	for name := range self.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// isLoadedLocked reports whether a tool's definition goes into the request.
// An unregistered name is not loaded, so a caller that checks this before
// resolving the tool cannot mistake an unknown name for an available one.
// The caller holds the mutex.
func (self *ToolRegistry) isLoadedLocked(name string) bool {
	if _, registered := self.tools[name]; !registered {
		return false
	}
	return !self.deferred[name] || self.activated[name]
}

// ApplyFilter removes tools not present in the allow list.
// A nil or empty list means all tools are kept (preserving defaults).
// Only an explicitly populated list restricts the tool set.
func (self *ToolRegistry) ApplyFilter(allowed []string) {
	if len(allowed) == 0 {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for name := range self.tools {
		if !allowlist.IsAllowed(name, allowed) {
			self.removeLocked(name)
		}
	}
}

// Defer withholds the full definitions of every tool except the given ones.
// Deferred tools stay callable and stay listed in DeferredCatalog, but they
// are left out of Definitions until Activate names them. Calling Defer resets
// any previous deferral and activation state.
func (self *ToolRegistry) Defer(keepLoaded []string) {
	loaded := make(map[string]bool, len(keepLoaded))
	for _, name := range keepLoaded {
		loaded[name] = true
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.deferred = make(map[string]bool, len(self.tools))
	self.activated = make(map[string]bool)
	for name := range self.tools {
		if !loaded[name] {
			self.deferred[name] = true
		}
	}
}

// Activate loads the full definitions of the named deferred tools so that
// subsequent Definitions calls include them. Names that are unknown or not
// deferred are ignored. It returns the names that were newly activated.
func (self *ToolRegistry) Activate(names []string) []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	activated := make([]string, 0, len(names))
	for _, name := range names {
		if !self.deferred[name] || self.activated[name] {
			continue
		}
		self.activated[name] = true
		activated = append(activated, name)
	}
	sort.Strings(activated)
	return activated
}

// CatalogEntry is a deferred tool advertised to the model by name and summary.
type CatalogEntry struct {
	Name    string
	Summary string
}

// DeferredCatalog returns the still-deferred tools in sorted order, each with
// a one-line summary derived from its description. Activated tools are left
// out because their full definitions are already in the request.
func (self *ToolRegistry) DeferredCatalog() []CatalogEntry {
	if self == nil {
		return nil
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	names := make([]string, 0, len(self.deferred))
	for name := range self.deferred {
		if !self.activated[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	entries := make([]CatalogEntry, 0, len(names))
	for _, name := range names {
		tool := self.tools[name]
		if tool == nil {
			continue
		}
		entries = append(entries, CatalogEntry{
			Name:    name,
			Summary: summarizeDescription(tool.Definition().Function.Description),
		})
	}
	return entries
}

// maxCatalogSummaryCharacters caps a catalog summary so that a large catalog
// stays far cheaper than the definitions it stands in for.
const maxCatalogSummaryCharacters = 110

// summarizeDescription reduces a tool description to a single short line: the
// first sentence, truncated at a word boundary.
func summarizeDescription(description string) string {
	summary := strings.TrimSpace(strings.ReplaceAll(description, "\n", " "))
	if index := strings.Index(summary, ". "); index >= 0 {
		summary = summary[:index+1]
	}
	if len(summary) <= maxCatalogSummaryCharacters {
		return summary
	}
	summary = summary[:maxCatalogSummaryCharacters]
	if index := strings.LastIndex(summary, " "); index > 0 {
		summary = summary[:index]
	}
	return strings.TrimRight(summary, " ,;:") + "..."
}

// BuildOverlays calls BuildOverlay on every registered tool that implements
// OverlayBuilder, returning results in stable tool-name-sorted order.
// Errors are silently skipped (best-effort).
func (self *ToolRegistry) BuildOverlays(ctx context.Context) []string {
	if self == nil {
		return nil
	}
	// Snapshot the builders before calling any of them: BuildOverlay reads
	// from the store, so holding the registry lock across it would block the
	// runner for the duration of that I/O.
	self.mutex.Lock()
	builders := make([]OverlayBuilder, 0, len(self.tools))
	for _, name := range self.namesLocked() {
		if builder, ok := self.tools[name].(OverlayBuilder); ok {
			builders = append(builders, builder)
		}
	}
	self.mutex.Unlock()

	var overlays []string
	for _, builder := range builders {
		overlay, err := builder.BuildOverlay(ctx)
		if err != nil || overlay == "" {
			continue
		}
		overlays = append(overlays, overlay)
	}
	return overlays
}

// ToolPolicyGroupInfo holds group and default policy for the settings UI.
type ToolPolicyGroupInfo struct {
	Group   models.ToolPolicyGroup
	Default models.ToolPolicyLevel
}

// ToolActionGroups returns a map of tool name -> policy group info for the
// settings UI, derived from each tool's PolicyGroups() declaration.
func (self *ToolRegistry) ToolActionGroups() map[string][]ToolPolicyGroupInfo {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	result := make(map[string][]ToolPolicyGroupInfo, len(self.tools))
	for name, tool := range self.tools {
		groups := tool.PolicyGroups()
		infos := make([]ToolPolicyGroupInfo, 0, len(groups))
		for _, group := range groups {
			infos = append(infos, ToolPolicyGroupInfo{
				Group:   group.Group,
				Default: group.Default,
			})
		}
		result[name] = infos
	}
	return result
}

// IsLoaded reports whether a tool's full definition went into the chat
// request, meaning the model has seen its parameter schema. A deferred tool
// that nothing has activated returns false: it is registered and known, but
// the model was only shown its one-line catalog entry.
func (self *ToolRegistry) IsLoaded(name string) bool {
	if self == nil {
		return false
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.isLoadedLocked(name)
}

// LoadedNames returns the sorted names of the tools whose full definitions go
// into the chat request, that is every tool except the deferred ones that have
// not been activated.
func (self *ToolRegistry) LoadedNames() []string {
	if self == nil {
		return nil
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	names := self.namesLocked()
	loaded := make([]string, 0, len(names))
	for _, name := range names {
		if !self.isLoadedLocked(name) {
			continue
		}
		loaded = append(loaded, name)
	}
	return loaded
}

// Definitions returns the tool definitions for the chat request in stable
// sorted order, leaving out deferred tools that have not been activated.
// Stable ordering is important for prompt caching: providers like Anthropic
// cache the request prefix, so tool definitions must appear in the same order
// across requests.
func (self *ToolRegistry) Definitions() []providers.ToolDefinition {
	if self == nil {
		return nil
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	names := self.namesLocked()
	definitions := make([]providers.ToolDefinition, 0, len(names))
	for _, name := range names {
		if !self.isLoadedLocked(name) {
			continue
		}
		definitions = append(definitions, self.tools[name].Definition())
	}
	return definitions
}
