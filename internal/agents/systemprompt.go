package agents

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/version"
)

var parsedSystemPrompt = template.Must(template.New("systemprompt").Parse(prompts.SystemPromptTemplate()))

type SystemPromptMode string

const (
	SystemPromptModeFull    SystemPromptMode = "full"
	SystemPromptModeMinimal SystemPromptMode = "minimal"
	SystemPromptModeNone    SystemPromptMode = "none"
)

type systemPromptData struct {
	IdentityLine            string
	MinimalMode             bool
	Version                 string
	CurrentUserID           string
	CurrentUserRole         string
	ProfileName             string
	ProfileDescription      string
	ProfileDescriptionBlock string
	AgentContent            string
	UserContent             string
	UserOnboarding          string
	SkillsContent           string
	SkillPrompts            string
	ProjectList             string
	ProjectLimit            int
	OtherUsers              string
}

type buildSystemPromptParameters struct {
	Context                 context.Context
	Configuration           *configs.Config
	AgentID                 string
	CurrentUserID           string
	CurrentUserRole         string
	OtherUsers              string
	ProjectList             string
	AgentWorkspaceScopeID   string
	UserWorkspaceScopeID    string
	AgentWorkspaceDirectory string
	UserWorkspaceDirectory  string
	SkillPrompts            string
	MaxWorkspaceFileChars   int
	Profile                 *models.User
	Mode                    SystemPromptMode
}

// buildSystemPrompt generates the system prompt for an agent run.
// If workspace directories are non-empty, workspace files are loaded and injected.
// maxWorkspaceFileChars controls the per-file truncation limit.
func buildSystemPrompt(parameters buildSystemPromptParameters) string {
	// Resolve the identity line.
	identityLine := resolveIdentityLine(parameters.Configuration, parameters.AgentID)
	mode := normalizeSystemPromptMode(parameters.Mode)

	if mode == SystemPromptModeNone {
		return identityLine
	}

	data := systemPromptData{
		IdentityLine:    identityLine,
		MinimalMode:     mode == SystemPromptModeMinimal,
		Version:         version.Version(),
		CurrentUserID:   strings.TrimSpace(parameters.CurrentUserID),
		SkillPrompts:    parameters.SkillPrompts,
		ProjectLimit:    8,
		ProjectList:     strings.TrimSpace(parameters.ProjectList),
		CurrentUserRole: strings.TrimSpace(parameters.CurrentUserRole),
		OtherUsers:      strings.TrimSpace(parameters.OtherUsers),
	}
	if data.ProjectList == "" {
		data.ProjectList = loadProjectList(parameters.Context, 8)
	}

	if data.CurrentUserRole == "" || data.OtherUsers == "" {
		currentUserRole, otherUsers := loadCurrentUserRoleAndOtherUsers(parameters.Context, data.CurrentUserID)
		if data.CurrentUserRole == "" {
			data.CurrentUserRole = currentUserRole
		}
		if data.OtherUsers == "" {
			data.OtherUsers = otherUsers
		}
	}
	if data.CurrentUserRole == "" {
		data.CurrentUserRole = "user"
	}
	if parameters.Profile != nil {
		data.ProfileName = strings.TrimSpace(valueOrEmptyString(parameters.Profile.Username))
		data.ProfileDescription = strings.TrimSpace(valueOrEmptyString(parameters.Profile.Description))
		data.ProfileDescriptionBlock = formatPromptMultiline(data.ProfileDescription, "  ")
	}

	agentScopeId := strings.TrimSpace(parameters.AgentWorkspaceScopeID)
	userScopeId := strings.TrimSpace(parameters.UserWorkspaceScopeID)
	if agentScopeId != "" {
		data.AgentContent = loadWorkspaceFileFromStore(parameters.Context, models.ScopeAgent, agentScopeId, "AGENT.md", parameters.MaxWorkspaceFileChars)
		data.SkillsContent = loadWorkspaceFileFromStore(parameters.Context, models.ScopeAgent, agentScopeId, "SKILLS.md", parameters.MaxWorkspaceFileChars)
	} else if parameters.AgentWorkspaceDirectory != "" {
		data.AgentContent = loadWorkspaceFile(parameters.AgentWorkspaceDirectory, "AGENT.md", parameters.MaxWorkspaceFileChars)
		data.SkillsContent = loadWorkspaceFile(parameters.AgentWorkspaceDirectory, "SKILLS.md", parameters.MaxWorkspaceFileChars)
	}
	if userScopeId != "" {
		data.UserContent = loadWorkspaceFileFromStore(parameters.Context, models.ScopeUser, userScopeId, "USER.md", parameters.MaxWorkspaceFileChars)
		data.UserOnboarding = loadWorkspaceFileFromStore(parameters.Context, models.ScopeUser, userScopeId, "ONBOARDING.md", parameters.MaxWorkspaceFileChars)
	} else if parameters.UserWorkspaceDirectory != "" {
		data.UserContent = loadWorkspaceFile(parameters.UserWorkspaceDirectory, "USER.md", parameters.MaxWorkspaceFileChars)
		data.UserOnboarding = loadWorkspaceFile(parameters.UserWorkspaceDirectory, "ONBOARDING.md", parameters.MaxWorkspaceFileChars)
	}

	var buffer bytes.Buffer
	if err := parsedSystemPrompt.Execute(&buffer, data); err != nil {
		// Fallback: return a minimal prompt if template fails.
		return prompts.DefaultIdentityLine
	}
	return buffer.String()
}

func normalizeSystemPromptMode(mode SystemPromptMode) SystemPromptMode {
	switch mode {
	case SystemPromptModeFull, SystemPromptModeMinimal, SystemPromptModeNone:
		return mode
	default:
		return SystemPromptModeFull
	}
}

func loadCurrentUserRoleAndOtherUsers(ctx context.Context, currentUserID string) (string, string) {
	currentUserRole := "user"
	if ctx == nil {
		return currentUserRole, ""
	}
	users := make([]models.User, 0)
	if err := store.StoreFromContext(ctx).Transaction(func(transaction store.Transaction) error {
		listedUsers, listError := transaction.ListUsers(nil)
		if listError != nil {
			return listError
		}
		users = listedUsers
		return nil
	}); err != nil {
		return currentUserRole, ""
	}

	filteredUsers := make([]models.User, 0, len(users))
	for _, user := range users {
		if strings.TrimSpace(user.ID) == strings.TrimSpace(currentUserID) {
			if user.Admin != nil && *user.Admin {
				currentUserRole = "admin"
			}
			continue
		}
		filteredUsers = append(filteredUsers, user)
	}
	if len(filteredUsers) == 0 {
		return currentUserRole, ""
	}

	sort.Slice(filteredUsers, func(leftIndex int, rightIndex int) bool {
		return filteredUsers[leftIndex].ID < filteredUsers[rightIndex].ID
	})
	lines := make([]string, 0, len(filteredUsers))
	for _, user := range filteredUsers {
		username := strings.TrimSpace(valueOrEmptyString(user.Username))
		if username == "" {
			username = user.ID
		}
		role := "user"
		if user.Admin != nil && *user.Admin {
			role = "admin"
		}
		description := strings.TrimSpace(valueOrEmptyString(user.Description))
		if description == "" {
			description = "No description provided."
		}
		lines = append(lines, fmt.Sprintf("- %s (userId: %s, role: %s)\n  description:\n%s", username, user.ID, role, formatPromptMultiline(description, "    ")))
	}
	return currentUserRole, strings.Join(lines, "\n")
}

func formatPromptMultiline(text, indent string) string {
	if strings.TrimSpace(text) == "" {
		return indent
	}
	lines := strings.Split(text, "\n")
	for index := range lines {
		lines[index] = indent + strings.TrimRight(lines[index], " \t")
	}
	return strings.Join(lines, "\n")
}

func loadProjectList(ctx context.Context, limit int) string {
	if ctx == nil {
		return ""
	}
	items := make([]models.Project, 0)
	if err := store.StoreFromContext(ctx).Transaction(func(transaction store.Transaction) error {
		projects, listError := transaction.ListProjects(nil)
		if listError != nil {
			return listError
		}
		items = projects
		return nil
	}); err != nil {
		return ""
	}
	return formatProjectList(items, limit)
}

func formatProjectList(items []models.Project, limit int) string {
	if len(items) == 0 {
		return ""
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(valueOrEmptyString(item.Name))
		if name == "" {
			continue
		}
		description := strings.TrimSpace(valueOrEmptyString(item.Description))
		if description == "" {
			description = "No description available."
		}
		updatedAt := "unknown"
		if item.ModifiedAt != nil {
			updatedAt = item.ModifiedAt.String()
		}
		lines = append(lines, fmt.Sprintf("- %s (projectId: %s, updatedAt: %s): %s", name, item.ID, updatedAt, description))
	}
	return strings.Join(lines, "\n")
}

// resolveIdentityLine determines the identity line for the system prompt.
func resolveIdentityLine(configuration *configs.Config, agentId string) string {
	return fmt.Sprintf("%s %s", prompts.DefaultIdentityLine, agentIdentitySuffix(configuration, agentId))
}

// agentIdentitySuffix returns a sentence fragment identifying the agent by name
// and ID (e.g. "You are 'Research Assistant' (agent: research).") or just by ID
// when no friendly name is set.
func agentIdentitySuffix(configuration *configs.Config, agentId string) string {
	if agentConfig := configuration.AgentByID(agentId); agentConfig != nil && agentConfig.Name != "" {
		return fmt.Sprintf("You are '%s' (agent: %s).", agentConfig.Name, agentId)
	}
	return fmt.Sprintf("You are the '%s' agent.", agentId)
}

// loadWorkspaceFile reads a file from the workspace directory, truncating if too large.
// Returns empty string if the file doesn't exist.
func loadWorkspaceFile(workspaceDirectory, relPath string, maxChars int) string {
	full := filepath.Join(workspaceDirectory, relPath)
	data, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	content := string(data)
	if len(content) > maxChars {
		content = content[:maxChars] + "\n... (truncated)"
	}
	return content
}

func loadWorkspaceFileFromStore(ctx context.Context, scope models.Scope, scopeId string, relativePath string, maxChars int) string {
	if ctx == nil {
		return ""
	}
	content := ""
	_ = store.StoreFromContext(ctx).Transaction(func(transaction store.Transaction) error {
		file, err := transaction.GetWorkspaceFileByPath(scope, scopeId, relativePath, nil)
		if err != nil || file == nil || file.Content == nil {
			return nil
		}
		content = string(*file.Content)
		return nil
	})
	if len(content) > maxChars {
		content = content[:maxChars] + "\n... (truncated)"
	}
	return content
}
