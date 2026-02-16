package agents

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/provider"
	"github.com/teanode/teanode/internal/util/ulid"
	"gopkg.in/yaml.v3"
)

// persistedState is the YAML structure written to ~/.teanode/state.yaml.
type persistedState struct {
	ActiveAgentId         string            `yaml:"activeAgentId,omitempty"`
	ActiveConversationIds map[string]string `yaml:"activeConversationIds,omitempty"`
}

// AgentRegistry manages multiple named runners (one per agent).
type AgentRegistry struct {
	mutex                 sync.RWMutex
	runners               map[string]*Runner // agentId → Runner
	defaultAgentId        string             // resolved default agent ID
	activeAgentId         string             // system-wide active agent (falls back to defaultAgentId)
	activeConversationIds map[string]string   // agentId → active conversationId
}

// NewAgentRegistry creates an empty agent registry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		runners:               make(map[string]*Runner),
		activeConversationIds: make(map[string]string),
	}
}

// Register adds or replaces a runner for the given agent ID.
func (self *AgentRegistry) Register(agentId string, runner *Runner) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.runners[agentId] = runner
}

// Get returns the runner for the given agent ID, or nil if not found.
func (self *AgentRegistry) Get(agentId string) *Runner {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.runners[agentId]
}

// SetDefault sets the default agent ID.
func (self *AgentRegistry) SetDefault(agentId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.defaultAgentId = agentId
}

// DefaultID returns the configured default agent ID.
func (self *AgentRegistry) DefaultID() string {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.defaultAgentId
}

// Default returns the runner for the default agent.
func (self *AgentRegistry) Default() *Runner {
	return self.Get(self.DefaultID())
}

// AgentIDs returns a list of all registered agent IDs.
func (self *AgentRegistry) AgentIDs() []string {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	ids := make([]string, 0, len(self.runners))
	for agentId := range self.runners {
		ids = append(ids, agentId)
	}
	return ids
}

// Reconfigure hot-swaps a single agent's runner configuration.
func (self *AgentRegistry) Reconfigure(agentId string, configuration *configs.Config, providers *provider.Registry, tools *ToolRegistry, skillPrompts string) {
	runner := self.Get(agentId)
	if runner == nil {
		return
	}
	runner.Reconfigure(configuration, providers, tools, skillPrompts)
}

// ForEach iterates over all agents, calling fn for each one.
func (self *AgentRegistry) ForEach(fn func(agentId string, runner *Runner)) {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	for agentId, runner := range self.runners {
		fn(agentId, runner)
	}
}

// LoadState restores active agent and conversation state from ~/.teanode/state.yaml.
// Missing or malformed files are silently ignored (fresh start).
func (self *AgentRegistry) LoadState() {
	stateFile, err := configs.StateFile()
	if err != nil {
		return
	}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return
	}
	var state persistedState
	if err := yaml.Unmarshal(data, &state); err != nil {
		slog.Warn("ignoring malformed state file", "path", stateFile, "error", err)
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	// Only restore activeAgentId if the agent is registered.
	if state.ActiveAgentId != "" {
		if _, ok := self.runners[state.ActiveAgentId]; ok {
			self.activeAgentId = state.ActiveAgentId
		}
	}
	for agentId, conversationId := range state.ActiveConversationIds {
		if conversationId != "" {
			self.activeConversationIds[agentId] = conversationId
		}
	}
}

// saveState writes current active state to ~/.teanode/state.yaml.
// Must be called with mutex held (at least RLock).
func (self *AgentRegistry) saveState() {
	stateFile, err := configs.StateFile()
	if err != nil {
		return
	}
	state := persistedState{
		ActiveAgentId:         self.activeAgentId,
		ActiveConversationIds: make(map[string]string, len(self.activeConversationIds)),
	}
	for agentId, conversationId := range self.activeConversationIds {
		state.ActiveConversationIds[agentId] = conversationId
	}
	data, err := yaml.Marshal(state)
	if err != nil {
		slog.Warn("failed to marshal state", "error", err)
		return
	}
	if err := os.WriteFile(stateFile, data, 0644); err != nil {
		slog.Warn("failed to write state file", "path", stateFile, "error", err)
	}
}

// ActiveAgentID returns the system-wide active agent ID, falling back to the default.
func (self *AgentRegistry) ActiveAgentID() string {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	if self.activeAgentId != "" {
		return self.activeAgentId
	}
	return self.defaultAgentId
}

// SetActiveAgent sets the system-wide active agent. Returns an error if the agent doesn't exist.
func (self *AgentRegistry) SetActiveAgent(agentId string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if _, ok := self.runners[agentId]; !ok {
		return fmt.Errorf("agent not found: %s", agentId)
	}
	self.activeAgentId = agentId
	self.saveState()
	return nil
}

// ActiveConversationID returns the active conversation for the given agent.
// If none is set, it auto-generates a new ULID and stores it.
func (self *AgentRegistry) ActiveConversationID(agentId string) string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if conversationId, ok := self.activeConversationIds[agentId]; ok {
		return conversationId
	}
	conversationId := ulid.GenerateString()
	self.activeConversationIds[agentId] = conversationId
	self.saveState()
	return conversationId
}

// SetActiveConversation sets the active conversation for the given agent.
func (self *AgentRegistry) SetActiveConversation(agentId, conversationId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.activeConversationIds[agentId] = conversationId
	self.saveState()
}

// SetActiveConversationIfUnset sets the active conversation only if the agent has no active conversation.
// Returns true if the conversation was set.
func (self *AgentRegistry) SetActiveConversationIfUnset(agentId, conversationId string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if _, ok := self.activeConversationIds[agentId]; ok {
		return false
	}
	self.activeConversationIds[agentId] = conversationId
	self.saveState()
	return true
}

// NewConversation generates a new ULID, sets it as the active conversation for the agent, and returns it.
func (self *AgentRegistry) NewConversation(agentId string) string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	conversationId := ulid.GenerateString()
	self.activeConversationIds[agentId] = conversationId
	self.saveState()
	return conversationId
}
