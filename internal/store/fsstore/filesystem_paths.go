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

func (self *fileSystemTransaction) securityFilename() string {
	return filepath.Join(self.dataDirectory(), "security.yaml")
}

func (self *fileSystemTransaction) stateFilename() string {
	return filepath.Join(self.dataDirectory(), "state.yaml")
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

func (self *fileSystemTransaction) sessionsDirectory() string {
	return filepath.Join(self.dataDirectory(), "sessions")
}

func (self *fileSystemTransaction) mediaDirectory() string {
	return filepath.Join(self.dataDirectory(), "media")
}

func (self *fileSystemTransaction) skillsDirectory() string {
	return filepath.Join(self.dataDirectory(), "skills")
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
