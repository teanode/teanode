package api

import (
	"context"
	"encoding/json"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
)

// rpcHandlerError is a typed error that carries an HTTP-like status code for RPC responses.
type rpcHandlerError struct {
	code    int
	message string
}

func (self *rpcHandlerError) Error() string { return self.message }

// rpcError creates a new rpcHandlerError.
func rpcError(code int, message string) *rpcHandlerError {
	return &rpcHandlerError{code: code, message: message}
}

// handleRpc dispatches an RPC request to the appropriate handler and returns
// the response payload or an error.
func (self *webSocketConnection) handleRpc(frame requestFrame) (interface{}, error) {
	switch frame.Method {
	// Handshake & health.
	case "connect":
		return self.handleConnect(frame)
	case "health":
		return self.handleHealth(frame)

	// Agents.
	case "agents.list":
		return self.handleAgentsList(frame)
	case "agents.setDefault":
		return self.handleAgentsSetDefault(frame)
	case "agents.config.schema":
		return self.handleAgentsConfigSchema(frame)
	case "agents.config.list":
		return self.handleAgentsConfigList(frame)
	case "agents.config.save":
		return self.handleAgentsConfigSave(frame)
	case "agents.config.delete":
		return self.handleAgentsConfigDelete(frame)
	case "agents.avatar.set":
		return self.handleAgentsAvatarSet(frame)
	case "agents.avatar.remove":
		return self.handleAgentsAvatarRemove(frame)

	// Conversations.
	case "conversations.send":
		return self.handleConversationsSend(frame)
	case "conversations.history":
		return self.handleConversationsHistory(frame)
	case "conversations.abort":
		return self.handleConversationsAbort(frame)
	case "conversations.list":
		return self.handleConversationsList(frame)
	case "conversations.delete":
		return self.handleConversationsDelete(frame)
	case "conversations.setDefault":
		return self.handleConversationsSetDefault(frame)
	case "conversations.state":
		return self.handleConversationsState(frame)

	// Models.
	case "models.list":
		return self.handleModelsList(frame)

	// Config.
	case "config.schema":
		return self.handleConfigSchema(frame)
	case "config.get":
		return self.handleConfigGet(frame)
	case "config.update":
		return self.handleConfigUpdate(frame)

	// Jobs.
	case "jobs.list":
		return self.handleJobsList(frame)
	case "jobs.create":
		return self.handleJobsCreate(frame)
	case "jobs.update":
		return self.handleJobsUpdate(frame)
	case "jobs.delete":
		return self.handleJobsDelete(frame)
	case "jobs.trigger":
		return self.handleJobsTrigger(frame)
	case "jobs.runs.list":
		return self.handleJobRunsList(frame)

	// Sessions.
	case "sessions.list":
		return self.handleSessionsList(frame)
	case "sessions.revoke":
		return self.handleSessionsRevoke(frame)

	// Auth.
	case "auth.tokens.list":
		return self.handleAuthTokensList(frame)
	case "auth.tokens.create":
		return self.handleAuthTokensCreate(frame)
	case "auth.tokens.delete":
		return self.handleAuthTokensDelete(frame)
	case "auth.changePassword":
		return self.handleAuthChangePassword(frame)

	// Users.
	case "users.list":
		return self.handleUsersList(frame)
	case "users.create":
		return self.handleUsersCreate(frame)
	case "users.delete":
		return self.handleUsersDelete(frame)
	case "users.changePassword":
		return self.handleUsersChangePassword(frame)
	case "users.update":
		return self.handleUsersUpdate(frame)
	case "users.setRole":
		return self.handleUsersSetRole(frame)

	// Profile.
	case "profile.get":
		return self.handleProfileGet(frame)
	case "profile.update":
		return self.handleProfileUpdate(frame)
	case "profile.avatar.remove":
		return self.handleProfileAvatarRemove(frame)

	// Skills & secrets.
	case "skills.local.list":
		return self.handleSkillsLocalList(frame)
	case "skills.library.search":
		return self.handleSkillsLibrarySearch(frame)
	case "skills.install":
		return self.handleSkillsInstall(frame)
	case "skills.installed.list":
		return self.handleSkillsInstalledList(frame)
	case "skills.uninstall":
		return self.handleSkillsUninstall(frame)
	case "skills.update":
		return self.handleSkillsUpdate(frame)
	case "skills.setEnabled":
		return self.handleSkillsSetEnabled(frame)
	case "secrets.list":
		return self.handleSecretsList(frame)
	case "secrets.set":
		return self.handleSecretsSet(frame)

	// Voice.
	case "voice.providers":
		return self.handleVoiceProviders(frame)
	case "voice.start":
		return self.handleVoiceStart(frame)
	case "voice.end":
		return self.handleVoiceEnd(frame)
	case "voice.response.cancel":
		return self.handleVoiceResponseCancel(frame)
	case "voice.input.commit":
		return self.handleVoiceInputCommit(frame)

	// Projects.
	case "projects.list":
		return self.handleProjectsList(frame)
	case "projects.create":
		return self.handleProjectsCreate(frame)
	case "projects.rename":
		return self.handleProjectsRename(frame)
	case "projects.delete":
		return self.handleProjectsDelete(frame)

	// Todos.
	case "conversations.todos.list":
		return self.handleConversationsTodosList(frame)
	case "conversations.todos.batch":
		return self.handleConversationsTodosBatch(frame)
	case "projects.todos.summary":
		return self.handleProjectsTodosSummary(frame)
	case "projects.todos.list":
		return self.handleProjectsTodosList(frame)

	// Questions.
	case "questions.list":
		return self.handleQuestionsList(frame)
	case "questions.answer":
		return self.handleQuestionsAnswer(frame)

	// Approvals.
	case "approvals.list":
		return self.handleApprovalsList(frame)
	case "approvals.resolve":
		return self.handleApprovalsResolve(frame)

	// Tab integration.
	case "tab.attach":
		return self.handleTabAttach(frame)
	case "tab.detach":
		return self.handleTabDetach(frame)
	case "tab.commandResult":
		return self.handleTabCommandResult(frame)

	// Usage.
	case "usages.list":
		return self.handleListUsages(frame)

	// Memory.
	case "memory.list":
		return self.handleMemoryList(frame)
	case "memory.search":
		return self.handleMemorySearch(frame)
	case "memory.delete":
		return self.handleMemoryDelete(frame)

	// Tool policies.
	case "toolPolicies.list":
		return self.handleToolPoliciesList(frame)
	case "toolPolicies.update":
		return self.handleToolPoliciesUpdate(frame)

	// Update.
	case "update.status":
		return self.handleUpdateStatus(frame)
	case "update.check":
		return self.handleUpdateCheck(frame)
	case "update.apply":
		return self.handleUpdateApply(frame)

	default:
		return nil, rpcError(404, "unknown method: "+frame.Method)
	}
}

// requireAdmin returns an error if the current user is not an admin.
func (self *webSocketConnection) requireAdmin() error {
	if !self.isAdmin() {
		return rpcError(403, "admin access required")
	}
	return nil
}

// listAgents loads all agents from the store.
func (self *webSocketConnection) listAgents() ([]*models.Agent, error) {
	agentsList := make([]*models.Agent, 0)
	if transactionError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		listedAgents, listError := transaction.ListAgents(ctx, nil)
		if listError != nil {
			return listError
		}
		agentsList = listedAgents
		return nil
	}); transactionError != nil {
		return nil, transactionError
	}
	return agentsList, nil
}

// loadConfiguration loads the configuration from the store.
func (self *webSocketConnection) loadConfiguration() (*models.Configuration, error) {
	var configuration *models.Configuration
	if transactionError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		loadedConfiguration, loadError := transaction.GetConfiguration(ctx, nil)
		if loadError != nil {
			return loadError
		}
		configuration = loadedConfiguration
		return nil
	}); transactionError != nil {
		return nil, transactionError
	}
	return configuration, nil
}

// agentAvatarMediaId returns the avatar media ID for the given agent.
func (self *webSocketConnection) agentAvatarMediaId(agentId string) string {
	avatarMediaId := ""
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		agent, err := transaction.GetAgent(ctx, agentId, nil)
		if err != nil {
			return nil
		}
		avatarMediaId = agent.GetAvatarMediaID()
		return nil
	})
	return avatarMediaId
}

// verifyConversationOwnership checks that the conversation belongs to the current user.
func (self *webSocketConnection) verifyConversationOwnership(conversationId string) error {
	return store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		conversation, err := transaction.GetConversation(ctx, conversationId, nil)
		if err != nil {
			return err
		}
		if conversation.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		return nil
	})
}

// verifyConversationAccess checks that the current user has access to the conversation.
func (self *webSocketConnection) verifyConversationAccess(conversationId string) error {
	var conversation *models.Conversation
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		conv, err := tx.GetConversation(ctx, conversationId, nil)
		if err != nil {
			return err
		}
		conversation = conv
		return nil
	}); err != nil {
		return err
	}
	userId := self.userId()
	if conversation.GetUserID() != userId && !self.isAdmin() {
		return store.ErrNotFound
	}
	return nil
}

// unmarshalParameters unmarshals the request frame parameters into the given pointer.
func unmarshalParameters[T any](frame requestFrame) (T, error) {
	var parameters T
	if err := json.Unmarshal(frame.Parameters, &parameters); err != nil {
		return parameters, rpcError(400, "invalid parameters: "+err.Error())
	}
	return parameters, nil
}
