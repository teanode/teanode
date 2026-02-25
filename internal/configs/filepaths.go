package configs

import "path/filepath"

var configDirectory string

// SetDirectory overrides the data directory. Must be called before any other
// config functions (EnsureDirs, Load, etc.).
func SetDirectory(directory string) {
	configDirectory = directory
}

// Directory returns the teanode data directory (default ~/.teanode).
func Directory() string {
	return configDirectory
}

// ConfigFilename returns the path to ~/.teanode/config.yaml.
func ConfigFilename() string {
	return filepath.Join(configDirectory, "config.yaml")
}

// AgentsDirectory returns the agents config directory (~/.teanode/agents/).
func AgentsDirectory() string {
	return filepath.Join(configDirectory, "agents")
}

// AgentWorkspaceDirectory returns the workspace directory for an agent (~/.teanode/agents/<agentId>/workspace/).
func AgentWorkspaceDirectory(agentId string) string {
	agentsDirectory := AgentsDirectory()
	return filepath.Join(agentsDirectory, agentId, "workspace")
}

// AgentConfigFilename returns the path to the agent config file (~/.teanode/agents/<agentId>/agent.yaml).
func AgentConfigFilename(agentId string) string {
	agentsDirectory := AgentsDirectory()
	return filepath.Join(agentsDirectory, agentId, "agent.yaml")
}

// UsersDirectory returns the users directory (~/.teanode/users).
func UsersDirectory() string {
	return filepath.Join(configDirectory, "users")
}

// UserDirectory returns the directory for a specific user (~/.teanode/users/<userId>).
func UserDirectory(userId string) string {
	usersDirectory := UsersDirectory()
	return filepath.Join(usersDirectory, userId)
}

// UserWorkspaceDirectory returns the workspace directory for a specific user (~/.teanode/users/<userId>/workspace).
func UserWorkspaceDirectory(userId string) string {
	userDirectory := UserDirectory(userId)
	return filepath.Join(userDirectory, "workspace")
}

// UserConversationsDirectory returns the conversations root for a specific user (~/.teanode/users/<userId>/conversations).
func UserConversationsDirectory(userId string) string {
	userDirectory := UserDirectory(userId)
	return filepath.Join(userDirectory, "conversations")
}

// UserJobsDirectory returns the jobs directory for a specific user (~/.teanode/users/<userId>/jobs).
func UserJobsDirectory(userId string) string {
	userDirectory := UserDirectory(userId)
	return filepath.Join(userDirectory, "jobs")
}

// UserAgentConversationsDirectory returns a user+agent conversation directory
// (~/.teanode/users/<userId>/conversations/<agentId>).
func UserAgentConversationsDirectory(userId, agentId string) string {
	userConversationsDirectory := UserConversationsDirectory(userId)
	return filepath.Join(userConversationsDirectory, agentId)
}

// UserConfigFilename returns the config path for a specific user (~/.teanode/users/<userId>/user.yaml).
func UserConfigFilename(userId string) string {
	userDirectory := UserDirectory(userId)
	return filepath.Join(userDirectory, "user.yaml")
}

// SkillsDirectory returns the skills directory (~/.teanode/skills).
func SkillsDirectory() string {
	return filepath.Join(configDirectory, "skills")
}

// ProjectsDirectory returns the projects directory (~/.teanode/projects).
func ProjectsDirectory() string {
	return filepath.Join(configDirectory, "projects")
}

// ProjectDirectory returns the project directory (~/.teanode/projects/<projectId>).
func ProjectDirectory(projectId string) string {
	return filepath.Join(ProjectsDirectory(), projectId)
}

// ProjectConfigFilename returns the project config path (~/.teanode/projects/<projectId>/project.yaml).
func ProjectConfigFilename(projectId string) string {
	return filepath.Join(ProjectDirectory(projectId), "project.yaml")
}

// ModelsFilename returns the path to the models cache file (~/.teanode/models.yaml).
func ModelsFilename() string {
	return filepath.Join(configDirectory, "models.yaml")
}

// MediaDirectory returns the media directory (~/.teanode/media).
func MediaDirectory() string {
	return filepath.Join(configDirectory, "media")
}

// SessionsDirectory returns the sessions directory (~/.teanode/sessions).
func SessionsDirectory() string {
	return filepath.Join(configDirectory, "sessions")
}

// GatewayPIDFilename returns the gateway process PID file path (~/.teanode/gateway.pid).
func GatewayPIDFilename() string {
	return filepath.Join(configDirectory, "gateway.pid")
}

// TrashDirectory returns the trash directory (~/.teanode/.trash).
func TrashDirectory() string {
	return filepath.Join(configDirectory, ".trash")
}

// StateFilename returns the path to the state file (~/.teanode/state.yaml).
func StateFilename() string {
	return filepath.Join(configDirectory, "state.yaml")
}

// SecurityFilename returns the path to ~/.teanode/security.yaml.
func SecurityFilename() string {
	return filepath.Join(configDirectory, "security.yaml")
}
