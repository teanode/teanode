package agents

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

type persistedUserState struct {
	DefaultConversationIds map[string]string `yaml:"defaultConversationIds,omitempty"`
}

// persistedState is the YAML structure written to ~/.teanode/state.yaml.
type persistedState struct {
	Users map[string]persistedUserState `yaml:"users,omitempty"`
}

type userRuntimeState struct {
	DefaultConversationIds map[string]string
}

// AgentRegistry manages multiple named runners (one per agent).
type AgentRegistry struct {
	ctx         context.Context
	mutex       sync.RWMutex
	runners     map[string]*Runner // agentId → Runner
	userStates  map[string]*userRuntimeState
	createAgent func(agentId string, name string) error
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

func (self *AgentRegistry) SetCreateAgentFunc(create func(agentId string, name string) error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.createAgent = create
}

func (self *AgentRegistry) CreateAgentWithName(agentId string, name string) error {
	self.mutex.RLock()
	create := self.createAgent
	self.mutex.RUnlock()
	if create == nil {
		return fmt.Errorf("agent creation is not configured")
	}
	return create(agentId, name)
}

func (self *AgentRegistry) GetRunner(agentId string) *Runner {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.runners[agentId]
}

func (self *AgentRegistry) AgentDescription(agentId string) string {
	description := ""
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		agent, err := transaction.GetAgent(ctx, agentId, nil)
		if err != nil {
			return nil
		}
		description = agent.GetDescription()
		return nil
	})
	if description != "" {
		return description
	}
	return ""
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

func (self *AgentRegistry) EnsureDefaultConversation(userId, agentId string) string {
	if agentId == "" {
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
	if state == nil || userId == "" {
		return
	}
	if state.DefaultConversationIds == nil {
		state.DefaultConversationIds = map[string]string{}
	}
	// Collect registered agent IDs (mutex is already held by caller).
	agentIds := make([]string, 0, len(self.runners))
	for agentId := range self.runners {
		agentIds = append(agentIds, agentId)
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		for _, agentId := range agentIds {
			if _, exists := state.DefaultConversationIds[agentId]; exists {
				continue
			}
			// Try FindDefaultConversation first (reads persisted state).
			defaultConversation, findError := transaction.FindDefaultConversation(ctx, userId, agentId, nil)
			if findError == nil && defaultConversation != nil {
				state.DefaultConversationIds[agentId] = defaultConversation.ID
				continue
			}
			// Fall back to the most recent conversation for this agent.
			conversations, listError := transaction.ListConversations(ctx, store.ConversationListOptions{
				UserID:  ptrto.Value(userId),
				AgentID: ptrto.Value(agentId),
			}, nil)
			if listError != nil || len(conversations) == 0 {
				continue
			}
			state.DefaultConversationIds[agentId] = conversations[0].ID
		}
		return nil
	}); err != nil {
		log.Warningf("loading user state from store failed: %v", err)
	}
}

func (self *AgentRegistry) persistDefaultConversationLocked(userId string, agentId string, conversationId string) {
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		existingDefaultConversation, err := transaction.FindDefaultConversation(ctx, userId, agentId, nil)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err == nil && existingDefaultConversation != nil && existingDefaultConversation.ID != conversationId {
			if _, modifyError := transaction.ModifyConversation(ctx, existingDefaultConversation.ID, func(conversation *models.Conversation) error {
				conversation.Default = ptrto.Value(false)
				return nil
			}, nil); modifyError != nil {
				return modifyError
			}
		}

		conversation, getError := transaction.GetConversation(ctx, conversationId, nil)
		if getError != nil {
			if !errors.Is(getError, store.ErrNotFound) {
				return getError
			}
			_, createError := transaction.CreateConversation(ctx, &models.Conversation{
				ID:      conversationId,
				UserID:  ptrto.Value(userId),
				AgentID: ptrto.Value(agentId),
				Default: ptrto.Value(true),
			}, nil)
			return createError
		}

		_, modifyError := transaction.ModifyConversation(ctx, conversation.ID, func(conversation *models.Conversation) error {
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

