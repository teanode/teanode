// Package node exposes tools for interacting with the node runtime.
package node

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/lifecycle"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/updater"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&nodeTool{}}
	})
}

// --- node (multi-action) ---

type nodeTool struct{}

func (self *nodeTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "node",
			Description: "Manage the node process. Actions: restart (graceful restart with same configuration), terminate (shut down the node), check_update (check for available updates), apply_update (download and apply an available update, then restart).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"restart", "terminate", "check_update", "apply_update"},
						"description": "The action to perform.",
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

func (self *nodeTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAdminApproval},
	}
}

func (self *nodeTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	user := models.UserFromContext(ctx)
	if user == nil || !user.GetAdmin() {
		return "", fmt.Errorf("admin access required to manage the node")
	}

	var arguments struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "check_update":
		return self.executeCheckUpdate(ctx)
	case "apply_update":
		return self.executeApplyUpdate(ctx)
	default:
		return self.executeLifecycle(ctx, arguments.Action)
	}
}

func (self *nodeTool) executeLifecycle(ctx context.Context, actionName string) (string, error) {
	lifecycleManager := lifecycle.LifecycleFromContext(ctx)
	if lifecycleManager == nil {
		return "", fmt.Errorf("missing lifecycle context")
	}

	var action lifecycle.Action
	var status string

	switch actionName {
	case "restart":
		action = lifecycle.Restart
		status = "scheduled_restart"
	case "terminate":
		action = lifecycle.Shutdown
		status = "scheduled_shutdown"
	default:
		return "", fmt.Errorf("unknown node action: %s", actionName)
	}

	lifecycleManager.ScheduleLifecycle(action)

	result, _ := json.Marshal(map[string]interface{}{
		"action":  actionName,
		"status":  status,
		"message": "The node will " + actionName + " after you finish your response. The full conversation history is preserved across restarts — you will have complete context when the conversation resumes.",
	})
	return string(result), nil
}

func (self *nodeTool) executeCheckUpdate(ctx context.Context) (string, error) {
	updateManager := updater.UpdaterFromContext(ctx)
	if updateManager == nil {
		return "", fmt.Errorf("updater is not available")
	}

	status := updateManager.Check(ctx)

	response := map[string]interface{}{
		"action":          "check_update",
		"currentVersion":  status.CurrentVersion,
		"updateAvailable": status.UpdateAvailable,
		"policy":          string(status.Policy),
	}
	if status.LatestVersion != "" {
		response["latestVersion"] = status.LatestVersion
	}
	if status.AheadOfRelease {
		response["aheadOfRelease"] = true
	}
	if status.IsContainer {
		response["isContainer"] = true
	}
	if status.Error != "" {
		response["error"] = status.Error
	}

	result, _ := json.Marshal(response)
	return string(result), nil
}

func (self *nodeTool) executeApplyUpdate(ctx context.Context) (string, error) {
	updateManager := updater.UpdaterFromContext(ctx)
	if updateManager == nil {
		return "", fmt.Errorf("updater is not available")
	}

	if err := updateManager.Apply(ctx); err != nil {
		result, _ := json.Marshal(map[string]interface{}{
			"action":  "apply_update",
			"status":  "error",
			"message": err.Error(),
		})
		return string(result), nil
	}

	result, _ := json.Marshal(map[string]interface{}{
		"action":  "apply_update",
		"status":  "applied",
		"message": "Update applied successfully. The node will restart momentarily. The full conversation history is preserved — you will have complete context when the conversation resumes.",
	})
	return string(result), nil
}
