package agents

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/prompts"
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
	Configuration           *configs.Config
	AgentID                 string
	CurrentUserID           string
	AgentWorkspaceDirectory string
	UserWorkspaceDirectory  string
	SkillPrompts            string
	MaxWorkspaceFileChars   int
	Profile                 *configs.UserConfig
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
		IdentityLine:  identityLine,
		MinimalMode:   mode == SystemPromptModeMinimal,
		Version:       version.Version(),
		CurrentUserID: strings.TrimSpace(parameters.CurrentUserID),
		SkillPrompts:  parameters.SkillPrompts,
		ProjectLimit:  8,
		ProjectList:   loadProjectList(8),
	}

	if securityConfig, err := configs.LoadSecurity(); err == nil && securityConfig != nil {
		data.CurrentUserRole = "user"
		if securityConfig.IsAdmin(data.CurrentUserID) {
			data.CurrentUserRole = "admin"
		}
		data.OtherUsers = loadOtherUsers(securityConfig, data.CurrentUserID)
	}
	if parameters.Profile != nil {
		data.ProfileName = strings.TrimSpace(parameters.Profile.Name)
		data.ProfileDescription = strings.TrimSpace(parameters.Profile.Description)
		data.ProfileDescriptionBlock = formatPromptMultiline(data.ProfileDescription, "  ")
	}

	if parameters.AgentWorkspaceDirectory != "" {
		data.AgentContent = loadWorkspaceFile(parameters.AgentWorkspaceDirectory, "AGENT.md", parameters.MaxWorkspaceFileChars)
		data.SkillsContent = loadWorkspaceFile(parameters.AgentWorkspaceDirectory, "SKILLS.md", parameters.MaxWorkspaceFileChars)
	}
	if parameters.UserWorkspaceDirectory != "" {
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

func loadOtherUsers(securityConfig *configs.SecurityConfig, currentUserId string) string {
	if securityConfig == nil || len(securityConfig.Users) == 0 {
		return ""
	}
	userIds := make([]string, 0, len(securityConfig.Users))
	for userId := range securityConfig.Users {
		if userId == currentUserId {
			continue
		}
		userIds = append(userIds, userId)
	}
	if len(userIds) == 0 {
		return ""
	}
	sort.Strings(userIds)
	lines := make([]string, 0, len(userIds))
	for _, userId := range userIds {
		user := securityConfig.Users[userId]
		username := strings.TrimSpace(user.Username)
		role := "user"
		if user.Admin {
			role = "admin"
		}
		description := "No description provided."
		if profile, err := configs.LoadUserConfig(userId); err == nil {
			if parsed := strings.TrimSpace(profile.Description); parsed != "" {
				description = parsed
			}
		}
		lines = append(lines, fmt.Sprintf("- %s (userId: %s, role: %s)\n  description:\n%s", username, userId, role, formatPromptMultiline(description, "    ")))
	}
	return strings.Join(lines, "\n")
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

func loadProjectList(limit int) string {
	items, err := configs.LoadProjectConfigs()
	if err != nil || len(items) == 0 {
		return ""
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		description := strings.TrimSpace(item.Description)
		if description == "" {
			description = "No description available."
		}
		updatedAt := "unknown"
		if !item.UpdatedAt.IsZero() {
			updatedAt = item.UpdatedAt.String()
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
