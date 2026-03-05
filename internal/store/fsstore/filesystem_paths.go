package fsstore

import (
	"os"
	"os/user"
	"path/filepath"
)

func (self *fileSystemTransaction) dataDirectory() string {
	return self.store.dataDirectory
}

func (self *fileSystemTransaction) configurationFilename() string {
	return filepath.Join(self.dataDirectory(), "config.yaml")
}

func (self *fileSystemTransaction) usersDirectory() string {
	return filepath.Join(self.dataDirectory(), "users")
}

func (self *fileSystemTransaction) userDirectory(userId string) string {
	return filepath.Join(self.usersDirectory(), userId)
}

func (self *fileSystemTransaction) userConfigurationFilename(userId string) string {
	return filepath.Join(self.userDirectory(userId), "user.yaml")
}

func (self *fileSystemTransaction) userWorkspaceDirectory(userId string) string {
	return filepath.Join(self.userDirectory(userId), "workspace")
}

func (self *fileSystemTransaction) userConversationsDirectory(userId string) string {
	return filepath.Join(self.userDirectory(userId), "conversations")
}

func (self *fileSystemTransaction) userAgentConversationsDirectory(userId, agentId string) string {
	return filepath.Join(self.userConversationsDirectory(userId), agentId)
}

func (self *fileSystemTransaction) userJobsDirectory(userId string) string {
	return filepath.Join(self.userDirectory(userId), "jobs")
}

func (self *fileSystemTransaction) agentsDirectory() string {
	return filepath.Join(self.dataDirectory(), "agents")
}

func (self *fileSystemTransaction) agentDirectory(agentId string) string {
	return filepath.Join(self.agentsDirectory(), agentId)
}

func (self *fileSystemTransaction) agentConfigurationFilename(agentId string) string {
	return filepath.Join(self.agentDirectory(agentId), "agent.yaml")
}

func (self *fileSystemTransaction) agentWorkspaceDirectory(agentId string) string {
	return filepath.Join(self.agentDirectory(agentId), "workspace")
}

func (self *fileSystemTransaction) projectsDirectory() string {
	return filepath.Join(self.dataDirectory(), "projects")
}

func (self *fileSystemTransaction) projectDirectory(projectId string) string {
	return filepath.Join(self.projectsDirectory(), projectId)
}

func (self *fileSystemTransaction) projectConfigurationFilename(projectId string) string {
	return filepath.Join(self.projectDirectory(projectId), "project.yaml")
}

func (self *fileSystemTransaction) projectWorkspaceDirectory(projectId string) string {
	return filepath.Join(self.projectDirectory(projectId), "workspace")
}

func (self *fileSystemTransaction) userSessionsDirectory(userId string) string {
	return filepath.Join(self.userDirectory(userId), "sessions")
}

func (self *fileSystemTransaction) userSessionFilename(userId, sessionId string) string {
	return filepath.Join(self.userSessionsDirectory(userId), sessionId+".yaml")
}

func (self *fileSystemTransaction) userTokensDirectory(userId string) string {
	return filepath.Join(self.userDirectory(userId), "tokens")
}

func (self *fileSystemTransaction) userTokenFilename(userId, tokenId string) string {
	return filepath.Join(self.userTokensDirectory(userId), tokenId+".yaml")
}

func (self *fileSystemTransaction) mediaDirectory() string {
	return filepath.Join(self.dataDirectory(), "media")
}

func (self *fileSystemTransaction) skillsDirectory() string {
	return filepath.Join(self.dataDirectory(), "skills")
}

func (self *fileSystemTransaction) projectTodosDirectory(projectId string) string {
	return filepath.Join(self.projectDirectory(projectId), "todos")
}

func (self *fileSystemTransaction) projectTodoFilePath(projectId, todoId string) string {
	return filepath.Join(self.projectTodosDirectory(projectId), todoId+".yaml")
}

func (self *fileSystemTransaction) conversationTodosDirectory(userId, agentId, conversationId string) string {
	return filepath.Join(self.userAgentConversationsDirectory(userId, agentId), conversationId+".todos")
}

func (self *fileSystemTransaction) conversationTodoFilePath(userId, agentId, conversationId, todoId string) string {
	return filepath.Join(self.conversationTodosDirectory(userId, agentId, conversationId), todoId+".yaml")
}

func (self *fileSystemTransaction) trashDirectory() string {
	return filepath.Join(self.dataDirectory(), ".trash")
}

func processUsername() string {
	if currentUser, currentUserError := user.Current(); currentUserError == nil {
		trimmedUsername := currentUser.Username
		if trimmedUsername != "" {
			return trimmedUsername
		}
	}
	for _, environmentName := range []string{"USER", "USERNAME"} {
		trimmedUsername := os.Getenv(environmentName)
		if trimmedUsername != "" {
			return trimmedUsername
		}
	}
	return "teanode"
}
