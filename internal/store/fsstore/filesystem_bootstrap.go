package fsstore

import (
	"fmt"
	"github.com/teanode/teanode/internal/util/trash"
	"os"
)

func (self *fileSystemTransaction) deleteAgentDirectories(agentId string) error {
	agentDirectory := self.agentDirectory(agentId)
	if _, statError := os.Stat(agentDirectory); os.IsNotExist(statError) {
		return fmt.Errorf("agent not found: %s", agentId)
	}
	movePaths := []string{agentDirectory}
	userEntries, readError := os.ReadDir(self.usersDirectory())
	if readError != nil && !os.IsNotExist(readError) {
		return readError
	}
	for _, userEntry := range userEntries {
		if !userEntry.IsDir() {
			continue
		}
		movePaths = append(movePaths, self.userAgentConversationsDirectory(userEntry.Name(), agentId))
	}
	for _, movePath := range movePaths {
		if _, statError := os.Stat(movePath); os.IsNotExist(statError) {
			continue
		} else if statError != nil {
			return statError
		}
		if moveError := trash.Move(movePath, self.trashDirectory()); moveError != nil {
			return moveError
		}
	}
	return nil
}
