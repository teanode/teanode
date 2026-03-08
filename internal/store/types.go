package store

import (
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/util/timeutil"
)

type Option struct {
	Limit  *uint64
	Offset *uint64
}

type WorkspaceSearchOptions struct {
	Limit          *uint64
	CaseSensitive  *bool
	PathPrefix     *string
	IncludeContent *bool
}

type WorkspaceFileSearchResult struct {
	WorkspaceFileID *string
	Scope           *models.Scope
	ScopeID         *string
	Path            *string
	MatchedLines    *[]string
}

type ConversationListOptions struct {
	UserID  *string
	AgentID *string
	Default *bool
}

type TodoListOptions struct {
	ProjectID      *string
	ConversationID *string
}

type MediaListOptions struct {
	UserID         *string
	ConversationID *string
	Source         *string
	ToolName       *string
}

type UsageListOptions struct {
	UserID       *string
	IntervalType timeutil.IntervalType
	StartedAt    time.Time
	EndedAt      time.Time
	ProviderName *string
	ModelName    *string
}

type MemoryItemListOptions struct {
	Tags            *[]string
	IncludeArchived *bool
	Limit           *uint64
}

type MemoryItemSearchOptions struct {
	Limit           *uint64
	IncludeContent  *bool
	CaseSensitive   *bool
	IncludeArchived *bool
}

type MemoryItemSearchResult struct {
	MemoryItemID *string
	Scope        *models.Scope
	ScopeID      *string
	Title        *string
	Tags         *[]string
	MatchedLines *[]string
	Score        *float64
}
