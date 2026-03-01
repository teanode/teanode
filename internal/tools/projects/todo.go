package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{NewProjectTodoTool()}
	})
}

func NewProjectTodoTool() *projectTodoTool { return &projectTodoTool{} }

type projectTodoTool struct{}

type projectTodoResponse struct {
	Action     string         `json:"action"`
	Todo       *models.Todo   `json:"todo,omitempty"`
	Todos      []*models.Todo `json:"todos,omitempty"`
	TotalCount int            `json:"totalCount,omitempty"`
	OpenCount  int            `json:"openCount,omitempty"`
	DoneCount  int            `json:"doneCount,omitempty"`
	Success    bool           `json:"success,omitempty"`
}

func (self *projectTodoTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "project_todo",
			Description: "Manage project-scoped todos/tasks. Actions: list, add, update, complete, reopen, delete, clear_done, reset.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "add", "update", "complete", "reopen", "delete", "clear_done", "reset"},
						"description": "The todo action to perform.",
					},
					"projectId": map[string]interface{}{
						"type":        "string",
						"description": "Project ID (required for all actions).",
					},
					"projectName": map[string]interface{}{
						"type":        "string",
						"description": "Project name — resolved to projectId if projectId is omitted.",
					},
					"todoId": map[string]interface{}{
						"type":        "string",
						"description": "Todo ID (for update, complete, reopen, delete).",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Todo title (for add, update).",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Optional longer description (for add, update).",
					},
					"priority": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"low", "medium", "high"},
						"description": "Priority level (for add, update, list filter).",
					},
					"tags": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Labels/tags (for add, update).",
					},
					"tag": map[string]interface{}{
						"type":        "string",
						"description": "Filter by tag (for list).",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"open", "done"},
						"description": "Filter by status (for list). Default: returns all.",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *projectTodoTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action      string   `json:"action"`
		ProjectID   string   `json:"projectId"`
		ProjectName string   `json:"projectName"`
		TodoID      string   `json:"todoId"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Priority    string   `json:"priority"`
		Tags        []string `json:"tags"`
		Tag         string   `json:"tag"`
		Status      string   `json:"status"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	action := arguments.Action

	// Mutating actions require admin access.
	switch action {
	case "add", "update", "complete", "reopen", "delete", "clear_done", "reset":
		user := models.UserFromContext(ctx)
		if user == nil || !user.GetAdmin() {
			return "", fmt.Errorf("admin access required to %s project todos", action)
		}
	}

	// Resolve project ID.
	projectId, err := resolveProjectId(ctx, arguments.ProjectID, arguments.ProjectName)
	if err != nil {
		return "", err
	}

	switch action {
	case "list":
		return self.executeList(ctx, projectId, arguments.Status, arguments.Priority, arguments.Tag)
	case "add":
		return self.executeAdd(ctx, projectId, arguments.Title, arguments.Description, arguments.Priority, arguments.Tags)
	case "update":
		return self.executeUpdate(ctx, projectId, arguments.TodoID, arguments.Title, arguments.Description, arguments.Priority, arguments.Tags)
	case "complete":
		return self.executeComplete(ctx, projectId, arguments.TodoID)
	case "reopen":
		return self.executeReopen(ctx, projectId, arguments.TodoID)
	case "delete":
		return self.executeDelete(ctx, projectId, arguments.TodoID)
	case "clear_done":
		return self.executeClearDone(ctx, projectId)
	case "reset":
		return self.executeReset(ctx, projectId)
	default:
		return "", fmt.Errorf("unknown project_todo action: %s", action)
	}
}

func (self *projectTodoTool) executeList(ctx context.Context, projectId, statusFilter, priorityFilter, tagFilter string) (string, error) {
	var todos []*models.Todo
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		listed, err := tx.ListTodos(ctx, store.TodoListOptions{ProjectID: &projectId}, nil)
		if err != nil {
			return err
		}
		todos = listed
		return nil
	}); err != nil {
		return "", err
	}

	// Apply in-memory filters.
	filtered := filterTodos(todos, statusFilter, priorityFilter, tagFilter)
	openCount, doneCount := countByStatus(todos)

	output, _ := json.Marshal(projectTodoResponse{
		Action:     "list",
		Todos:      filtered,
		TotalCount: len(todos),
		OpenCount:  openCount,
		DoneCount:  doneCount,
	})
	return string(output), nil
}

func (self *projectTodoTool) executeAdd(ctx context.Context, projectId, title, description, priority string, tags []string) (string, error) {
	if title == "" {
		return "", fmt.Errorf("title is required")
	}
	if priority == "" {
		priority = "medium"
	}
	if tags == nil {
		tags = make([]string, 0)
	}

	var created *models.Todo
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todo := &models.Todo{
			ID:        security.NewULID(),
			ProjectID: ptrto.Value(projectId),
			Title:     ptrto.Value(title),
			Status:    ptrto.Value("open"),
			Priority:  ptrto.Value(priority),
			Tags:      &tags,
		}
		if description != "" {
			todo.Description = ptrto.Value(description)
		}
		result, err := tx.CreateTodo(ctx, todo, nil)
		if err != nil {
			return err
		}
		created = result
		return nil
	}); err != nil {
		return "", err
	}

	afterMutateProject(ctx, projectId)
	output, _ := json.Marshal(projectTodoResponse{Action: "add", Todo: created})
	return string(output), nil
}

func (self *projectTodoTool) executeUpdate(ctx context.Context, projectId, todoId, title, description, priority string, tags []string) (string, error) {
	if todoId == "" {
		return "", fmt.Errorf("todoId is required")
	}

	var updated *models.Todo
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.ModifyTodo(ctx, todoId, func(todo *models.Todo) error {
			if title != "" {
				todo.Title = ptrto.Value(title)
			}
			if description != "" {
				todo.Description = ptrto.Value(description)
			}
			if priority != "" {
				todo.Priority = ptrto.Value(priority)
			}
			if tags != nil {
				todo.Tags = &tags
			}
			return nil
		}, nil)
		if err != nil {
			return err
		}
		updated = result
		return nil
	}); err != nil {
		return "", err
	}

	afterMutateProject(ctx, projectId)
	output, _ := json.Marshal(projectTodoResponse{Action: "update", Todo: updated})
	return string(output), nil
}

func (self *projectTodoTool) executeComplete(ctx context.Context, projectId, todoId string) (string, error) {
	if todoId == "" {
		return "", fmt.Errorf("todoId is required")
	}

	var updated *models.Todo
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.ModifyTodo(ctx, todoId, func(todo *models.Todo) error {
			todo.Status = ptrto.Value("done")
			now := time.Now()
			todo.CompletedAt = &now
			return nil
		}, nil)
		if err != nil {
			return err
		}
		updated = result
		return nil
	}); err != nil {
		return "", err
	}

	afterMutateProject(ctx, projectId)
	output, _ := json.Marshal(projectTodoResponse{Action: "complete", Todo: updated})
	return string(output), nil
}

func (self *projectTodoTool) executeReopen(ctx context.Context, projectId, todoId string) (string, error) {
	if todoId == "" {
		return "", fmt.Errorf("todoId is required")
	}

	var updated *models.Todo
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.ModifyTodo(ctx, todoId, func(todo *models.Todo) error {
			todo.Status = ptrto.Value("open")
			todo.CompletedAt = nil
			return nil
		}, nil)
		if err != nil {
			return err
		}
		updated = result
		return nil
	}); err != nil {
		return "", err
	}

	afterMutateProject(ctx, projectId)
	output, _ := json.Marshal(projectTodoResponse{Action: "reopen", Todo: updated})
	return string(output), nil
}

func (self *projectTodoTool) executeDelete(ctx context.Context, projectId, todoId string) (string, error) {
	if todoId == "" {
		return "", fmt.Errorf("todoId is required")
	}

	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		return tx.DeleteTodo(ctx, todoId, nil)
	}); err != nil {
		return "", err
	}

	afterMutateProject(ctx, projectId)
	output, _ := json.Marshal(projectTodoResponse{Action: "delete", Success: true})
	return string(output), nil
}

func (self *projectTodoTool) executeClearDone(ctx context.Context, projectId string) (string, error) {
	var deleted int
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todos, err := tx.ListTodos(ctx, store.TodoListOptions{ProjectID: &projectId}, nil)
		if err != nil {
			return err
		}
		for _, todo := range todos {
			if todo.GetStatus() == "done" {
				if err := tx.DeleteTodo(ctx, todo.ID, nil); err != nil {
					return err
				}
				deleted++
			}
		}
		return nil
	}); err != nil {
		return "", err
	}

	if deleted > 0 {
		afterMutateProject(ctx, projectId)
	}
	output, _ := json.Marshal(projectTodoResponse{Action: "clear_done", Success: true, DoneCount: deleted})
	return string(output), nil
}

func (self *projectTodoTool) executeReset(ctx context.Context, projectId string) (string, error) {
	var deleted int
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todos, err := tx.ListTodos(ctx, store.TodoListOptions{ProjectID: &projectId}, nil)
		if err != nil {
			return err
		}
		for _, todo := range todos {
			if err := tx.DeleteTodo(ctx, todo.ID, nil); err != nil {
				return err
			}
			deleted++
		}
		return nil
	}); err != nil {
		return "", err
	}

	if deleted > 0 {
		afterMutateProject(ctx, projectId)
	}
	output, _ := json.Marshal(projectTodoResponse{Action: "reset", Success: true, TotalCount: deleted})
	return string(output), nil
}

func resolveProjectId(ctx context.Context, projectId, projectName string) (string, error) {
	if projectId != "" {
		return projectId, nil
	}
	if projectName == "" {
		return "", fmt.Errorf("projectId or projectName is required")
	}
	projects, err := listProjects(ctx)
	if err != nil {
		return "", err
	}
	lowerName := strings.ToLower(projectName)
	for _, p := range projects {
		if p.Name != nil && strings.ToLower(*p.Name) == lowerName {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("project not found: %s", projectName)
}

func afterMutateProject(ctx context.Context, projectId string) {
	_ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.ModifyProject(ctx, projectId, func(p *models.Project) error {
			now := time.Now()
			p.ModifiedAt = &now
			return nil
		}, nil)
		return err
	})
}

func filterTodos(todos []*models.Todo, status, priority, tag string) []*models.Todo {
	if status == "" && priority == "" && tag == "" {
		return todos
	}
	filtered := make([]*models.Todo, 0, len(todos))
	for _, todo := range todos {
		if status != "" && todo.GetStatus() != status {
			continue
		}
		if priority != "" && todo.GetPriority() != priority {
			continue
		}
		if tag != "" && !containsTag(todo.GetTags(), tag) {
			continue
		}
		filtered = append(filtered, todo)
	}
	return filtered
}

func containsTag(tags []string, tag string) bool {
	lowerTag := strings.ToLower(tag)
	for _, t := range tags {
		if strings.ToLower(t) == lowerTag {
			return true
		}
	}
	return false
}

func countByStatus(todos []*models.Todo) (openCount, doneCount int) {
	for _, todo := range todos {
		switch todo.GetStatus() {
		case "open":
			openCount++
		case "done":
			doneCount++
		}
	}
	return
}
