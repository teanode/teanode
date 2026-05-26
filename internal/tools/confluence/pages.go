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
	var arguments struct {
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
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("confluence: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "read":
		if arguments.PageID == "" {
			return "", fmt.Errorf("confluence: page_id is required for read action")
		}
		commandArguments := []string{"read", arguments.PageID}
		if arguments.ReadFormat != "" {
			commandArguments = append(commandArguments, "--format", arguments.ReadFormat)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArguments...)

	case "info":
		if arguments.PageID == "" {
			return "", fmt.Errorf("confluence: page_id is required for info action")
		}
		return execConfluence(ctx, self.runner, self.binary, "info", arguments.PageID)

	case "search":
		if arguments.Query == "" {
			return "", fmt.Errorf("confluence: query is required for search action")
		}
		commandArguments := []string{"search", arguments.Query}
		if arguments.CQL {
			commandArguments = append(commandArguments, "--cql")
		}
		if arguments.Limit > 0 {
			commandArguments = append(commandArguments, "--limit", fmt.Sprintf("%d", arguments.Limit))
		}
		return execConfluence(ctx, self.runner, self.binary, commandArguments...)

	case "find":
		if arguments.Title == "" {
			return "", fmt.Errorf("confluence: title is required for find action")
		}
		commandArguments := []string{"find", arguments.Title}
		if arguments.SpaceKey != "" {
			commandArguments = append(commandArguments, "--space", arguments.SpaceKey)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArguments...)

	case "create":
		if arguments.Title == "" {
			return "", fmt.Errorf("confluence: title is required for create action")
		}
		if arguments.SpaceKey == "" {
			return "", fmt.Errorf("confluence: space_key is required for create action")
		}
		commandArguments := []string{"create", arguments.Title, arguments.SpaceKey}
		if arguments.Content != "" {
			commandArguments = append(commandArguments, "--content", arguments.Content)
		}
		if arguments.ContentFormat != "" {
			commandArguments = append(commandArguments, "--format", arguments.ContentFormat)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArguments...)

	case "create_child":
		if arguments.Title == "" {
			return "", fmt.Errorf("confluence: title is required for create_child action")
		}
		if arguments.ParentID == "" {
			return "", fmt.Errorf("confluence: parent_id is required for create_child action")
		}
		commandArguments := []string{"create-child", arguments.Title, arguments.ParentID}
		if arguments.Content != "" {
			commandArguments = append(commandArguments, "--content", arguments.Content)
		}
		if arguments.ContentFormat != "" {
			commandArguments = append(commandArguments, "--format", arguments.ContentFormat)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArguments...)

	case "update":
		if arguments.PageID == "" {
			return "", fmt.Errorf("confluence: page_id is required for update action")
		}
		commandArguments := []string{"update", arguments.PageID}
		if arguments.Title != "" {
			commandArguments = append(commandArguments, "--title", arguments.Title)
		}
		if arguments.Content != "" {
			commandArguments = append(commandArguments, "--content", arguments.Content)
		}
		if arguments.ContentFormat != "" {
			commandArguments = append(commandArguments, "--format", arguments.ContentFormat)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArguments...)

	case "delete":
		if arguments.PageID == "" {
			return "", fmt.Errorf("confluence: page_id is required for delete action")
		}
		output, err := execConfluence(ctx, self.runner, self.binary, "delete", arguments.PageID, "--yes")
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("deleted", output), nil

	case "edit":
		if arguments.PageID == "" {
			return "", fmt.Errorf("confluence: page_id is required for edit action")
		}
		return execConfluence(ctx, self.runner, self.binary, "edit", arguments.PageID)

	case "move":
		if arguments.PageID == "" {
			return "", fmt.Errorf("confluence: page_id is required for move action")
		}
		if arguments.ParentID == "" {
			return "", fmt.Errorf("confluence: parent_id is required for move action")
		}
		commandArguments := []string{"move", arguments.PageID, arguments.ParentID}
		if arguments.Title != "" {
			commandArguments = append(commandArguments, "--title", arguments.Title)
		}
		output, err := execConfluence(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("moved", output), nil

	case "children":
		if arguments.PageID == "" {
			return "", fmt.Errorf("confluence: page_id is required for children action")
		}
		commandArguments := []string{"children", arguments.PageID, "--show-id"}
		if arguments.Recursive {
			commandArguments = append(commandArguments, "--recursive")
		}
		return execConfluence(ctx, self.runner, self.binary, commandArguments...)

	case "export":
		if arguments.PageID == "" {
			return "", fmt.Errorf("confluence: page_id is required for export action")
		}
		commandArguments := []string{"export", arguments.PageID, "--format", "markdown"}
		if arguments.Dest != "" {
			commandArguments = append(commandArguments, "--dest", arguments.Dest)
		}
		if arguments.Recursive {
			commandArguments = append(commandArguments, "--recursive")
		}
		if arguments.DryRun {
			commandArguments = append(commandArguments, "--dry-run")
		}
		return execConfluence(ctx, self.runner, self.binary, commandArguments...)

	case "copy_tree":
		if arguments.PageID == "" {
			return "", fmt.Errorf("confluence: page_id is required for copy_tree action")
		}
		if arguments.ParentID == "" {
			return "", fmt.Errorf("confluence: parent_id is required for copy_tree action")
		}
		commandArguments := []string{"copy-tree", arguments.PageID, arguments.ParentID}
		if arguments.Title != "" {
			commandArguments = append(commandArguments, arguments.Title)
		}
		if arguments.DryRun {
			commandArguments = append(commandArguments, "--dry-run")
		}
		return execConfluence(ctx, self.runner, self.binary, commandArguments...)

	default:
		return "", fmt.Errorf("confluence: unknown pages action: %s", arguments.Action)
	}
}
