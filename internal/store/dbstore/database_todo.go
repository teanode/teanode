package dbstore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

type databaseTodoRecord struct {
	ID             string     `gorm:"column:id;type:varchar(32);primaryKey"`
	ProjectID      *string    `gorm:"column:project_id;type:varchar(32)"`
	ConversationID *string    `gorm:"column:conversation_id;type:varchar(32)"`
	Title          string     `gorm:"column:title;type:varchar(512);not null"`
	Description    *string    `gorm:"column:description;type:text"`
	Status         string     `gorm:"column:status;type:varchar(16);not null"`
	Priority       string     `gorm:"column:priority;type:varchar(16);not null"`
	Tags           []byte     `gorm:"column:tags;type:jsonb;not null"`
	CompletedAt    *time.Time `gorm:"column:completed_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null"`
	ModifiedAt     time.Time  `gorm:"column:modified_at;not null"`
}

func (self databaseTodoRecord) TableName() string {
	return "todos"
}

func (self *databaseTransaction) ListTodos(ctx context.Context, listOptions store.TodoListOptions, options *store.Option) ([]*models.Todo, error) {
	if listOptions.ProjectID == nil && listOptions.ConversationID == nil {
		return nil, store.ErrInvalidOptions
	}
	query := self.database.Model(&databaseTodoRecord{})
	if listOptions.ProjectID != nil {
		query = query.Where("project_id = ?", *listOptions.ProjectID)
	}
	if listOptions.ConversationID != nil {
		query = query.Where("conversation_id = ?", *listOptions.ConversationID)
	}
	query = applyOption(query.Order("CASE status WHEN 'open' THEN 0 ELSE 1 END, CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END, created_at DESC"), options)
	records := make([]databaseTodoRecord, 0)
	if err := query.Find(&records).Error; err != nil {
		return nil, databaseError(err)
	}
	todos := make([]*models.Todo, 0, len(records))
	for _, record := range records {
		todos = append(todos, todoRecordToModel(&record))
	}
	return todos, nil
}

func (self *databaseTransaction) CreateTodo(ctx context.Context, todo *models.Todo, options *store.Option) (*models.Todo, error) {
	if todo == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToTodoRecord(todo)
	if record.ID == "" {
		record.ID = security.NewULID()
	}
	now := *ptrto.TimeNowInLocal()
	record.CreatedAt = now
	record.ModifiedAt = now
	if record.Status == "" {
		record.Status = string(models.TodoStatusOpen)
	}
	if record.Priority == "" {
		record.Priority = string(models.TodoPriorityMedium)
	}
	if record.Tags == nil {
		record.Tags = []byte("[]")
	}
	if err := self.database.Create(record).Error; err != nil {
		return nil, databaseError(err)
	}
	return self.GetTodo(ctx, record.ID, options)
}

func (self *databaseTransaction) GetTodo(ctx context.Context, todoId string, options *store.Option) (*models.Todo, error) {
	record := &databaseTodoRecord{}
	if err := self.database.Where("id = ?", todoId).Take(record).Error; err != nil {
		return nil, databaseError(err)
	}
	return todoRecordToModel(record), nil
}

func (self *databaseTransaction) ModifyTodo(ctx context.Context, todoId string, modifier func(*models.Todo) error, options *store.Option) (*models.Todo, error) {
	todo, err := self.GetTodo(ctx, todoId, options)
	if err != nil {
		return nil, err
	}
	if err := modifier(todo); err != nil {
		return nil, err
	}
	record := modelToTodoRecord(todo)
	record.ID = todoId
	record.ModifiedAt = *ptrto.TimeNowInLocal()
	updateError := self.database.Model(&databaseTodoRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"title":        record.Title,
		"description":  record.Description,
		"status":       record.Status,
		"priority":     record.Priority,
		"tags":         record.Tags,
		"completed_at": record.CompletedAt,
		"modified_at":  record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetTodo(ctx, todoId, options)
}

func (self *databaseTransaction) DeleteTodo(ctx context.Context, todoId string, options *store.Option) error {
	result := self.database.Where("id = ?", todoId).Delete(&databaseTodoRecord{})
	if result.Error != nil {
		return databaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func todoRecordToModel(record *databaseTodoRecord) *models.Todo {
	todo := &models.Todo{
		ID:             record.ID,
		ProjectID:      record.ProjectID,
		ConversationID: record.ConversationID,
		Title:          ptrto.Value(record.Title),
		Description:    record.Description,
		Status:         ptrto.Value(models.TodoStatus(record.Status)),
		Priority:       ptrto.Value(models.TodoPriority(record.Priority)),
		CompletedAt:    record.CompletedAt,
		CreatedAt:      &record.CreatedAt,
		ModifiedAt:     &record.ModifiedAt,
	}
	if record.Tags != nil {
		var tags []string
		if err := json.Unmarshal(record.Tags, &tags); err == nil {
			todo.Tags = &tags
		}
	}
	if todo.Tags == nil {
		emptyTags := make([]string, 0)
		todo.Tags = &emptyTags
	}
	return todo
}

func modelToTodoRecord(todo *models.Todo) *databaseTodoRecord {
	record := &databaseTodoRecord{
		ID:             todo.ID,
		ProjectID:      todo.ProjectID,
		ConversationID: todo.ConversationID,
		Title:          todo.GetTitle(),
		Description:    todo.Description,
		Status:         string(todo.GetStatus()),
		Priority:       string(todo.GetPriority()),
		CompletedAt:    todo.CompletedAt,
	}
	if todo.CreatedAt != nil {
		record.CreatedAt = *todo.CreatedAt
	}
	if todo.ModifiedAt != nil {
		record.ModifiedAt = *todo.ModifiedAt
	}
	if todo.Tags != nil {
		tagsJSON, err := json.Marshal(*todo.Tags)
		if err == nil {
			record.Tags = tagsJSON
		}
	}
	if record.Tags == nil {
		record.Tags = []byte("[]")
	}
	return record
}
