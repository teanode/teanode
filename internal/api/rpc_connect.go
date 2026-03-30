package api

import (
	"github.com/teanode/teanode/internal/updater"
	"github.com/teanode/teanode/internal/version"
)

// handleConnect: handshake, return capabilities.
func (self *webSocketConnection) handleConnect(frame requestFrame) (interface{}, error) {
	agentsList, listError := self.listAgents()
	if listError != nil {
		return nil, rpcError(500, "listing agents: "+listError.Error())
	}
	defaultAgentId := self.defaultAgentId()
	agentInfos := make([]map[string]interface{}, 0, len(agentsList))
	for _, agent := range agentsList {
		info := map[string]interface{}{
			"id":                    agent.ID,
			"defaultConversationId": self.api.coordinator.EnsureDefaultConversation(self.userId(), agent.ID),
		}
		if name := agent.GetName(); name != "" {
			info["name"] = name
		}
		if avatarMediaId := self.agentAvatarMediaId(agent.ID); avatarMediaId != "" {
			info["avatarMediaId"] = avatarMediaId
		}
		agentInfos = append(agentInfos, info)
	}

	defaultProviderModelName := ""
	if configuration, err := self.loadConfiguration(); err == nil {
		if configuration.Models != nil && configuration.Models.Default != nil {
			defaultProviderModelName = *configuration.Models.Default
		}
	}

	capabilities := []string{"conversations"}
	if providerRegistry := self.api.coordinator.ProviderRegistry(); providerRegistry != nil {
		if _, _, ok := providerRegistry.FindTranscriber(); ok {
			capabilities = append(capabilities, "audio")
		}
	}

	result := map[string]interface{}{
		"version":                  version.Version(),
		"capabilities":             capabilities,
		"defaultProviderModelName": defaultProviderModelName,
		"agents":                   agentInfos,
		"defaultAgentId":           defaultAgentId,
		"defaultConversationId":    self.api.coordinator.EnsureDefaultConversation(self.userId(), defaultAgentId),
		"isAdmin":                  self.isAdmin(),
		"userId":                   self.userId(),
	}

	// Include update status if available and user is admin.
	if self.isAdmin() {
		if updateManager := updater.UpdaterFromContext(self.ctx); updateManager != nil {
			status := updateManager.Status()
			if status.UpdateAvailable {
				result["updateAvailable"] = map[string]interface{}{
					"version": status.LatestVersion,
				}
			}
		}
	}

	return result, nil
}

// handleHealth: health check.
func (self *webSocketConnection) handleHealth(frame requestFrame) (interface{}, error) {
	return map[string]interface{}{
		"status": "ok",
	}, nil
}
