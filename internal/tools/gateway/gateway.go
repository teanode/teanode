package gateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/lifecycle"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&gatewayTool{}}
	})
}

// --- gateway (multi-action) ---

type gatewayTool struct{}

func (self *gatewayTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
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

func (self *gatewayTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	user := models.UserFromContext(ctx)
	if user == nil || !user.GetAdmin() {
		return "", fmt.Errorf("admin access required to manage the gateway")
	}

	var arguments struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	lifecycleManager := lifecycle.LifecycleFromContext(ctx)
	if lifecycleManager == nil {
		return "", fmt.Errorf("missing lifecycle context")
	}

	var action lifecycle.Action
	var status string

	switch arguments.Action {
	case "restart":
		action = lifecycle.Restart
		status = "scheduled_restart"
	case "terminate":
		action = lifecycle.Shutdown
		status = "scheduled_shutdown"
	default:
		return "", fmt.Errorf("unknown gateway action: %s", arguments.Action)
	}

	lifecycleManager.ScheduleLifecycle(action)

	result, _ := json.Marshal(map[string]interface{}{
		"action":  arguments.Action,
		"status":  status,
		"message": "The gateway will " + arguments.Action + " after you finish your response. The full conversation history is preserved across restarts — you will have complete context when the conversation resumes.",
	})
	return string(result), nil
}
