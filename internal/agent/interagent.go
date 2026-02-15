package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/teanode/teanode/internal/config"
	"github.com/teanode/teanode/internal/provider"
)

// RegisterInterAgentTools adds agent_list and agent_message tools to the registry
// if the agent has permission to message other agents (canMessage).
func RegisterInterAgentTools(registry *ToolRegistry, selfAgentId string, agentRegistry *AgentRegistry, configuration *config.Config) {
	agentConfig := configuration.AgentByID(selfAgentId)
	if agentConfig == nil {
		return
	}

	registry.Register(&agentListTool{
		agentRegistry: agentRegistry,
		configuration: configuration,
	})
	registry.Register(&agentMessageTool{
		selfAgentId:   selfAgentId,
		agentRegistry: agentRegistry,
		configuration: configuration,
	})
}

// --- agent_list ---

type agentListTool struct {
	agentRegistry *AgentRegistry
	configuration *config.Config
}

func (self *agentListTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "agent_list",
			Description: "List all available agents in the system.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agents": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id":   map[string]interface{}{"type": "string"},
								"name": map[string]interface{}{"type": "string"},
							},
						},
					},
				},
			},
		},
	}
}

func (self *agentListTool) Execute(_ context.Context, _ string) (string, error) {
	agentIds := self.agentRegistry.AgentIDs()
	agents := make([]map[string]interface{}, 0, len(agentIds))
	for _, agentId := range agentIds {
		entry := map[string]interface{}{
			"id": agentId,
		}
		if agentConfig := self.configuration.AgentByID(agentId); agentConfig != nil && agentConfig.Name != "" {
			entry["name"] = agentConfig.Name
		}
		agents = append(agents, entry)
	}
	result, _ := json.Marshal(map[string]interface{}{
		"agents": agents,
	})
	return string(result), nil
}

// --- agent_message ---

type agentMessageTool struct {
	selfAgentId   string
	agentRegistry *AgentRegistry
	configuration *config.Config
}

func (self *agentMessageTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "agent_message",
			Description: "Send a message to another agent and receive its response. The message runs synchronously — the tool returns when the target agent completes its response.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agentId": map[string]interface{}{
						"type":        "string",
						"description": "The ID of the target agent to message. Likely the user will specify agent by name, use agent_list tool to find its ID.",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "The message to send to the target agent.",
					},
					"sessionKey": map[string]interface{}{
						"type":        "string",
						"description": "Optional session key to continue a previous conversation with this agent. If omitted, a new session is created.",
					},
				},
				"required": []string{"agentId", "message"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agentId":    map[string]interface{}{"type": "string"},
					"response":   map[string]interface{}{"type": "string"},
					"sessionKey": map[string]interface{}{"type": "string"},
				},
			},
		},
	}
}

func (self *agentMessageTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		AgentID    string `json:"agentId"`
		Message    string `json:"message"`
		SessionKey string `json:"sessionKey"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.AgentID == "" || arguments.Message == "" {
		return "", fmt.Errorf("agentId and message are required")
	}

	// Permission check.
	callerConfig := self.configuration.AgentByID(self.selfAgentId)
	if callerConfig == nil {
		return "", fmt.Errorf("caller agent %q not found in config", self.selfAgentId)
	}
	allowed := false
	for _, targetId := range callerConfig.CanMessage {
		if targetId == "*" || targetId == arguments.AgentID {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("agent %q is not allowed to message agent %q", self.selfAgentId, arguments.AgentID)
	}

	// Resolve target runner.
	targetRunner := self.agentRegistry.Get(arguments.AgentID)
	if targetRunner == nil {
		return "", fmt.Errorf("agent %q not found", arguments.AgentID)
	}

	// Generate session key if not provided.
	sessionKey := arguments.SessionKey
	if sessionKey == "" {
		sessionKey = uuid.New().String()
	}

	// Prefix the message with source agent identity.
	prefixedMessage := fmt.Sprintf("[Message from agent '%s']: %s", self.selfAgentId, arguments.Message)

	// Run synchronously against the target agent.
	result, err := targetRunner.Run(ctx, RunParams{
		SessionKey: sessionKey,
		Message:    prefixedMessage,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("agent %q run failed: %w", arguments.AgentID, err)
	}

	response, _ := json.Marshal(map[string]interface{}{
		"agentId":    arguments.AgentID,
		"response":   result.Response,
		"sessionKey": sessionKey,
	})
	return string(response), nil
}
