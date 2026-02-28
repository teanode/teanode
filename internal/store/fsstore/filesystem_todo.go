package fsstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
	"gopkg.in/yaml.v3"
)

func (self *fileSystemTransaction) ListTodos(ctx context.Context, listOptions store.TodoListOptions, options *store.Option) ([]*models.Todo, error) {
	var directory string
	if listOptions.ProjectID != nil {
		directory = self.projectTodosDirectory(*listOptions.ProjectID)
	} else if listOptions.ConversationID != nil {
		conversation, err := self.GetConversation(ctx, *listOptions.ConversationID, nil)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return []*models.Todo{}, nil
			}
			return nil, err
		}
		directory = self.conversationTodosDirectory(conversation.GetUserID(), conversation.GetAgentID(), conversation.ID)
	} else {
		return nil, store.ErrInvalidOptions
	}

	entries, readError := os.ReadDir(directory)
	if os.IsNotExist(readError) {
		return []*models.Todo{}, nil
	}
	if readError != nil {
		return nil, readError
	}

	todos := make([]*models.Todo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		filePath := directory + "/" + entry.Name()
		todo, err := readTodoFile(filePath)
		if err != nil {
			continue
		}
		todos = append(todos, todo)
	}

	sortTodos(todos)
	return applyOffsetLimit(todos, options), nil
}

func (self *fileSystemTransaction) CreateTodo(ctx context.Context, todo *models.Todo, options *store.Option) (*models.Todo, error) {
	if todo == nil {
		return nil, store.ErrInvalidOptions
	}

	todoId := todo.ID
	if todoId == "" {
		todoId = security.NewULID()
	}

	now := time.Now()
	result := *todo
	result.ID = todoId
	result.CreatedAt = &now
	result.ModifiedAt = &now

	if result.Status == nil {
		result.Status = ptrto.Value("open")
	}
	if result.Priority == nil {
		result.Priority = ptrto.Value("medium")
	}
	if result.Tags == nil {
		emptyTags := make([]string, 0)
		result.Tags = &emptyTags
	}

	filePath, err := self.resolveTodoFilePath(ctx, &result)
	if err != nil {
		return nil, err
	}

	if err := writeTodoFile(filePath, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (self *fileSystemTransaction) GetTodo(ctx context.Context, todoId string, options *store.Option) (*models.Todo, error) {
	filePath, err := self.findTodoFilePath(todoId)
	if err != nil {
		return nil, err
	}
	return readTodoFile(filePath)
}

func (self *fileSystemTransaction) ModifyTodo(ctx context.Context, todoId string, modifier func(*models.Todo) error, options *store.Option) (*models.Todo, error) {
	filePath, err := self.findTodoFilePath(todoId)
	if err != nil {
		return nil, err
	}

	todo, err := readTodoFile(filePath)
	if err != nil {
		return nil, err
	}

	if err := modifier(todo); err != nil {
		return nil, err
	}

	now := time.Now()
	todo.ModifiedAt = &now

	if err := writeTodoFile(filePath, todo); err != nil {
		return nil, err
	}
	return todo, nil
}

func (self *fileSystemTransaction) DeleteTodo(ctx context.Context, todoId string, options *store.Option) error {
	filePath, err := self.findTodoFilePath(todoId)
	if err != nil {
		return err
	}
	return trash.Move(filePath, self.trashDirectory())
}

// resolveTodoFilePath determines the file path for a new todo based on its scope.
func (self *fileSystemTransaction) resolveTodoFilePath(ctx context.Context, todo *models.Todo) (string, error) {
	if todo.ProjectID != nil && *todo.ProjectID != "" {
		return self.projectTodoFilePath(*todo.ProjectID, todo.ID), nil
	}
	if todo.ConversationID != nil && *todo.ConversationID != "" {
		conversation, err := self.GetConversation(ctx, *todo.ConversationID, nil)
		if err != nil {
			return "", err
		}
		return self.conversationTodoFilePath(conversation.GetUserID(), conversation.GetAgentID(), conversation.ID, todo.ID), nil
	}
	return "", store.ErrInvalidOptions
}

// findTodoFilePath searches for a todo file by ID across both project and conversation directories.
func (self *fileSystemTransaction) findTodoFilePath(todoId string) (string, error) {
	// Check project todos: projects/*/todos/{todoId}.yaml
	projectPattern := self.projectsDirectory() + "/*/todos/" + todoId + ".yaml"
	if matches, _ := doubleStarGlob(projectPattern); len(matches) > 0 {
		return matches[0], nil
	}

	// Check conversation todos: users/*/conversations/*/*.todos/{todoId}.yaml
	convPattern := self.usersDirectory() + "/*/conversations/*/*.todos/" + todoId + ".yaml"
	if matches, _ := doubleStarGlob(convPattern); len(matches) > 0 {
		return matches[0], nil
	}

	return "", store.ErrNotFound
}

func readTodoFile(filePath string) (*models.Todo, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	var todo models.Todo
	if err := yaml.Unmarshal(data, &todo); err != nil {
		return nil, err
	}
	return &todo, nil
}

func writeTodoFile(filePath string, todo *models.Todo) error {
	directory := filePath[:strings.LastIndex(filePath, "/")]
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(todo)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

var priorityOrder = map[string]int{"high": 0, "medium": 1, "low": 2}

func sortTodos(todos []*models.Todo) {
	sort.Slice(todos, func(i, j int) bool {
		a, b := todos[i], todos[j]
		// Open items first
		aStatus := a.GetStatus()
		bStatus := b.GetStatus()
		if aStatus != bStatus {
			return aStatus == "open"
		}
		// Then by priority (high > medium > low)
		aPri := priorityOrder[a.GetPriority()]
		bPri := priorityOrder[b.GetPriority()]
		if aPri != bPri {
			return aPri < bPri
		}
		// Then by createdAt (newest first)
		if a.CreatedAt != nil && b.CreatedAt != nil {
			return a.CreatedAt.After(*b.CreatedAt)
		}
		return a.ID > b.ID
	})
}

// doubleStarGlob wraps filepath.Glob.
func doubleStarGlob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}
