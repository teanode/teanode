package agent

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

const maxWorkspaceFileChars = 8000

//go:embed systemprompt.txt
var systemPromptTemplate string

var parsedSystemPrompt = template.Must(template.New("systemprompt").Parse(systemPromptTemplate))

type systemPromptData struct {
	DateTime      string
	Timezone      string
	Today         string
	Yesterday     string
	AgentsContent string
	MemoryContent string
	TodayLog      string
	YesterdayLog  string
	SkillPrompts  string
}

// BuildSystemPrompt generates the system prompt for an agent run.
// If workspaceDir is non-empty, workspace files are loaded and injected.
func BuildSystemPrompt(config *config.Config, workspaceDir string, skillPrompts string) string {
	if config.SystemPrompt != "" {
		return config.SystemPrompt
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	data := systemPromptData{
		DateTime:     now.Format("2006-01-02 15:04:05"),
		Timezone:     now.Location().String(),
		Today:        today,
		Yesterday:    yesterday,
		SkillPrompts: skillPrompts,
	}

	if workspaceDir != "" {
		data.AgentsContent = loadWorkspaceFile(workspaceDir, "AGENTS.md")
		data.MemoryContent = loadWorkspaceFile(workspaceDir, "MEMORY.md")
		data.TodayLog = loadWorkspaceFile(workspaceDir, filepath.Join("memory", today+".md"))
		data.YesterdayLog = loadWorkspaceFile(workspaceDir, filepath.Join("memory", yesterday+".md"))
	}

	var buffer bytes.Buffer
	if err := parsedSystemPrompt.Execute(&buffer, data); err != nil {
		// Fallback: return a minimal prompt if template fails.
		return "You are a personal AI assistant running inside TeaNode."
	}
	return buffer.String()
}

// loadWorkspaceFile reads a file from the workspace directory, truncating if too large.
// Returns empty string if the file doesn't exist.
func loadWorkspaceFile(workspaceDir, relPath string) string {
	full := filepath.Join(workspaceDir, relPath)
	data, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	content := string(data)
	if len(content) > maxWorkspaceFileChars {
		content = content[:maxWorkspaceFileChars] + "\n... (truncated)"
	}
	return content
}
