package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type mergeRequestsTool struct {
	binary string
	runner commandRunner
}

func (self *mergeRequestsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "gitlab_merge_requests",
			Description: "Interact with GitLab merge requests. Actions: list (list MRs), view (get MR details), " +
				"create (open new MR), comment (add comment), merge (merge MR), diff (get MR diff), " +
				"approve (approve MR).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "view", "create", "comment", "merge", "diff", "approve"},
						"description": "The merge request action to perform.",
					},
					"number": map[string]interface{}{
						"type":        "integer",
						"description": "Merge request IID (for view, comment, merge, diff, approve actions).",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Merge request title (for create action).",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Merge request description or comment text (for create and comment actions).",
					},
					"squash": map[string]interface{}{
						"type":        "boolean",
						"description": "Squash commits when merging (for merge action).",
					},
					"rebase": map[string]interface{}{
						"type":        "boolean",
						"description": "Rebase before merging (for merge action).",
					},
					"assignee": map[string]interface{}{
						"type":        "string",
						"description": "Filter by assignee username, use \"@me\" to refer to current user (for list action).",
					},
					"reviewer": map[string]interface{}{
						"type":        "string",
						"description": "Filter by reviewer username, use \"@me\" to refer to current user (for list action).",
					},
					"author": map[string]interface{}{
						"type":        "string",
						"description": "Filter by author username, use \"@me\" to refer to current user (for list action).",
					},
					"group": map[string]interface{}{
						"type":        "string",
						"description": "Filter merge requests within a GitLab group or subgroup (for list action). Ignored when repository is set.",
					},
					"state": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"opened", "closed", "merged", "all"},
						"description": "Filter by state (for list action, default opened).",
					},
					"page": map[string]interface{}{
						"type":        "integer",
						"description": "Page number to fetch for list action. Defaults to 1.",
					},
					"per_page": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results per page (for list action, default 30).",
					},
					"search": map[string]interface{}{
						"type":        "string",
						"description": "Search merge requests by title and description (for list action).",
					},
					"order": map[string]interface{}{
						"type":        "string",
						"description": "Order merge requests by a supported field such as created_at, updated_at, merged_at, title, priority, label_priority, milestone_due, or popularity (for list action).",
					},
					"sort": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"asc", "desc"},
						"description": "Sort direction for the selected order field (for list action).",
					},
					"source_branch": map[string]interface{}{
						"type":        "string",
						"description": "Filter by source branch name (for list action and create action).",
					},
					"target_branch": map[string]interface{}{
						"type":        "string",
						"description": "Filter by target branch name (for list action and create action, defaults to repo default branch when creating).",
					},
					"label": map[string]interface{}{
						"oneOf": []map[string]interface{}{
							{"type": "string"},
							{
								"type":  "array",
								"items": map[string]interface{}{"type": "string"},
							},
						},
						"description": "Filter by one or more labels for list action.",
					},
					"not_label": map[string]interface{}{
						"oneOf": []map[string]interface{}{
							{"type": "string"},
							{
								"type":  "array",
								"items": map[string]interface{}{"type": "string"},
							},
						},
						"description": "Exclude one or more labels for list action.",
					},
					"milestone": map[string]interface{}{
						"type":        "string",
						"description": "Filter by milestone identifier for list action.",
					},
					"draft": map[string]interface{}{
						"type":        "boolean",
						"description": "Filter draft merge requests for list action.",
					},
					"not_draft": map[string]interface{}{
						"type":        "boolean",
						"description": "Filter non-draft merge requests for list action.",
					},
					"created_after": map[string]interface{}{
						"type":        "string",
						"description": "Filter merge requests created after an ISO 8601 timestamp for list action.",
					},
					"created_before": map[string]interface{}{
						"type":        "string",
						"description": "Filter merge requests created before an ISO 8601 timestamp for list action.",
					},
					"repository": map[string]interface{}{
						"type":        "string",
						"description": "Target project in OWNER/REPO or GROUP/NAMESPACE/REPO format. If omitted, uses the current repository context.",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "JSON object with merge request data from GitLab.",
			},
		},
	}
}

func (self *mergeRequestsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list", "view", "diff"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *mergeRequestsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action        string `json:"action"`
		Number        int    `json:"number"`
		Title         string `json:"title"`
		Description   string `json:"description"`
		SourceBranch  string `json:"source_branch"`
		TargetBranch  string `json:"target_branch"`
		Squash        bool   `json:"squash"`
		Rebase        bool   `json:"rebase"`
		Assignee      string `json:"assignee"`
		Reviewer      string `json:"reviewer"`
		Author        string `json:"author"`
		Group         string `json:"group"`
		State         string `json:"state"`
		Page          int    `json:"page"`
		PerPage       int    `json:"per_page"`
		Search        string `json:"search"`
		Order         string `json:"order"`
		Sort          string `json:"sort"`
		Label         any    `json:"label"`
		NotLabel      any    `json:"not_label"`
		Milestone     string `json:"milestone"`
		Draft         bool   `json:"draft"`
		NotDraft      bool   `json:"not_draft"`
		CreatedAfter  string `json:"created_after"`
		CreatedBefore string `json:"created_before"`
		Repository    string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("gitlab: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list":
		perPage := arguments.PerPage
		if perPage <= 0 {
			perPage = 30
		}
		page := arguments.Page
		if page <= 0 {
			page = 1
		}
		commandArguments := []string{"mr", "list",
			"--output", "json",
			"--page", strconv.Itoa(page),
			"--per-page", strconv.Itoa(perPage)}
		switch arguments.State {
		case "closed":
			commandArguments = append(commandArguments, "--closed")
		case "merged":
			commandArguments = append(commandArguments, "--merged")
		case "all":
			commandArguments = append(commandArguments, "--all")
		case "opened", "":
			// Default behavior: glab lists opened MRs without any flag.
		}
		if arguments.Assignee != "" {
			commandArguments = append(commandArguments, "--assignee", arguments.Assignee)
		}
		if arguments.Reviewer != "" {
			commandArguments = append(commandArguments, "--reviewer", arguments.Reviewer)
		}
		if arguments.Author != "" {
			commandArguments = append(commandArguments, "--author", arguments.Author)
		}
		if arguments.Group != "" && arguments.Repository == "" {
			commandArguments = append(commandArguments, "--group", arguments.Group)
		}
		if arguments.Search != "" {
			commandArguments = append(commandArguments, "--search", arguments.Search)
		}
		order := arguments.Order
		sort := arguments.Sort
		if order == "" {
			if arguments.State == "merged" {
				order = "merged_at"
			} else {
				order = "updated_at"
			}
		}
		if sort == "" {
			sort = "desc"
		}
		if order != "" {
			commandArguments = append(commandArguments, "--order", order)
		}
		if sort != "" {
			commandArguments = append(commandArguments, "--sort", sort)
		}
		if arguments.SourceBranch != "" {
			commandArguments = append(commandArguments, "--source-branch", arguments.SourceBranch)
		}
		if arguments.TargetBranch != "" {
			commandArguments = append(commandArguments, "--target-branch", arguments.TargetBranch)
		}
		commandArguments = appendStringFlags(commandArguments, "--label", arguments.Label)
		commandArguments = appendStringFlags(commandArguments, "--not-label", arguments.NotLabel)
		if arguments.Milestone != "" {
			commandArguments = append(commandArguments, "--milestone", arguments.Milestone)
		}
		if arguments.Draft {
			commandArguments = append(commandArguments, "--draft")
		}
		if arguments.NotDraft {
			commandArguments = append(commandArguments, "--not-draft")
		}
		if arguments.CreatedAfter != "" {
			commandArguments = append(commandArguments, "--created-after", arguments.CreatedAfter)
		}
		if arguments.CreatedBefore != "" {
			commandArguments = append(commandArguments, "--created-before", arguments.CreatedBefore)
		}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArguments...)

	case "view":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitlab: number is required for view action")
		}
		commandArguments := []string{"mr", "view", strconv.Itoa(arguments.Number),
			"--output", "json", "--comments"}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArguments...)

	case "create":
		if arguments.Title == "" {
			return "", fmt.Errorf("gitlab: title is required for create action")
		}
		if arguments.Description == "" {
			return "", fmt.Errorf("gitlab: description is required for create action")
		}
		if arguments.SourceBranch == "" {
			return "", fmt.Errorf("gitlab: source_branch is required for create action")
		}
		commandArguments := []string{"mr", "create",
			"--title", arguments.Title, "--description", arguments.Description,
			"--source-branch", arguments.SourceBranch}
		if arguments.TargetBranch != "" {
			commandArguments = append(commandArguments, "--target-branch", arguments.TargetBranch)
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("created", output), nil

	case "comment":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitlab: number is required for comment action")
		}
		if arguments.Description == "" {
			return "", fmt.Errorf("gitlab: description is required for comment action")
		}
		commandArguments := []string{"mr", "note", strconv.Itoa(arguments.Number),
			"--message", arguments.Description}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("commented", output), nil

	case "merge":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitlab: number is required for merge action")
		}
		commandArguments := []string{"mr", "merge", strconv.Itoa(arguments.Number)}
		if arguments.Squash {
			commandArguments = append(commandArguments, "--squash")
		}
		if arguments.Rebase {
			commandArguments = append(commandArguments, "--rebase")
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("merged", output), nil

	case "diff":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitlab: number is required for diff action")
		}
		commandArguments := []string{"mr", "diff", strconv.Itoa(arguments.Number)}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		envelope := map[string]string{"diff": output}
		data, _ := json.Marshal(envelope)
		return string(data), nil

	case "approve":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitlab: number is required for approve action")
		}
		commandArguments := []string{"mr", "approve", strconv.Itoa(arguments.Number)}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("approved", output), nil

	default:
		return "", fmt.Errorf("gitlab: unknown merge_requests action: %s", arguments.Action)
	}
}
