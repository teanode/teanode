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
			Description: "Manage the node process. Actions: restart (graceful restart with same configuration), terminate (shut down the node), update (check for and optionally apply available updates).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"restart", "terminate", "update"},
						"description": "The action to perform.",
					},
					"forceCheck": map[string]interface{}{
						"type":        "boolean",
						"description": "When action is \"update\": if true, always perform a fresh remote check; if false or omitted, use cached check results when available.",
					},
					"applyIfAvailable": map[string]interface{}{
						"type":        "boolean",
						"description": "When action is \"update\": if true, download and apply the update when one is available, then restart; returns a no_update status if the node is already up to date.",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "The action that was performed.",
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
		Action           string `json:"action"`
		ForceCheck       bool   `json:"forceCheck"`
		ApplyIfAvailable bool   `json:"applyIfAvailable"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "update":
		return self.executeUpdate(ctx, arguments.ForceCheck, arguments.ApplyIfAvailable)
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

func (self *nodeTool) executeUpdate(ctx context.Context, forceCheck bool, applyIfAvailable bool) (string, error) {
	updateManager := updater.UpdaterFromContext(ctx)
	if updateManager == nil {
		return "", fmt.Errorf("updater is not available")
	}

	// Resolve update status: force a fresh remote check, use cache, or
	// fall back to a remote check when no cached result exists.
	var status updater.Status
	if forceCheck {
		status = updateManager.Check(ctx)
	} else {
		status = updateManager.Status()
		if status.LastChecked == nil {
			status = updateManager.Check(ctx)
		}
	}

	// Apply the update if requested.
	if applyIfAvailable {
		if !status.UpdateAvailable {
			result, _ := json.Marshal(map[string]interface{}{
				"action":          "update",
				"status":          "no_update",
				"currentVersion":  status.CurrentVersion,
				"updateAvailable": false,
				"message":         "No update available to apply.",
			})
			return string(result), nil
		}

		if err := updateManager.Apply(ctx); err != nil {
			result, _ := json.Marshal(map[string]interface{}{
				"action":  "update",
				"status":  "error",
				"message": err.Error(),
			})
			return string(result), nil
		}

		response := map[string]interface{}{
			"action":  "update",
			"status":  "applied",
			"message": "The node will restart after you finish your response. The full conversation history is preserved across restarts — you will have complete context when the conversation resumes.",
		}
		if status.Available != nil {
			response["version"] = status.Available.Version()
			if status.Available.Body != "" {
				response["releaseNotes"] = status.Available.Body
			}
		}
		result, _ := json.Marshal(response)
		return string(result), nil
	}

	// Check-only: return structured status.
	response := map[string]interface{}{
		"action":          "update",
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
	if status.Available != nil && status.Available.Body != "" {
		response["releaseNotes"] = status.Available.Body
	}

	result, _ := json.Marshal(response)
	return string(result), nil
}
