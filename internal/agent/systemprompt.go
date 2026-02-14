package agent

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/teanode/teanode/internal/config"
)

const defaultIdentityLine = "You are a personal AI assistant running inside TeaNode."

//go:embed systemprompt.txt
var systemPromptTemplate string

var parsedSystemPrompt = template.Must(template.New("systemprompt").Parse(systemPromptTemplate))

type systemPromptData struct {
	IdentityLine  string
	DateTime      string
	Timezone      string
	Today         string
	Yesterday     string
	AgentContent  string
	MemoryContent string
	TodayLog      string
	YesterdayLog  string
	SkillPrompts  string
}

// BuildSystemPrompt generates the system prompt for an agent run.
// If workspaceDir is non-empty, workspace files are loaded and injected.
// maxWorkspaceFileChars controls the per-file truncation limit.
func BuildSystemPrompt(configuration *config.Config, agentId string, workspaceDir string, skillPrompts string, maxWorkspaceFileChars int) string {
	// Resolve the identity line.
	identityLine := resolveIdentityLine(configuration, agentId)

	now := time.Now()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	data := systemPromptData{
		IdentityLine: identityLine,
		DateTime:     now.Format("2006-01-02 15:04:05"),
		Timezone:     now.Location().String(),
		Today:        today,
		Yesterday:    yesterday,
		SkillPrompts: skillPrompts,
	}

	if workspaceDir != "" {
		data.AgentContent = loadWorkspaceFile(workspaceDir, "AGENT.md", maxWorkspaceFileChars)
		data.MemoryContent = loadWorkspaceFile(workspaceDir, "MEMORY.md", maxWorkspaceFileChars)
		data.TodayLog = loadWorkspaceFile(workspaceDir, filepath.Join("memory", today+".md"), maxWorkspaceFileChars)
		data.YesterdayLog = loadWorkspaceFile(workspaceDir, filepath.Join("memory", yesterday+".md"), maxWorkspaceFileChars)
	}

	var buffer bytes.Buffer
	if err := parsedSystemPrompt.Execute(&buffer, data); err != nil {
		// Fallback: return a minimal prompt if template fails.
		return defaultIdentityLine
	}
	return buffer.String()
}

// resolveIdentityLine determines the identity line for the system prompt.
// Priority: per-agent SystemPrompt > global Config.SystemPrompt > default.
// For non-"main" agents without a custom SystemPrompt, appends agent identity.
func resolveIdentityLine(configuration *config.Config, agentId string) string {
	// Check per-agent SystemPrompt.
	if agentConfig := configuration.AgentByID(agentId); agentConfig != nil && agentConfig.SystemPrompt != "" {
		return agentConfig.SystemPrompt
	}

	// Check global SystemPrompt.
	if configuration.SystemPrompt != "" {
		if agentId != "" && agentId != config.DefaultAgentID {
			return fmt.Sprintf("%s You are the '%s' agent.", configuration.SystemPrompt, agentId)
		}
		return configuration.SystemPrompt
	}

	// Default identity.
	if agentId != "" && agentId != config.DefaultAgentID {
		return fmt.Sprintf("%s You are the '%s' agent.", defaultIdentityLine, agentId)
	}
	return defaultIdentityLine
}

// loadWorkspaceFile reads a file from the workspace directory, truncating if too large.
// Returns empty string if the file doesn't exist.
func loadWorkspaceFile(workspaceDir, relPath string, maxChars int) string {
	full := filepath.Join(workspaceDir, relPath)
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
