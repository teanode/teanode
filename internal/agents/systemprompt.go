package agents

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"text/template"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/version"
)

const defaultIdentityLine = "You are a personal AI assistant running inside TeaNode."

//go:embed systemprompt.txt
var systemPromptTemplate string

var parsedSystemPrompt = template.Must(template.New("systemprompt").Parse(systemPromptTemplate))

type systemPromptData struct {
	IdentityLine  string
	Version       string
	Date          string
	Timezone      string
	Username      string
	HomeDirectory string
	AgentContent  string
	MemoryContent string
	SkillsContent string
	SkillPrompts  string
}

// BuildSystemPrompt generates the system prompt for an agent run.
// If workspaceDirectory is non-empty, workspace files are loaded and injected.
// maxWorkspaceFileChars controls the per-file truncation limit.
func BuildSystemPrompt(configuration *configs.Config, agentId string, workspaceDirectory string, skillPrompts string, maxWorkspaceFileChars int) string {
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
		HomeDirectory: homeDir,
		SkillPrompts:  skillPrompts,
	}

	if workspaceDirectory != "" {
		data.AgentContent = loadWorkspaceFile(workspaceDirectory, "AGENT.md", maxWorkspaceFileChars)
		data.MemoryContent = loadWorkspaceFile(workspaceDirectory, "MEMORY.md", maxWorkspaceFileChars)
		data.SkillsContent = loadWorkspaceFile(workspaceDirectory, "SKILLS.md", maxWorkspaceFileChars)
	}

	var buffer bytes.Buffer
	if err := parsedSystemPrompt.Execute(&buffer, data); err != nil {
		// Fallback: return a minimal prompt if template fails.
		return defaultIdentityLine
	}
	return buffer.String()
}

// resolveIdentityLine determines the identity line for the system prompt.
// Priority: per-agent SystemPrompt > default + agent suffix.
func resolveIdentityLine(configuration *configs.Config, agentId string) string {
	// Check per-agent SystemPrompt.
	if agentConfig := configuration.AgentByID(agentId); agentConfig != nil && agentConfig.SystemPrompt != "" {
		return agentConfig.SystemPrompt
	}

	// Default identity.
	return fmt.Sprintf("%s %s", defaultIdentityLine, agentIdentitySuffix(configuration, agentId))
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
