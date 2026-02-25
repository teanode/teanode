package store

import (
	"io"

	"github.com/teanode/teanode/internal/models"
)

type Store interface {
	Close() error
	Migrate() error
	Transaction(func(Transaction) error) error
}

type Transaction interface {
	Commit() error

	ConfigurationOperation
	AgentOperation
	UserOperation
	ProjectOperation
	TokenOperation
	WorkspaceFileOperation
	ConversationOperation
	ConversationMessageOperation
	JobOperation
	SessionOperation
	MediaOperation
	SkillOperation
}

type ConfigurationOperation interface {
	GetConfiguration(options *Option) (*models.Configuration, error)
	ModifyConfiguration(modifier func(*models.Configuration) error, options *Option) (*models.Configuration, error)
}

type AgentOperation interface {
	ListAgents(options *Option) ([]models.Agent, error)
	CreateAgent(agent *models.Agent, seedWorkspaceFiles []models.WorkspaceFile, options *Option) (*models.Agent, error)
	GetAgent(agentId string, options *Option) (*models.Agent, error)
	ModifyAgent(agentId string, modifier func(*models.Agent) error, options *Option) (*models.Agent, error)
	DeleteAgent(agentId string, options *Option) error
}

type UserOperation interface {
	ListUsers(options *Option) ([]models.User, error)
	CreateUser(user *models.User, seedWorkspaceFiles []models.WorkspaceFile, options *Option) (*models.User, error)
	GetUser(userId string, options *Option) (*models.User, error)
	GetUserByUsername(username string, options *Option) (string, *models.User, bool)
	GetUserByTelegramChatID(telegramChatId int64, options *Option) (string, *models.User, bool)
	GetUserByDiscordUserID(discordUserId string, options *Option) (string, *models.User, bool)
	ModifyUser(userId string, modifier func(*models.User) error, options *Option) (*models.User, error)
	DeleteUser(userId string, options *Option) error
}

type ProjectOperation interface {
	ListProjects(options *Option) ([]models.Project, error)
	CreateProject(project *models.Project, seedWorkspaceFiles []models.WorkspaceFile, options *Option) (*models.Project, error)
	GetProject(projectId string, options *Option) (*models.Project, error)
	ModifyProject(projectId string, modifier func(*models.Project) error, options *Option) (*models.Project, error)
	DeleteProject(projectId string, options *Option) error
}

type TokenOperation interface {
	ListTokens(userId string, options *Option) ([]models.Token, error)
	CreateToken(token *models.Token, options *Option) (*models.Token, error)
	GetToken(tokenId string, options *Option) (*models.Token, error)
	GetTokenByToken(token string, options *Option) (string, *models.Token, bool)
	ModifyToken(tokenId string, modifier func(*models.Token) error, options *Option) (*models.Token, error)
	DeleteToken(tokenId string, options *Option) error
}

type WorkspaceFileOperation interface {
	CreateWorkspaceFile(file *models.WorkspaceFile, options *Option) (*models.WorkspaceFile, error)
	GetWorkspaceFileByPath(scope models.Scope, scopeId string, relativePath string, options *Option) (*models.WorkspaceFile, error)
	ModifyWorkspaceFileByPath(scope models.Scope, scopeId string, relativePath string, modifier func(*models.WorkspaceFile) error, options *Option) (*models.WorkspaceFile, error)
	DeleteWorkspaceFileByPath(scope models.Scope, scopeId string, relativePath string, options *Option) error
	ListWorkspaceFilesByPath(scope models.Scope, scopeId string, relativePath string, options *Option) ([]models.WorkspaceFile, error)
	SearchWorkspaceFiles(scope models.Scope, scopeId string, query string, searchOptions WorkspaceSearchOptions, options *Option) ([]WorkspaceFileSearchResult, error)
}

type ConversationOperation interface {
	ListConversations(listOptions ConversationListOptions, options *Option) ([]models.Conversation, error)
	CreateConversation(conversation *models.Conversation, options *Option) (*models.Conversation, error)
	GetConversation(conversationId string, options *Option) (*models.Conversation, error)
	FindDefaultConversation(userId string, agentId string, options *Option) (*models.Conversation, error)
	ModifyConversation(conversationId string, modifier func(*models.Conversation) error, options *Option) (*models.Conversation, error)
	DeleteConversation(conversationId string, options *Option) error
}

type ConversationMessageOperation interface {
	ListConversationMessages(conversationId string, options *Option) ([]models.ConversationMessage, error)
	CreateConversationMessage(message *models.ConversationMessage, options *Option) (*models.ConversationMessage, error)
	GetConversationMessage(messageId string, options *Option) (*models.ConversationMessage, error)
	ModifyConversationMessage(messageId string, modifier func(*models.ConversationMessage) error, options *Option) (*models.ConversationMessage, error)
	DeleteConversationMessage(messageId string, options *Option) error
}

type JobOperation interface {
	ListJobs(userId string, options *Option) ([]models.Job, error)
	CreateJob(job *models.Job, options *Option) (*models.Job, error)
	GetJob(jobId string, options *Option) (*models.Job, error)
	ModifyJob(jobId string, modifier func(*models.Job) error, options *Option) (*models.Job, error)
	DeleteJob(jobId string, options *Option) error
}

type SessionOperation interface {
	ListSessions(options *Option) ([]models.Session, error)
	CreateSession(session *models.Session, options *Option) (*models.Session, error)
	GetSession(sessionId string, options *Option) (*models.Session, error)
	ModifySession(sessionId string, modifier func(*models.Session) error, options *Option) (*models.Session, error)
	DeleteSession(sessionId string, options *Option) error
}

type MediaOperation interface {
	ListMedia(listOptions MediaListOptions, options *Option) ([]models.Media, error)
	CreateMedia(content io.Reader, metadata *models.Media, options *Option) (*models.Media, error)
	GetMedia(mediaId string, options *Option) ([]byte, *models.Media, error)
	OpenMedia(mediaId string, options *Option) (io.ReadCloser, *models.Media, error)
	ModifyMedia(mediaId string, modifier func(*models.Media) error, options *Option) (*models.Media, error)
	DeleteMedia(mediaId string, options *Option) error
}

type SkillOperation interface {
	ListSkills(options *Option) ([]models.Skill, error)
	CreateSkill(skill *models.Skill, options *Option) (*models.Skill, error)
	GetSkill(skillId string, options *Option) (*models.Skill, error)
	ModifySkill(skillId string, modifier func(*models.Skill) error, options *Option) (*models.Skill, error)
	DeleteSkill(skillId string, options *Option) error
}
