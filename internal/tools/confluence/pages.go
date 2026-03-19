package confluence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type pagesTool struct {
	binary string
	runner commandRunner
}

func (self *pagesTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "confluence_pages",
			Description: "Interact with Confluence pages. Actions: read (read page content), " +
				"info (get page metadata), search (search for pages), find (find page by title), " +
				"create (create a new page), create_child (create a child page), " +
				"update (update a page), delete (delete a page), edit (get page content for editing), " +
				"move (move a page to new parent), children (list child pages), " +
				"export (export page with attachments), copy_tree (copy a page tree).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"read", "info", "search", "find", "create", "create_child", "update", "delete", "edit", "move", "children", "export", "copy_tree"},
						"description": "The page action to perform.",
					},
					"page_id": map[string]interface{}{
						"type":        "string",
						"description": "Page ID or URL (for 'read', 'info', 'update', 'delete', 'edit', 'move', 'children', 'export', 'copy_tree' actions).",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Page title (for 'create', 'create_child', 'find' actions; optional new title for 'update' and 'move').",
					},
					"space_key": map[string]interface{}{
						"type":        "string",
						"description": "Space key (for 'create' action; optional for 'find' to limit search to a space).",
					},
					"parent_id": map[string]interface{}{
						"type":        "string",
						"description": "Parent page ID (for 'create_child' action; target parent for 'move' and 'copy_tree').",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Page content (for 'create', 'create_child', 'update' actions).",
					},
					"content_format": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"storage", "html", "markdown"},
						"description": "Content format (for 'create', 'create_child', 'update' actions; default: storage).",
					},
					"read_format": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"html", "text", "markdown"},
						"description": "Output format for reading (for 'read' action; default: text).",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query (for 'search' action).",
					},
					"cql": map[string]interface{}{
						"type":        "boolean",
						"description": "Treat query as raw CQL (for 'search' action).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (for 'search' action, default 10).",
					},
					"recursive": map[string]interface{}{
						"type":        "boolean",
						"description": "List descendants recursively (for 'children' and 'export' actions).",
					},
					"dest": map[string]interface{}{
						"type":        "string",
						"description": "Destination directory (for 'export' action).",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "Preview without making changes (for 'copy_tree' and 'export' actions).",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *pagesTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"read", "info", "search", "find", "edit", "children", "export"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *pagesTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action        string `json:"action"`
		PageID        string `json:"page_id"`
		Title         string `json:"title"`
		SpaceKey      string `json:"space_key"`
		ParentID      string `json:"parent_id"`
		Content       string `json:"content"`
		ContentFormat string `json:"content_format"`
		ReadFormat    string `json:"read_format"`
		Query         string `json:"query"`
		CQL           bool   `json:"cql"`
		Limit         int    `json:"limit"`
		Recursive     bool   `json:"recursive"`
		Dest          string `json:"dest"`
		DryRun        bool   `json:"dry_run"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "read":
		if args.PageID == "" {
			return "", fmt.Errorf("page_id is required for read action")
		}
		commandArgs := []string{"read", args.PageID}
		if args.ReadFormat != "" {
			commandArgs = append(commandArgs, "--format", args.ReadFormat)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArgs...)

	case "info":
		if args.PageID == "" {
			return "", fmt.Errorf("page_id is required for info action")
		}
		return execConfluence(ctx, self.runner, self.binary, "info", args.PageID)

	case "search":
		if args.Query == "" {
			return "", fmt.Errorf("query is required for search action")
		}
		commandArgs := []string{"search", args.Query}
		if args.CQL {
			commandArgs = append(commandArgs, "--cql")
		}
		if args.Limit > 0 {
			commandArgs = append(commandArgs, "--limit", fmt.Sprintf("%d", args.Limit))
		}
		return execConfluence(ctx, self.runner, self.binary, commandArgs...)

	case "find":
		if args.Title == "" {
			return "", fmt.Errorf("title is required for find action")
		}
		commandArgs := []string{"find", args.Title}
		if args.SpaceKey != "" {
			commandArgs = append(commandArgs, "--space", args.SpaceKey)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArgs...)

	case "create":
		if args.Title == "" {
			return "", fmt.Errorf("title is required for create action")
		}
		if args.SpaceKey == "" {
			return "", fmt.Errorf("space_key is required for create action")
		}
		commandArgs := []string{"create", args.Title, args.SpaceKey}
		if args.Content != "" {
			commandArgs = append(commandArgs, "--content", args.Content)
		}
		if args.ContentFormat != "" {
			commandArgs = append(commandArgs, "--format", args.ContentFormat)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArgs...)

	case "create_child":
		if args.Title == "" {
			return "", fmt.Errorf("title is required for create_child action")
		}
		if args.ParentID == "" {
			return "", fmt.Errorf("parent_id is required for create_child action")
		}
		commandArgs := []string{"create-child", args.Title, args.ParentID}
		if args.Content != "" {
			commandArgs = append(commandArgs, "--content", args.Content)
		}
		if args.ContentFormat != "" {
			commandArgs = append(commandArgs, "--format", args.ContentFormat)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArgs...)

	case "update":
		if args.PageID == "" {
			return "", fmt.Errorf("page_id is required for update action")
		}
		commandArgs := []string{"update", args.PageID}
		if args.Title != "" {
			commandArgs = append(commandArgs, "--title", args.Title)
		}
		if args.Content != "" {
			commandArgs = append(commandArgs, "--content", args.Content)
		}
		if args.ContentFormat != "" {
			commandArgs = append(commandArgs, "--format", args.ContentFormat)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArgs...)

	case "delete":
		if args.PageID == "" {
			return "", fmt.Errorf("page_id is required for delete action")
		}
		output, err := execConfluence(ctx, self.runner, self.binary, "delete", args.PageID, "--yes")
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("deleted", output), nil

	case "edit":
		if args.PageID == "" {
			return "", fmt.Errorf("page_id is required for edit action")
		}
		return execConfluence(ctx, self.runner, self.binary, "edit", args.PageID)

	case "move":
		if args.PageID == "" {
			return "", fmt.Errorf("page_id is required for move action")
		}
		if args.ParentID == "" {
			return "", fmt.Errorf("parent_id is required for move action")
		}
		commandArgs := []string{"move", args.PageID, args.ParentID}
		if args.Title != "" {
			commandArgs = append(commandArgs, "--title", args.Title)
		}
		output, err := execConfluence(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("moved", output), nil

	case "children":
		if args.PageID == "" {
			return "", fmt.Errorf("page_id is required for children action")
		}
		commandArgs := []string{"children", args.PageID, "--show-id"}
		if args.Recursive {
			commandArgs = append(commandArgs, "--recursive")
		}
		return execConfluence(ctx, self.runner, self.binary, commandArgs...)

	case "export":
		if args.PageID == "" {
			return "", fmt.Errorf("page_id is required for export action")
		}
		commandArgs := []string{"export", args.PageID, "--format", "markdown"}
		if args.Dest != "" {
			commandArgs = append(commandArgs, "--dest", args.Dest)
		}
		if args.Recursive {
			commandArgs = append(commandArgs, "--recursive")
		}
		if args.DryRun {
			commandArgs = append(commandArgs, "--dry-run")
		}
		return execConfluence(ctx, self.runner, self.binary, commandArgs...)

	case "copy_tree":
		if args.PageID == "" {
			return "", fmt.Errorf("page_id is required for copy_tree action")
		}
		if args.ParentID == "" {
			return "", fmt.Errorf("parent_id is required for copy_tree action")
		}
		commandArgs := []string{"copy-tree", args.PageID, args.ParentID}
		if args.Title != "" {
			commandArgs = append(commandArgs, args.Title)
		}
		if args.DryRun {
			commandArgs = append(commandArgs, "--dry-run")
		}
		return execConfluence(ctx, self.runner, self.binary, commandArgs...)

	default:
		return "", fmt.Errorf("unknown pages action: %s", args.Action)
	}
}
