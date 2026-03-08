package store

import (
	"context"
	"io"

	"github.com/teanode/teanode/internal/models"
)

type Store interface {
	Close() error
	Migrate(context.Context) error
	Transaction(context.Context, func(context.Context, Transaction) error) error
}

type Transaction interface {
	ConfigurationOperation
	AgentOperation
	UserOperation
	ProjectOperation
	TokenOperation
	WorkspaceFileOperation
	MemoryItemOperation
	ConversationOperation
	ConversationMessageOperation
	JobOperation
	SessionOperation
	MediaOperation
	SkillOperation
	TodoOperation
	UsageOperation
}

type ConfigurationOperation interface {
	GetConfiguration(ctx context.Context, options *Option) (*models.Configuration, error)
	ModifyConfiguration(ctx context.Context, modifier func(*models.Configuration) error, options *Option) (*models.Configuration, error)
}

type AgentOperation interface {
	ListAgents(ctx context.Context, options *Option) ([]*models.Agent, error)
	CreateAgent(ctx context.Context, agent *models.Agent, seedWorkspaceFiles []models.WorkspaceFile, options *Option) (*models.Agent, error)
	GetAgent(ctx context.Context, agentId string, options *Option) (*models.Agent, error)
	ModifyAgent(ctx context.Context, agentId string, modifier func(*models.Agent) error, options *Option) (*models.Agent, error)
	DeleteAgent(ctx context.Context, agentId string, options *Option) error
}

type UserOperation interface {
	ListUsers(ctx context.Context, options *Option) ([]*models.User, error)
	CreateUser(ctx context.Context, user *models.User, seedWorkspaceFiles []models.WorkspaceFile, options *Option) (*models.User, error)
	GetUser(ctx context.Context, userId string, options *Option) (*models.User, error)
	GetUserByUsername(ctx context.Context, username string, options *Option) (*models.User, error)
	GetUserByTelegramChatID(ctx context.Context, telegramChatId int64, options *Option) (*models.User, error)
	GetUserByDiscordUserID(ctx context.Context, discordUserId string, options *Option) (*models.User, error)
	ModifyUser(ctx context.Context, userId string, modifier func(*models.User) error, options *Option) (*models.User, error)
	DeleteUser(ctx context.Context, userId string, options *Option) error
}

type ProjectOperation interface {
	ListProjects(ctx context.Context, options *Option) ([]*models.Project, error)
	CreateProject(ctx context.Context, project *models.Project, seedWorkspaceFiles []models.WorkspaceFile, options *Option) (*models.Project, error)
	GetProject(ctx context.Context, projectId string, options *Option) (*models.Project, error)
	ModifyProject(ctx context.Context, projectId string, modifier func(*models.Project) error, options *Option) (*models.Project, error)
	DeleteProject(ctx context.Context, projectId string, options *Option) error
}

type TokenOperation interface {
	ListTokens(ctx context.Context, userId string, options *Option) ([]*models.Token, error)
	CreateToken(ctx context.Context, token *models.Token, options *Option) (*models.Token, error)
	GetToken(ctx context.Context, tokenId string, options *Option) (*models.Token, error)
	GetTokenByToken(ctx context.Context, token string, options *Option) (*models.Token, error)
	ModifyToken(ctx context.Context, tokenId string, modifier func(*models.Token) error, options *Option) (*models.Token, error)
	DeleteToken(ctx context.Context, tokenId string, options *Option) error
}

type WorkspaceFileOperation interface {
	CreateWorkspaceFile(ctx context.Context, file *models.WorkspaceFile, options *Option) (*models.WorkspaceFile, error)
	GetWorkspaceFileByPath(ctx context.Context, scope models.Scope, scopeId string, relativePath string, options *Option) (*models.WorkspaceFile, error)
	ModifyWorkspaceFileByPath(ctx context.Context, scope models.Scope, scopeId string, relativePath string, modifier func(*models.WorkspaceFile) error, options *Option) (*models.WorkspaceFile, error)
	DeleteWorkspaceFileByPath(ctx context.Context, scope models.Scope, scopeId string, relativePath string, options *Option) error
	ListWorkspaceFilesByPath(ctx context.Context, scope models.Scope, scopeId string, relativePath string, options *Option) ([]*models.WorkspaceFile, error)
	SearchWorkspaceFiles(ctx context.Context, scope models.Scope, scopeId string, query string, searchOptions WorkspaceSearchOptions, options *Option) ([]WorkspaceFileSearchResult, error)
}

type ConversationOperation interface {
	ListConversations(ctx context.Context, listOptions ConversationListOptions, options *Option) ([]*models.Conversation, error)
	CreateConversation(ctx context.Context, conversation *models.Conversation, options *Option) (*models.Conversation, error)
	GetConversation(ctx context.Context, conversationId string, options *Option) (*models.Conversation, error)
	FindDefaultConversation(ctx context.Context, userId string, agentId string, options *Option) (*models.Conversation, error)
	SetDefaultConversation(ctx context.Context, conversationId string, options *Option) error
	ModifyConversation(ctx context.Context, conversationId string, modifier func(*models.Conversation) error, options *Option) (*models.Conversation, error)
	DeleteConversation(ctx context.Context, conversationId string, options *Option) error
}

type ConversationMessageOperation interface {
	ListConversationMessages(ctx context.Context, conversationId string, options *Option) ([]*models.ConversationMessage, error)
	CreateConversationMessage(ctx context.Context, message *models.ConversationMessage, options *Option) (*models.ConversationMessage, error)
}

type JobOperation interface {
	ListJobs(ctx context.Context, userId string, options *Option) ([]*models.Job, error)
	CreateJob(ctx context.Context, job *models.Job, options *Option) (*models.Job, error)
	GetJob(ctx context.Context, jobId string, options *Option) (*models.Job, error)
	ModifyJob(ctx context.Context, jobId string, modifier func(*models.Job) error, options *Option) (*models.Job, error)
	DeleteJob(ctx context.Context, jobId string, options *Option) error
}

type SessionOperation interface {
	ListSessions(ctx context.Context, options *Option) ([]*models.Session, error)
	CreateSession(ctx context.Context, session *models.Session, options *Option) (*models.Session, error)
	GetSession(ctx context.Context, sessionId string, options *Option) (*models.Session, error)
	ModifySession(ctx context.Context, sessionId string, modifier func(*models.Session) error, options *Option) (*models.Session, error)
	DeleteSession(ctx context.Context, sessionId string, options *Option) error
}

type MediaOperation interface {
	ListMedia(ctx context.Context, listOptions MediaListOptions, options *Option) ([]*models.Media, error)
	CreateMedia(ctx context.Context, content io.Reader, metadata *models.Media, options *Option) (*models.Media, error)
	GetMedia(ctx context.Context, mediaId string, options *Option) ([]byte, *models.Media, error)
	OpenMedia(ctx context.Context, mediaId string, options *Option) (io.ReadCloser, *models.Media, error)
	ModifyMedia(ctx context.Context, mediaId string, modifier func(*models.Media) error, options *Option) (*models.Media, error)
	DeleteMedia(ctx context.Context, mediaId string, options *Option) error
}

type TodoOperation interface {
	ListTodos(ctx context.Context, listOptions TodoListOptions, options *Option) ([]*models.Todo, error)
	CreateTodo(ctx context.Context, todo *models.Todo, options *Option) (*models.Todo, error)
	GetTodo(ctx context.Context, todoId string, options *Option) (*models.Todo, error)
	ModifyTodo(ctx context.Context, todoId string, modifier func(*models.Todo) error, options *Option) (*models.Todo, error)
	DeleteTodo(ctx context.Context, todoId string, options *Option) error
}

type SkillOperation interface {
	ListSkills(ctx context.Context, options *Option) ([]*models.Skill, error)
	CreateSkill(ctx context.Context, skill *models.Skill, options *Option) (*models.Skill, error)
	GetSkill(ctx context.Context, skillId string, options *Option) (*models.Skill, error)
	ModifySkill(ctx context.Context, skillId string, modifier func(*models.Skill) error, options *Option) (*models.Skill, error)
	DeleteSkill(ctx context.Context, skillId string, options *Option) error
}

type UsageOperation interface {
	AccumulateUsage(ctx context.Context, usage *models.Usage, options *Option) error
	ListUsages(ctx context.Context, listOptions UsageListOptions, options *Option) ([]*models.Usage, error)
}

type MemoryItemOperation interface {
	CreateMemoryItem(ctx context.Context, item *models.MemoryItem, options *Option) (*models.MemoryItem, error)
	GetMemoryItem(ctx context.Context, memoryItemId string, options *Option) (*models.MemoryItem, error)
	ModifyMemoryItem(ctx context.Context, memoryItemId string, modifier func(*models.MemoryItem) error, options *Option) (*models.MemoryItem, error)
	DeleteMemoryItem(ctx context.Context, memoryItemId string, options *Option) error
	ListMemoryItems(ctx context.Context, scope models.Scope, scopeId string, listOptions MemoryItemListOptions, options *Option) ([]*models.MemoryItem, error)
	SearchMemoryItems(ctx context.Context, scope models.Scope, scopeId string, query string, searchOptions MemoryItemSearchOptions, options *Option) ([]MemoryItemSearchResult, error)
}
