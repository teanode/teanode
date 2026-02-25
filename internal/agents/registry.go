package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

type persistedUserState struct {
	DefaultAgentID         string            `yaml:"defaultAgentId,omitempty"`
	DefaultConversationIds map[string]string `yaml:"defaultConversationIds,omitempty"`
}

// persistedState is the YAML structure written to ~/.teanode/state.yaml.
type persistedState struct {
	Users map[string]persistedUserState `yaml:"users,omitempty"`
}

type userRuntimeState struct {
	DefaultAgentID         string
	DefaultConversationIds map[string]string
}

// AgentRegistry manages multiple named runners (one per agent).
type AgentRegistry struct {
	ctx         context.Context
	mutex       sync.RWMutex
	runners     map[string]*Runner // agentId → Runner
	userStates  map[string]*userRuntimeState
	createAgent func(agentConfig configs.AgentConfig) error
}

// NewAgentRegistry creates an empty agent registry.
func NewAgentRegistry(ctx context.Context) *AgentRegistry {
	return &AgentRegistry{
		ctx:        ctx,
		runners:    make(map[string]*Runner),
		userStates: make(map[string]*userRuntimeState),
	}
}

func (self *AgentRegistry) ensureUserStateLocked(userId string) *userRuntimeState {
	if userId == "" {
		log.Warningf("agent registry requires non-empty userId")
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

func (self *AgentRegistry) GetRunner(agentId string) *Runner {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.runners[agentId]
}

func (self *AgentRegistry) AgentDescription(agentId string) string {
	description := ""
	_ = store.StoreFromContext(self.ctx).Transaction(func(transaction store.Transaction) error {
		agent, err := transaction.GetAgent(agentId, nil)
		if err != nil {
			return nil
		}
		description = strings.TrimSpace(valueOrEmptyString(agent.Description))
		return nil
	})
	if description != "" {
		return description
	}
	return ""
}

func (self *AgentRegistry) EnsureDefaultAgent(userId string, defaultAgentId string) (string, bool, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	state := self.ensureUserStateLocked(userId)
	if state == nil {
		return "", false, fmt.Errorf("userId is required")
	}
	self.loadUserStateFromStoreLocked(userId, state)
	if state.DefaultAgentID != "" {
		if _, exists := self.runners[state.DefaultAgentID]; exists {
			return state.DefaultAgentID, false, nil
		}
	}

	if defaultAgentId == "" {
		return "", false, fmt.Errorf("defaultAgentId is required")
	}
	if _, exists := self.runners[defaultAgentId]; !exists {
		return "", false, fmt.Errorf("agent not found: %s", defaultAgentId)
	}
	state.DefaultAgentID = defaultAgentId
	self.persistDefaultAgentLocked(userId, defaultAgentId)
	return defaultAgentId, true, nil
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
	runner := self.GetRunner(agentId)
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
	// No-op: runtime state is sourced from store-backed user and conversation records.
}

func (self *AgentRegistry) saveState() {
	// No-op: runtime state is sourced from store-backed user and conversation records.
}

func (self *AgentRegistry) SetDefaultAgent(userId, agentId string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if _, ok := self.runners[agentId]; !ok {
		return fmt.Errorf("agent not found: %s", agentId)
	}
	state := self.ensureUserStateLocked(userId)
	if state == nil {
		return fmt.Errorf("userId is required")
	}
	state.DefaultAgentID = agentId
	self.persistDefaultAgentLocked(userId, agentId)
	return nil
}

func (self *AgentRegistry) EnsureDefaultConversation(userId, agentId string) string {
	if strings.TrimSpace(agentId) == "" {
		return ""
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	state := self.ensureUserStateLocked(userId)
	if state == nil {
		return ""
	}
	self.loadUserStateFromStoreLocked(userId, state)
	if conversationId, ok := state.DefaultConversationIds[agentId]; ok {
		return conversationId
	}
	conversationId := security.NewULID()
	state.DefaultConversationIds[agentId] = conversationId
	self.persistDefaultConversationLocked(userId, agentId, conversationId)
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
	self.persistDefaultConversationLocked(userId, agentId, conversationId)
}

func (self *AgentRegistry) SetDefaultConversationIfUnset(userId, agentId, conversationId string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	state := self.ensureUserStateLocked(userId)
	if state == nil {
		return false
	}
	self.loadUserStateFromStoreLocked(userId, state)
	if _, ok := state.DefaultConversationIds[agentId]; ok {
		return false
	}
	state.DefaultConversationIds[agentId] = conversationId
	self.persistDefaultConversationLocked(userId, agentId, conversationId)
	return true
}

func (self *AgentRegistry) NewDefaultConversation(userId, agentId string) string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	state := self.ensureUserStateLocked(userId)
	if state == nil {
		return ""
	}
	conversationId := security.NewULID()
	state.DefaultConversationIds[agentId] = conversationId
	self.persistDefaultConversationLocked(userId, agentId, conversationId)
	return conversationId
}

func (self *AgentRegistry) loadUserStateFromStoreLocked(userId string, state *userRuntimeState) {
	if state == nil || strings.TrimSpace(userId) == "" {
		return
	}
	if state.DefaultConversationIds == nil {
		state.DefaultConversationIds = map[string]string{}
	}
	if err := store.StoreFromContext(self.ctx).Transaction(func(transaction store.Transaction) error {
		user, err := transaction.GetUser(userId, nil)
		if err == nil {
			defaultAgentId := strings.TrimSpace(valueOrEmptyString(user.DefaultAgentID))
			if defaultAgentId != "" {
				if _, exists := self.runners[defaultAgentId]; exists {
					state.DefaultAgentID = defaultAgentId
				}
			}
		}

		defaultConversation := true
		conversations, listError := transaction.ListConversations(store.ConversationListOptions{
			UserID:  ptrto.Value(userId),
			Default: ptrto.Value(defaultConversation),
		}, nil)
		if listError != nil {
			return nil
		}
		if len(conversations) == 0 {
			allConversations, allListError := transaction.ListConversations(store.ConversationListOptions{
				UserID: ptrto.Value(userId),
			}, nil)
			if allListError == nil {
				conversations = allConversations
			}
		}
		for _, conversation := range conversations {
			agentId := strings.TrimSpace(valueOrEmptyString(conversation.AgentID))
			if agentId == "" {
				continue
			}
			state.DefaultConversationIds[agentId] = conversation.ID
		}
		return nil
	}); err != nil {
		log.Warningf("loading user state from store failed: %v", err)
	}
}

func (self *AgentRegistry) persistDefaultAgentLocked(userId string, agentId string) {
	if err := store.StoreFromContext(self.ctx).Transaction(func(transaction store.Transaction) error {
		_, err := transaction.ModifyUser(userId, func(user *models.User) error {
			user.DefaultAgentID = ptrto.Value(strings.TrimSpace(agentId))
			return nil
		}, nil)
		return err
	}); err != nil {
		log.Warningf("persisting default agent failed userId=%s agentId=%s error=%v", userId, agentId, err)
	}
}

func (self *AgentRegistry) persistDefaultConversationLocked(userId string, agentId string, conversationId string) {
	if err := store.StoreFromContext(self.ctx).Transaction(func(transaction store.Transaction) error {
		existingDefaultConversation, err := transaction.FindDefaultConversation(userId, agentId, nil)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err == nil && existingDefaultConversation != nil && existingDefaultConversation.ID != conversationId {
			if _, modifyError := transaction.ModifyConversation(existingDefaultConversation.ID, func(conversation *models.Conversation) error {
				conversation.Default = ptrto.Value(false)
				return nil
			}, nil); modifyError != nil {
				return modifyError
			}
		}

		conversation, getError := transaction.GetConversation(conversationId, nil)
		if getError != nil {
			if !errors.Is(getError, store.ErrNotFound) {
				return getError
			}
			_, createError := transaction.CreateConversation(&models.Conversation{
				ID:      conversationId,
				UserID:  ptrto.Value(userId),
				AgentID: ptrto.Value(agentId),
				Default: ptrto.Value(true),
			}, nil)
			return createError
		}

		_, modifyError := transaction.ModifyConversation(conversation.ID, func(conversation *models.Conversation) error {
			conversation.UserID = ptrto.Value(userId)
			conversation.AgentID = ptrto.Value(agentId)
			conversation.Default = ptrto.Value(true)
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		log.Warningf("persisting default conversation failed userId=%s agentId=%s conversationId=%s error=%v", userId, agentId, conversationId, err)
	}
}

func valueOrEmptyString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
