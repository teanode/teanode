package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/*
var templateFiles embed.FS

var defaultAgentMarkdown = mustReadTemplateFile("AGENT.md")
var defaultMemoryMarkdown = mustReadTemplateFile("MEMORY.md")
var defaultUserMarkdown = mustReadTemplateFile("USER.md")
var defaultOnboardingMarkdown = mustReadTemplateFile("ONBOARDING.md")
var defaultSkillsMarkdown = mustReadTemplateFile("SKILLS.md")
var defaultProjectTemplateText = mustReadTemplateFile("PROJECT.md")
var systemPromptTemplateText = mustReadTemplateFile("systemprompt.txt")

var parsedDefaultProjectTemplate = template.Must(template.New("project").Parse(defaultProjectTemplateText))

const DefaultIdentityLine = "You are a personal AI assistant running inside TeaNode."
const PreviousConversationSummaryPrefix = "Previous conversation summary:\n"
const NoPriorHistorySummary = "No prior history."

const StructuredSummarySystemPrompt = "You produce compact, faithful conversation state for long-running agents."
const StructuredSummaryResponseSchema = `{"summary":"...", "criticalFacts":{"decisions":["..."],"todos":["..."],"constraints":["..."],"userPreferences":["..."],"openQuestions":["..."]}}`
const StructuredSummaryDefaultFocus = "Preserve decisions, todos, constraints, user preferences, and open questions."

const DescriberRoutingPromptPrefix = "Write a plain-text routing description in 1-2 sentences. State your specialty, what tasks should be routed to you, and key tools. Tools: "
const SummarizerTitleAndSummarySystemPrompt = "Analyze the following conversation. Output a JSON object with two fields:\n- \"title\": a short title (max 8 words)\n- \"summary\": a 2-4 sentence summary of the main topic, key decisions, and outcomes\n\nOutput only valid JSON, nothing else."
const OnboardingSeedMessage = "Welcome to TeaNode, shall we start with onboarding?"

func mustReadTemplateFile(name string) string {
	data, err := templateFiles.ReadFile("templates/" + name)
	if err != nil {
		panic(fmt.Sprintf("missing prompt template %q: %v", name, err))
	}
	return string(data)
}

func DefaultAgentMarkdown() string {
	return defaultAgentMarkdown
}

func DefaultMemoryMarkdown() string {
	return defaultMemoryMarkdown
}

func DefaultUserMarkdown() string {
	return defaultUserMarkdown
}

func DefaultOnboardingMarkdown() string {
	return defaultOnboardingMarkdown
}

func DefaultSkillsMarkdown() string {
	return defaultSkillsMarkdown
}

func SystemPromptTemplate() string {
	return systemPromptTemplateText
}

func BuildStructuredSummaryUserPrompt(previousSummary, focus, chunkText string) string {
	var builder strings.Builder
	builder.WriteString("Summarize the provided conversation chunk. Return JSON only with this schema:\n")
	builder.WriteString(StructuredSummaryResponseSchema)
	builder.WriteString("\n\nRequirements:\n")
	builder.WriteString("- Keep summary under 500 words.\n")
	builder.WriteString("- Keep criticalFacts concise and factual.\n")
	builder.WriteString("- Include unresolved tasks in todos/openQuestions.\n")
	if strings.TrimSpace(focus) != "" {
		builder.WriteString("- Additional focus: ")
		builder.WriteString(strings.TrimSpace(focus))
		builder.WriteString("\n")
	}
	if strings.TrimSpace(previousSummary) != "" {
		builder.WriteString("\nPrevious merged summary for continuity:\n")
		builder.WriteString(strings.TrimSpace(previousSummary))
		builder.WriteString("\n")
	}
	builder.WriteString("\nConversation chunk:\n")
	builder.WriteString(chunkText)
	return builder.String()
}

func BuildDescriberSystemPrompt(identityLine, agentContent, agentMemory string) string {
	return identityLine +
		"\n\nGenerate a concise self-description for inter-agent task routing.\nUse only plain text.\n\nAGENT.md:\n" +
		agentContent +
		"\n\nMEMORY.md:\n" +
		agentMemory
}

func BuildProjectMarkdown(name, projectId, description, purpose string) (string, error) {
	data := struct {
		Name        string
		ID          string
		Description string
		Purpose     string
	}{
		Name:        name,
		ID:          projectId,
		Description: description,
		Purpose:     strings.TrimSpace(purpose),
	}
	var buffer bytes.Buffer
	if err := parsedDefaultProjectTemplate.Execute(&buffer, data); err != nil {
		return "", err
	}
	return buffer.String(), nil
}
