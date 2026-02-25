package store

import "github.com/teanode/teanode/internal/models"

type Option struct {
	Limit  *uint64
	Offset *uint64
}

type ResolveConfigurationOptions struct {
	CLIFlags            *map[string]string
	Environment         *map[string]string
	ApplySchemaDefaults *bool
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

type MediaListOptions struct {
	UserID         *string
	ConversationID *string
	Source         *string
	ToolName       *string
}
