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

type projectBatchItem struct {
	Op          string   `json:"op"`
	TodoID      string   `json:"todoId"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    string   `json:"priority"`
	Tags        []string `json:"tags"`
}

type projectBatchResult struct {
	Index   int          `json:"index"`
	Op      string       `json:"op"`
	Success bool         `json:"success"`
	Todo    *models.Todo `json:"todo,omitempty"`
	Error   string       `json:"error,omitempty"`
	TodoID  string       `json:"todoId,omitempty"`
}

type projectBatchSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type projectTodoResponse struct {
	Action     string               `json:"action"`
	Todos      []*models.Todo       `json:"todos,omitempty"`
	Results    []projectBatchResult `json:"results,omitempty"`
	Summary    *projectBatchSummary `json:"summary,omitempty"`
	TotalCount int                  `json:"totalCount,omitempty"`
	OpenCount  int                  `json:"openCount,omitempty"`
	DoneCount  int                  `json:"doneCount,omitempty"`
	Success    bool                 `json:"success,omitempty"`
}

func (self *projectTodoTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "project_todo",
			Description: "Manage project-scoped todos/tasks. Use 'list' to view, 'batch' to create/update/complete/reopen/delete one or more todos in a single call, 'prune' to remove all completed todos.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "batch", "prune"},
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
					"status": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"open", "done"},
						"description": "Filter by status (for list). Default: returns all.",
					},
					"tag": map[string]interface{}{
						"type":        "string",
						"description": "Filter by tag (for list).",
					},
					"items": map[string]interface{}{
						"type":        "array",
						"minItems":    1,
						"maxItems":    50,
						"description": "Required when action is 'batch'. Each element describes one operation.",
						"items": map[string]interface{}{
							"type":     "object",
							"required": []string{"op"},
							"properties": map[string]interface{}{
								"op": map[string]interface{}{
									"type": "string",
									"enum": []string{"add", "update", "complete", "reopen", "delete"},
								},
								"todoId": map[string]interface{}{
									"type": "string",
								},
								"title": map[string]interface{}{
									"type":      "string",
									"maxLength": 512,
								},
								"description": map[string]interface{}{
									"type": "string",
								},
								"priority": map[string]interface{}{
									"type": "string",
									"enum": []string{"low", "medium", "high"},
								},
								"tags": map[string]interface{}{
									"type":  "array",
									"items": map[string]interface{}{"type": "string"},
								},
							},
						},
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *projectTodoTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAdminOnly},
	}
}

func (self *projectTodoTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action      string             `json:"action"`
		ProjectID   string             `json:"projectId"`
		ProjectName string             `json:"projectName"`
		Status      string             `json:"status"`
		Tag         string             `json:"tag"`
		Items       []projectBatchItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("projects: parsing arguments: %w", err)
	}

	action := arguments.Action

	// Mutating actions require admin access.
	switch action {
	case "batch", "prune":
		user := models.UserFromContext(ctx)
		if user == nil || !user.GetAdmin() {
			return "", fmt.Errorf("projects: admin access required to %s project todos", action)
		}
	}

	// Resolve project ID.
	projectId, err := resolveProjectId(ctx, arguments.ProjectID, arguments.ProjectName)
	if err != nil {
		return "", err
	}

	switch action {
	case "list":
		return self.executeList(ctx, projectId, arguments.Status, arguments.Tag)
	case "batch":
		return self.executeBatch(ctx, projectId, arguments.Items)
	case "prune":
		return self.executePrune(ctx, projectId)
	default:
		return "", fmt.Errorf("projects: unknown project_todo action: %s", action)
	}
}

func (self *projectTodoTool) executeList(ctx context.Context, projectId, statusFilter, tagFilter string) (string, error) {
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

	filtered := filterTodos(todos, statusFilter, "", tagFilter)
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

func (self *projectTodoTool) executeBatch(ctx context.Context, projectId string, items []projectBatchItem) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("projects: items is required and must contain 1-50 entries")
	}
	if len(items) > 50 {
		return "", fmt.Errorf("projects: items must contain at most 50 entries, got %d", len(items))
	}

	results := make([]projectBatchResult, len(items))
	succeeded := 0
	anySuccess := false

	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		for itemIndex, item := range items {
			results[itemIndex] = self.executeBatchItem(ctx, tx, projectId, itemIndex, item)
			if results[itemIndex].Success {
				succeeded++
				anySuccess = true
			}
		}
		return nil
	}); err != nil {
		return "", err
	}

	if anySuccess {
		afterMutateProject(ctx, projectId)
	}

	// Compute aggregate counts.
	var todos []*models.Todo
	_ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		listed, err := tx.ListTodos(ctx, store.TodoListOptions{ProjectID: &projectId}, nil)
		if err != nil {
			return err
		}
		todos = listed
		return nil
	})
	openCount, doneCount := countByStatus(todos)

	output, _ := json.Marshal(projectTodoResponse{
		Action:  "batch",
		Results: results,
		Summary: &projectBatchSummary{
			Total:     len(items),
			Succeeded: succeeded,
			Failed:    len(items) - succeeded,
		},
		TotalCount: len(todos),
		OpenCount:  openCount,
		DoneCount:  doneCount,
	})
	return string(output), nil
}

func (self *projectTodoTool) executeBatchItem(ctx context.Context, tx store.Transaction, projectId string, index int, item projectBatchItem) projectBatchResult {
	switch item.Op {
	case "add":
		return self.batchAdd(ctx, tx, projectId, index, item)
	case "update":
		return self.batchUpdate(ctx, tx, index, item)
	case "complete":
		return self.batchComplete(ctx, tx, index, item)
	case "reopen":
		return self.batchReopen(ctx, tx, index, item)
	case "delete":
		return self.batchDelete(ctx, tx, index, item)
	default:
		return projectBatchResult{Index: index, Op: item.Op, Success: false, Error: fmt.Sprintf("unknown op: %s", item.Op)}
	}
}

func (self *projectTodoTool) batchAdd(ctx context.Context, tx store.Transaction, projectId string, index int, item projectBatchItem) projectBatchResult {
	if item.Title == "" {
		return projectBatchResult{Index: index, Op: "add", Success: false, Error: "title is required for add"}
	}
	priority := item.Priority
	if priority == "" {
		priority = string(models.TodoPriorityMedium)
	}
	tags := item.Tags
	if tags == nil {
		tags = make([]string, 0)
	}
	todo := &models.Todo{
		ID:        security.NewULID(),
		ProjectID: ptrto.Value(projectId),
		Title:     ptrto.Value(item.Title),
		Status:    ptrto.Value(models.TodoStatusOpen),
		Priority:  ptrto.Value(models.TodoPriority(priority)),
		Tags:      &tags,
	}
	if item.Description != "" {
		todo.Description = ptrto.Value(item.Description)
	}
	created, err := tx.CreateTodo(ctx, todo, nil)
	if err != nil {
		return projectBatchResult{Index: index, Op: "add", Success: false, Error: err.Error()}
	}
	return projectBatchResult{Index: index, Op: "add", Success: true, Todo: created}
}

func (self *projectTodoTool) batchUpdate(ctx context.Context, tx store.Transaction, index int, item projectBatchItem) projectBatchResult {
	if item.TodoID == "" {
		return projectBatchResult{Index: index, Op: "update", Success: false, Error: "todoId is required for update"}
	}
	updated, err := tx.ModifyTodo(ctx, item.TodoID, func(todo *models.Todo) error {
		if item.Title != "" {
			todo.Title = ptrto.Value(item.Title)
		}
		if item.Description != "" {
			todo.Description = ptrto.Value(item.Description)
		}
		if item.Priority != "" {
			todo.Priority = ptrto.Value(models.TodoPriority(item.Priority))
		}
		if item.Tags != nil {
			todo.Tags = &item.Tags
		}
		return nil
	}, nil)
	if err != nil {
		return projectBatchResult{Index: index, Op: "update", Success: false, Error: err.Error(), TodoID: item.TodoID}
	}
	return projectBatchResult{Index: index, Op: "update", Success: true, Todo: updated}
}

func (self *projectTodoTool) batchComplete(ctx context.Context, tx store.Transaction, index int, item projectBatchItem) projectBatchResult {
	if item.TodoID == "" {
		return projectBatchResult{Index: index, Op: "complete", Success: false, Error: "todoId is required for complete"}
	}
	updated, err := tx.ModifyTodo(ctx, item.TodoID, func(todo *models.Todo) error {
		todo.Status = ptrto.Value(models.TodoStatusDone)
		now := time.Now()
		todo.CompletedAt = &now
		return nil
	}, nil)
	if err != nil {
		return projectBatchResult{Index: index, Op: "complete", Success: false, Error: err.Error(), TodoID: item.TodoID}
	}
	return projectBatchResult{Index: index, Op: "complete", Success: true, Todo: updated}
}

func (self *projectTodoTool) batchReopen(ctx context.Context, tx store.Transaction, index int, item projectBatchItem) projectBatchResult {
	if item.TodoID == "" {
		return projectBatchResult{Index: index, Op: "reopen", Success: false, Error: "todoId is required for reopen"}
	}
	updated, err := tx.ModifyTodo(ctx, item.TodoID, func(todo *models.Todo) error {
		todo.Status = ptrto.Value(models.TodoStatusOpen)
		todo.CompletedAt = nil
		return nil
	}, nil)
	if err != nil {
		return projectBatchResult{Index: index, Op: "reopen", Success: false, Error: err.Error(), TodoID: item.TodoID}
	}
	return projectBatchResult{Index: index, Op: "reopen", Success: true, Todo: updated}
}

func (self *projectTodoTool) batchDelete(ctx context.Context, tx store.Transaction, index int, item projectBatchItem) projectBatchResult {
	if item.TodoID == "" {
		return projectBatchResult{Index: index, Op: "delete", Success: false, Error: "todoId is required for delete"}
	}
	if err := tx.DeleteTodo(ctx, item.TodoID, nil); err != nil {
		return projectBatchResult{Index: index, Op: "delete", Success: false, Error: err.Error(), TodoID: item.TodoID}
	}
	return projectBatchResult{Index: index, Op: "delete", Success: true, TodoID: item.TodoID}
}

func (self *projectTodoTool) executePrune(ctx context.Context, projectId string) (string, error) {
	var deleted int
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todos, err := tx.ListTodos(ctx, store.TodoListOptions{ProjectID: &projectId}, nil)
		if err != nil {
			return err
		}
		for _, todo := range todos {
			if todo.GetStatus() == models.TodoStatusDone {
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
	output, _ := json.Marshal(projectTodoResponse{Action: "prune", Success: true, DoneCount: deleted})
	return string(output), nil
}

func resolveProjectId(ctx context.Context, projectId, projectName string) (string, error) {
	if projectId != "" {
		return projectId, nil
	}
	if projectName == "" {
		return "", fmt.Errorf("projects: projectId or projectName is required")
	}
	projects, err := listProjects(ctx)
	if err != nil {
		return "", err
	}
	lowerName := strings.ToLower(projectName)
	for _, project := range projects {
		if project.Name != nil && strings.ToLower(*project.Name) == lowerName {
			return project.ID, nil
		}
	}
	return "", fmt.Errorf("projects: project not found: %s", projectName)
}

func afterMutateProject(ctx context.Context, projectId string) {
	_ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.ModifyProject(ctx, projectId, func(project *models.Project) error {
			now := time.Now()
			project.ModifiedAt = &now
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
		if status != "" && todo.GetStatus() != models.TodoStatus(status) {
			continue
		}
		if priority != "" && todo.GetPriority() != models.TodoPriority(priority) {
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
	for _, todoTag := range tags {
		if strings.ToLower(todoTag) == lowerTag {
			return true
		}
	}
	return false
}

func countByStatus(todos []*models.Todo) (openCount, doneCount int) {
	for _, todo := range todos {
		switch todo.GetStatus() {
		case models.TodoStatusOpen:
			openCount++
		case models.TodoStatusDone:
			doneCount++
		}
	}
	return
}
