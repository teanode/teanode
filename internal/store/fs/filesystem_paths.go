package fs

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

func (self *transaction) dataDirectory() string {
	return self.store.dataDirectory
}

func (self *transaction) configFilename() string {
	return filepath.Join(self.dataDirectory(), "config.yaml")
}

func (self *transaction) securityFilename() string {
	return filepath.Join(self.dataDirectory(), "security.yaml")
}

func (self *transaction) stateFilename() string {
	return filepath.Join(self.dataDirectory(), "state.yaml")
}

func (self *transaction) usersDirectory() string {
	return filepath.Join(self.dataDirectory(), "users")
}

func (self *transaction) userDirectory(userId string) string {
	return filepath.Join(self.usersDirectory(), userId)
}

func (self *transaction) userConfigFilename(userId string) string {
	return filepath.Join(self.userDirectory(userId), "user.yaml")
}

func (self *transaction) userWorkspaceDirectory(userId string) string {
	return filepath.Join(self.userDirectory(userId), "workspace")
}

func (self *transaction) userConversationsDirectory(userId string) string {
	return filepath.Join(self.userDirectory(userId), "conversations")
}

func (self *transaction) userAgentConversationsDirectory(userId, agentId string) string {
	return filepath.Join(self.userConversationsDirectory(userId), agentId)
}

func (self *transaction) userJobsDirectory(userId string) string {
	return filepath.Join(self.userDirectory(userId), "jobs")
}

func (self *transaction) agentsDirectory() string {
	return filepath.Join(self.dataDirectory(), "agents")
}

func (self *transaction) agentDirectory(agentId string) string {
	return filepath.Join(self.agentsDirectory(), agentId)
}

func (self *transaction) agentConfigFilename(agentId string) string {
	return filepath.Join(self.agentDirectory(agentId), "agent.yaml")
}

func (self *transaction) agentWorkspaceDirectory(agentId string) string {
	return filepath.Join(self.agentDirectory(agentId), "workspace")
}

func (self *transaction) projectsDirectory() string {
	return filepath.Join(self.dataDirectory(), "projects")
}

func (self *transaction) projectDirectory(projectId string) string {
	return filepath.Join(self.projectsDirectory(), projectId)
}

func (self *transaction) projectConfigFilename(projectId string) string {
	return filepath.Join(self.projectDirectory(projectId), "project.yaml")
}

func (self *transaction) projectWorkspaceDirectory(projectId string) string {
	return filepath.Join(self.projectDirectory(projectId), "workspace")
}

func (self *transaction) sessionsDirectory() string {
	return filepath.Join(self.dataDirectory(), "sessions")
}

func (self *transaction) mediaDirectory() string {
	return filepath.Join(self.dataDirectory(), "media")
}

func (self *transaction) skillsDirectory() string {
	return filepath.Join(self.dataDirectory(), "skills")
}

func (self *transaction) trashDirectory() string {
	return filepath.Join(self.dataDirectory(), ".trash")
}

func processUsername() string {
	if currentUser, currentUserError := user.Current(); currentUserError == nil {
		trimmedUsername := strings.TrimSpace(currentUser.Username)
		if trimmedUsername != "" {
			return trimmedUsername
		}
	}
	for _, environmentName := range []string{"USER", "USERNAME"} {
		trimmedUsername := strings.TrimSpace(os.Getenv(environmentName))
		if trimmedUsername != "" {
			return trimmedUsername
		}
	}
	return "teanode"
}
