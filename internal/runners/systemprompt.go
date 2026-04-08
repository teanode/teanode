package runners

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"sort"
	"strings"
	"text/template"

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
	Hostname                string
	Username                string
	HomeDirectory           string
	Platform                string
	Architecture            string
}

type buildSystemPromptParameters struct {
	IdentityLine string
	AgentID      string
	OtherUsers   string
	ProjectList  string
	SkillPrompts string
	Mode         SystemPromptMode
}

// buildSystemPrompt generates the system prompt for an agent run.
func buildSystemPrompt(ctx context.Context, parameters buildSystemPromptParameters) string {
	// Resolve the identity line.
	identityLine := parameters.IdentityLine
	if identityLine == "" {
		identityLine = resolveIdentityLine(parameters.AgentID, "")
	}
	mode := normalizeSystemPromptMode(parameters.Mode)

	if mode == SystemPromptModeNone {
		return identityLine
	}

	currentUser := models.UserFromContext(ctx)
	if currentUser == nil {
		return identityLine
	}
	userId := currentUser.ID
	userRole := "user"
	if currentUser.GetAdmin() {
		userRole = "admin"
	}

	otherUsers := parameters.OtherUsers
	if otherUsers == "" {
		otherUsers = loadOtherUsers(ctx, userId)
	}
	projectList := parameters.ProjectList
	if projectList == "" {
		projectList = loadProjectList(ctx, 8)
	}

	hostname, _ := os.Hostname()
	user, _ := user.Current()
	homeDirectory := ""
	username := ""
	if user != nil {
		username = user.Username
		homeDirectory = user.HomeDir
	}

	data := systemPromptData{
		IdentityLine:    identityLine,
		MinimalMode:     mode == SystemPromptModeMinimal,
		Version:         version.Version(),
		CurrentUserID:   userId,
		CurrentUserRole: userRole,
		SkillPrompts:    parameters.SkillPrompts,
		ProjectLimit:    8,
		ProjectList:     projectList,
		OtherUsers:      otherUsers,
		Hostname:        hostname,
		Username:        username,
		HomeDirectory:   homeDirectory,
		Platform:        runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}
	data.ProfileName = currentUser.GetUsername()
	data.ProfileDescription = currentUser.GetDescription()
	data.ProfileDescriptionBlock = formatPromptMultiline(data.ProfileDescription, "  ")
	data.AgentContent = loadWorkspaceFileFromStore(ctx, models.ScopeAgent, parameters.AgentID, "AGENT.md", 8000)
	data.SkillsContent = loadWorkspaceFileFromStore(ctx, models.ScopeAgent, parameters.AgentID, "SKILLS.md", 8000)
	data.UserContent = loadWorkspaceFileFromStore(ctx, models.ScopeUser, userId, "USER.md", 8000)
	data.UserOnboarding = loadWorkspaceFileFromStore(ctx, models.ScopeUser, userId, "ONBOARDING.md", 8000)

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

func loadOtherUsers(ctx context.Context, currentUserID string) string {
	if ctx == nil {
		return ""
	}
	users := make([]*models.User, 0)
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		listedUsers, listError := transaction.ListUsers(ctx, nil)
		if listError != nil {
			return listError
		}
		users = listedUsers
		return nil
	}); err != nil {
		return ""
	}

	filteredUsers := make([]*models.User, 0, len(users))
	for _, user := range users {
		if user.ID == currentUserID {
			continue
		}
		filteredUsers = append(filteredUsers, user)
	}
	if len(filteredUsers) == 0 {
		return ""
	}

	sort.Slice(filteredUsers, func(leftIndex int, rightIndex int) bool {
		return filteredUsers[leftIndex].ID < filteredUsers[rightIndex].ID
	})
	lines := make([]string, 0, len(filteredUsers))
	for _, user := range filteredUsers {
		username := user.GetUsername()
		if username == "" {
			username = user.ID
		}
		role := "user"
		if user.GetAdmin() {
			role = "admin"
		}
		description := user.GetDescription()
		if description == "" {
			description = "No description provided."
		}
		lines = append(lines, fmt.Sprintf("- %s (userId: %s, role: %s)\n  description:\n%s", username, user.ID, role, formatPromptMultiline(description, "    ")))
	}
	return strings.Join(lines, "\n")
}

func formatPromptMultiline(text, indent string) string {
	if text == "" {
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
	items := make([]*models.Project, 0)
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		projects, listError := transaction.ListProjects(ctx, nil)
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

func formatProjectList(items []*models.Project, limit int) string {
	if len(items) == 0 {
		return ""
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		name := item.GetName()
		if name == "" {
			continue
		}
		description := item.GetDescription()
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
func resolveIdentityLine(agentId string, agentName string) string {
	return fmt.Sprintf("%s %s", prompts.DefaultIdentityLine, agentIdentitySuffix(agentId, agentName))
}

// agentIdentitySuffix returns a sentence fragment identifying the agent by name
// and ID or just by ID when no friendly name is set.
func agentIdentitySuffix(agentId string, agentName string) string {
	if agentName != "" {
		return fmt.Sprintf("You are '%s' (agent: %s).", agentName, agentId)
	}
	return fmt.Sprintf("You are the '%s' agent.", agentId)
}

func loadWorkspaceFileFromStore(ctx context.Context, scope models.Scope, scopeId string, relativePath string, maxChars int) string {
	if ctx == nil {
		return ""
	}
	content := ""
	_ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		file, err := transaction.GetWorkspaceFileByPath(ctx, scope, scopeId, relativePath, nil)
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
