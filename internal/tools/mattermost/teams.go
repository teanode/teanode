package mattermost

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type teamsTool struct {
	binary string
	runner commandRunner
}

func (self *teamsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "mattermost_teams",
			Description: "Manage Mattermost teams and server. Actions: list (list your teams), " +
				"switch (set active team), info (show team details), members (list team members), " +
				"server_info (show server information).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "switch", "info", "members", "server_info"},
						"description": "The team action to perform.",
					},
					"team": map[string]interface{}{
						"type":        "string",
						"description": "Team name (for 'switch', 'info', 'members' actions).",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *teamsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list", "info", "members", "server_info"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *teamsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action string `json:"action"`
		Team   string `json:"team"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list":
		return execMattermost(ctx, self.runner, self.binary, "team", "list")

	case "switch":
		if args.Team == "" {
			return "", fmt.Errorf("team is required for switch action")
		}
		output, err := execMattermost(ctx, self.runner, self.binary, "team", "switch", args.Team)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("switched", output), nil

	case "info":
		if args.Team != "" {
			return execMattermost(ctx, self.runner, self.binary, "team", "info", args.Team)
		}
		return execMattermost(ctx, self.runner, self.binary, "team", "info")

	case "members":
		if args.Team != "" {
			return execMattermost(ctx, self.runner, self.binary, "team", "members", args.Team)
		}
		return execMattermost(ctx, self.runner, self.binary, "team", "members")

	case "server_info":
		return execMattermost(ctx, self.runner, self.binary, "server", "info")

	default:
		return "", fmt.Errorf("unknown teams action: %s", args.Action)
	}
}
