package gw

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/provider"
)

// RegisterTools adds the consolidated gateway lifecycle tool to the registry.
// The scheduleLifecycle callback defers the action until the current agent run
// completes, ensuring the LLM can generate its final response before shutdown.
func RegisterTools(registry *agents.ToolRegistry, scheduleLifecycle func(LifecycleAction)) {
	registry.Register(&gatewayTool{scheduleLifecycle: scheduleLifecycle})
}

// --- gateway (multi-action) ---

type gatewayTool struct {
	scheduleLifecycle func(LifecycleAction)
}

func (self *gatewayTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "gateway",
			Description: "Manage the gateway process lifecycle. Actions: restart (graceful restart with same configuration), terminate (shut down the gateway).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"restart", "terminate"},
						"description": "The lifecycle action to perform.",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "The lifecycle action that was scheduled.",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"description": "Status of the request.",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Human-readable description of what will happen.",
					},
				},
			},
		},
	}
}

func (self *gatewayTool) Execute(context context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	var action LifecycleAction
	var status string

	switch arguments.Action {
	case "restart":
		action = LifecycleRestart
		status = "scheduled_restart"
	case "terminate":
		action = LifecycleShutdown
		status = "scheduled_shutdown"
	default:
		return "", fmt.Errorf("unknown gateway action: %s", arguments.Action)
	}

	self.scheduleLifecycle(action)

	result, _ := json.Marshal(map[string]interface{}{
		"action":  arguments.Action,
		"status":  status,
		"message": "The gateway will " + arguments.Action + " after you finish your response. The full conversation history is preserved across restarts — you will have complete context when the conversation resumes.",
	})
	return string(result), nil
}
