package agents

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/security"
	"gopkg.in/yaml.v3"
)

// persistedState is the YAML structure written to ~/.teanode/state.yaml.
type persistedState struct {
	DefaultAgentId         string            `yaml:"defaultAgentId,omitempty"`
	DefaultConversationIds map[string]string `yaml:"defaultConversationIds,omitempty"`
	DiscordChannelId       string            `yaml:"discordChannelId,omitempty"`
	TelegramChatId         int64             `yaml:"telegramChatId,omitempty"`
}

// AgentRegistry manages multiple named runners (one per agent).
type AgentRegistry struct {
	mutex                  sync.RWMutex
	runners                map[string]*Runner // agentId → Runner
	defaultAgentId         string             // resolved default agent ID
	defaultConversationIds map[string]string  // agentId → default conversationId
	discordChannelId       string
	telegramChatId         int64
}

// NewAgentRegistry creates an empty agent registry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		runners:                make(map[string]*Runner),
		defaultConversationIds: make(map[string]string),
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
func (self *AgentRegistry) Reconfigure(agentId string, configuration *configs.Config, providerRegistry *providers.Registry, tools *ToolRegistry, skillPrompts string) {
	runner := self.Get(agentId)
	if runner == nil {
		return
	}
	runner.Reconfigure(configuration, providerRegistry, tools, skillPrompts)
}

// ForEach iterates over all agents, calling fn for each one.
func (self *AgentRegistry) ForEach(fn func(agentId string, runner *Runner)) {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	for agentId, runner := range self.runners {
		fn(agentId, runner)
	}
}

// LoadState restores default agent and conversation state from ~/.teanode/state.yaml.
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
	// Only restore defaultAgentId if the agent is registered.
	if state.DefaultAgentId != "" {
		if _, ok := self.runners[state.DefaultAgentId]; ok {
			self.defaultAgentId = state.DefaultAgentId
		}
	}
	for agentId, conversationId := range state.DefaultConversationIds {
		if conversationId != "" {
			self.defaultConversationIds[agentId] = conversationId
		}
	}
	self.discordChannelId = state.DiscordChannelId
	self.telegramChatId = state.TelegramChatId
}

// saveState writes current default state to ~/.teanode/state.yaml.
// Must be called with mutex held (at least RLock).
func (self *AgentRegistry) saveState() {
	stateFile, err := configs.StateFile()
	if err != nil {
		return
	}
	state := persistedState{
		DefaultAgentId:         self.defaultAgentId,
		DefaultConversationIds: make(map[string]string, len(self.defaultConversationIds)),
		DiscordChannelId:       self.discordChannelId,
		TelegramChatId:         self.telegramChatId,
	}
	for agentId, conversationId := range self.defaultConversationIds {
		state.DefaultConversationIds[agentId] = conversationId
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

// SetDefaultAgent sets the system-wide default agent. Returns an error if the agent doesn't exist.
func (self *AgentRegistry) SetDefaultAgent(agentId string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if _, ok := self.runners[agentId]; !ok {
		return fmt.Errorf("agent not found: %s", agentId)
	}
	self.defaultAgentId = agentId
	self.saveState()
	return nil
}

// DefaultConversationID returns the default conversation for the given agent.
// If none is set, it auto-generates a new ULID and stores it.
func (self *AgentRegistry) DefaultConversationID(agentId string) string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if conversationId, ok := self.defaultConversationIds[agentId]; ok {
		return conversationId
	}
	conversationId := security.NewULID()
	self.defaultConversationIds[agentId] = conversationId
	self.saveState()
	return conversationId
}

// SetDefaultConversation sets the default conversation for the given agent.
func (self *AgentRegistry) SetDefaultConversation(agentId, conversationId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.defaultConversationIds[agentId] = conversationId
	self.saveState()
}

// SetDefaultConversationIfUnset sets the default conversation only if the agent has no default conversation.
// Returns true if the conversation was set.
func (self *AgentRegistry) SetDefaultConversationIfUnset(agentId, conversationId string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if _, ok := self.defaultConversationIds[agentId]; ok {
		return false
	}
	self.defaultConversationIds[agentId] = conversationId
	self.saveState()
	return true
}

// NewConversation generates a new ULID, sets it as the default conversation for the agent, and returns it.
func (self *AgentRegistry) NewConversation(agentId string) string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	conversationId := security.NewULID()
	self.defaultConversationIds[agentId] = conversationId
	self.saveState()
	return conversationId
}

// DiscordChannelID returns the persisted Discord channel ID.
func (self *AgentRegistry) DiscordChannelID() string {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.discordChannelId
}

// SetDiscordChannelID saves the Discord channel ID to state.
func (self *AgentRegistry) SetDiscordChannelID(channelId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.discordChannelId == channelId {
		return
	}
	self.discordChannelId = channelId
	self.saveState()
}

// TelegramChatID returns the persisted Telegram chat ID.
func (self *AgentRegistry) TelegramChatID() int64 {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.telegramChatId
}

// SetTelegramChatID saves the Telegram chat ID to state.
func (self *AgentRegistry) SetTelegramChatID(chatId int64) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.telegramChatId == chatId {
		return
	}
	self.telegramChatId = chatId
	self.saveState()
}
