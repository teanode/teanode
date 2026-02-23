package agents

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/security"
	"gopkg.in/yaml.v3"
)

type persistedUserState struct {
	DefaultAgentId         string            `yaml:"defaultAgentId,omitempty"`
	DefaultConversationIds map[string]string `yaml:"defaultConversationIds,omitempty"`
}

// persistedState is the YAML structure written to ~/.teanode/state.yaml.
type persistedState struct {
	Users map[string]persistedUserState `yaml:"users,omitempty"`
}

type userRuntimeState struct {
	DefaultAgentId         string
	DefaultConversationIds map[string]string
}

// AgentRegistry manages multiple named runners (one per agent).
type AgentRegistry struct {
	mutex          sync.RWMutex
	runners        map[string]*Runner // agentId → Runner
	defaultAgentId string
	userStates     map[string]*userRuntimeState
	createAgent    func(agentConfig configs.AgentConfig) error
}

// NewAgentRegistry creates an empty agent registry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		runners:    make(map[string]*Runner),
		userStates: make(map[string]*userRuntimeState),
	}
}

func (self *AgentRegistry) ensureUserStateLocked(userId string) *userRuntimeState {
	if userId == "" {
		slog.Warn("agent registry requires non-empty userId")
		return nil
	}
	state, ok := self.userStates[userId]
	if !ok {
		state = &userRuntimeState{DefaultConversationIds: map[string]string{}}
		self.userStates[userId] = state
	}
	if state.DefaultConversationIds == nil {
		state.DefaultConversationIds = map[string]string{}
	}
	return state
}

// Register adds or replaces a runner for the given agent ID.
func (self *AgentRegistry) Register(agentId string, runner *Runner) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.runners[agentId] = runner
}

func (self *AgentRegistry) SetCreateAgentFunc(create func(agentConfig configs.AgentConfig) error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.createAgent = create
}

func (self *AgentRegistry) CreateAgent(agentConfig configs.AgentConfig) error {
	self.mutex.RLock()
	create := self.createAgent
	self.mutex.RUnlock()
	if create == nil {
		return fmt.Errorf("agent creation is not configured")
	}
	return create(agentConfig)
}

func (self *AgentRegistry) Get(agentId string) *Runner {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.runners[agentId]
}

func (self *AgentRegistry) SetDefault(agentId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.defaultAgentId = agentId
}

func (self *AgentRegistry) DefaultID() string {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.defaultAgentId
}

func (self *AgentRegistry) DefaultIDForUser(userId string) string {
	if userId == "" {
		return ""
	}
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	if state, ok := self.userStates[userId]; ok {
		if state.DefaultAgentId != "" {
			if _, exists := self.runners[state.DefaultAgentId]; exists {
				return state.DefaultAgentId
			}
		}
	}
	return ""
}

func (self *AgentRegistry) Default() *Runner {
	return self.Get(self.DefaultID())
}

func (self *AgentRegistry) AgentIDs() []string {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	ids := make([]string, 0, len(self.runners))
	for agentId := range self.runners {
		ids = append(ids, agentId)
	}
	return ids
}

func (self *AgentRegistry) Reconfigure(agentId string, configuration *configs.Config, providerRegistry *providers.Registry, tools *ToolRegistry, skillPrompts string) {
	runner := self.Get(agentId)
	if runner == nil {
		return
	}
	runner.Reconfigure(configuration, providerRegistry, tools, skillPrompts)
}

func (self *AgentRegistry) ForEach(fn func(agentId string, runner *Runner)) {
	self.mutex.RLock()
	entries := make([]struct {
		agentId string
		runner  *Runner
	}, 0, len(self.runners))
	for agentId, runner := range self.runners {
		entries = append(entries, struct {
			agentId string
			runner  *Runner
		}{agentId: agentId, runner: runner})
	}
	self.mutex.RUnlock()

	for _, entry := range entries {
		fn(entry.agentId, entry.runner)
	}
}

// LoadState restores per-user default agent and conversation state from ~/.teanode/state.yaml.
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
	for userId, userState := range state.Users {
		if userId == "" {
			continue
		}
		runtimeState := self.ensureUserStateLocked(userId)
		if runtimeState == nil {
			continue
		}
		if userState.DefaultAgentId != "" {
			if _, ok := self.runners[userState.DefaultAgentId]; ok {
				runtimeState.DefaultAgentId = userState.DefaultAgentId
			}
		}
		for agentId, conversationId := range userState.DefaultConversationIds {
			if conversationId != "" {
				runtimeState.DefaultConversationIds[agentId] = conversationId
			}
		}
	}
}

func (self *AgentRegistry) saveState() {
	stateFile, err := configs.StateFile()
	if err != nil {
		return
	}
	state := persistedState{
		Users: make(map[string]persistedUserState, len(self.userStates)),
	}
	for userId, userState := range self.userStates {
		copyMap := map[string]string{}
		for agentId, conversationId := range userState.DefaultConversationIds {
			copyMap[agentId] = conversationId
		}
		state.Users[userId] = persistedUserState{
			DefaultAgentId:         userState.DefaultAgentId,
			DefaultConversationIds: copyMap,
		}
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

func (self *AgentRegistry) SetDefaultAgentForUser(userId, agentId string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if _, ok := self.runners[agentId]; !ok {
		return fmt.Errorf("agent not found: %s", agentId)
	}
	state := self.ensureUserStateLocked(userId)
	if state == nil {
		return fmt.Errorf("userId is required")
	}
	state.DefaultAgentId = agentId
	self.saveState()
	return nil
}

func (self *AgentRegistry) DefaultConversationID(userId, agentId string) string {
	if strings.TrimSpace(agentId) == "" {
		return ""
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	state := self.ensureUserStateLocked(userId)
	if state == nil {
		return ""
	}
	if conversationId, ok := state.DefaultConversationIds[agentId]; ok {
		return conversationId
	}
	conversationId := security.NewULID()
	state.DefaultConversationIds[agentId] = conversationId
	self.saveState()
	return conversationId
}

func (self *AgentRegistry) SetDefaultConversation(userId, agentId, conversationId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	state := self.ensureUserStateLocked(userId)
	if state == nil {
		return
	}
	state.DefaultConversationIds[agentId] = conversationId
	self.saveState()
}

func (self *AgentRegistry) SetDefaultConversationIfUnset(userId, agentId, conversationId string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	state := self.ensureUserStateLocked(userId)
	if state == nil {
		return false
	}
	if _, ok := state.DefaultConversationIds[agentId]; ok {
		return false
	}
	state.DefaultConversationIds[agentId] = conversationId
	self.saveState()
	return true
}

func (self *AgentRegistry) NewConversation(userId, agentId string) string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	state := self.ensureUserStateLocked(userId)
	if state == nil {
		return ""
	}
	conversationId := security.NewULID()
	state.DefaultConversationIds[agentId] = conversationId
	self.saveState()
	return conversationId
}
