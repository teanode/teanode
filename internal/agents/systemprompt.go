package agents

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/teanode/teanode/internal/configs"
	projectstore "github.com/teanode/teanode/internal/projects"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/version"
)

var parsedSystemPrompt = template.Must(template.New("systemprompt").Parse(prompts.SystemPromptTemplate()))

type systemPromptData struct {
	IdentityLine            string
	Version                 string
	Date                    string
	Timezone                string
	Username                string
	CurrentUserID           string
	CurrentUserRole         string
	ProfileName             string
	ProfileDescription      string
	ProfileDescriptionBlock string
	HomeDirectory           string
	AgentContent            string
	AgentMemory             string
	UserContent             string
	UserMemory              string
	UserOnboarding          string
	SkillsContent           string
	SkillPrompts            string
	ProjectList             string
	ProjectLimit            int
	OtherUsers              string
}

// BuildSystemPrompt generates the system prompt for an agent run.
// If workspaceDirectory is non-empty, workspace files are loaded and injected.
// maxWorkspaceFileChars controls the per-file truncation limit.
func BuildSystemPrompt(
	configuration *configs.Config,
	agentId string,
	currentUserId string,
	agentWorkspaceDirectory string,
	userWorkspaceDirectory string,
	skillPrompts string,
	maxWorkspaceFileChars int,
	profile *configs.UserProfile,
) string {
	// Resolve the identity line.
	identityLine := resolveIdentityLine(configuration, agentId)

	homeDir, _ := os.UserHomeDir()
	username := ""
	if u, err := user.Current(); err == nil {
		username = u.Username
	}

	now := time.Now()

	data := systemPromptData{
		IdentityLine:  identityLine,
		Version:       version.Version(),
		Date:          now.Format("2006-01-02"),
		Timezone:      now.Format("MST"),
		Username:      username,
		CurrentUserID: strings.TrimSpace(currentUserId),
		HomeDirectory: homeDir,
		SkillPrompts:  skillPrompts,
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
	if profile != nil {
		data.ProfileName = strings.TrimSpace(profile.Name)
		data.ProfileDescription = strings.TrimSpace(profile.Description)
		data.ProfileDescriptionBlock = formatPromptMultiline(data.ProfileDescription, "  ")
	}

	if agentWorkspaceDirectory != "" {
		data.AgentContent = loadWorkspaceFile(agentWorkspaceDirectory, "AGENT.md", maxWorkspaceFileChars)
		data.AgentMemory = loadWorkspaceFile(agentWorkspaceDirectory, "MEMORY.md", maxWorkspaceFileChars)
		data.SkillsContent = loadWorkspaceFile(agentWorkspaceDirectory, "SKILLS.md", maxWorkspaceFileChars)
	}
	if userWorkspaceDirectory != "" {
		data.UserContent = loadWorkspaceFile(userWorkspaceDirectory, "USER.md", maxWorkspaceFileChars)
		data.UserMemory = loadWorkspaceFile(userWorkspaceDirectory, "MEMORY.md", maxWorkspaceFileChars)
		data.UserOnboarding = loadWorkspaceFile(userWorkspaceDirectory, "ONBOARDING.md", maxWorkspaceFileChars)
	}

	var buffer bytes.Buffer
	if err := parsedSystemPrompt.Execute(&buffer, data); err != nil {
		// Fallback: return a minimal prompt if template fails.
		return prompts.DefaultIdentityLine
	}
	return buffer.String()
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
		if profile, err := configs.LoadUserProfile(userId); err == nil {
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
	items, err := projectstore.List()
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
