package agent

import (
	"sync"

	"github.com/teanode/teanode/internal/config"
	"github.com/teanode/teanode/internal/provider"
)

// AgentRegistry manages multiple named runners (one per agent).
type AgentRegistry struct {
	mutex          sync.RWMutex
	runners        map[string]*Runner // agentId → Runner
	defaultAgentID string             // resolved default agent ID
}

// NewAgentRegistry creates an empty agent registry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		runners: make(map[string]*Runner),
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
func (self *AgentRegistry) SetDefault(agentID string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.defaultAgentID = agentID
}

// DefaultID returns the configured default agent ID.
func (self *AgentRegistry) DefaultID() string {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.defaultAgentID
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
func (self *AgentRegistry) Reconfigure(agentId string, configuration *config.Config, providers *provider.Registry, tools *ToolRegistry, skillPrompts string) {
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
