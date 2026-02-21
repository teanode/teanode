package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAllNonExistentDirectory(t *testing.T) {
	skills, err := LoadAll("/nonexistent/path")
	if err != nil {
		t.Fatalf("expected nil error for missing directory, got %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil skills, got %v", skills)
	}
}

func TestLoadAllEmptyDirectory(t *testing.T) {
	directory := t.TempDir()
	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil skills for empty directory, got %v", skills)
	}
}

func TestLoadAllSkipsNonMarkdownFiles(t *testing.T) {
	directory := t.TempDir()
	_ = os.WriteFile(filepath.Join(directory, "readme.txt"), []byte("not a skill"), 0644)
	_ = os.WriteFile(filepath.Join(directory, "legacy.yaml"), []byte("name: legacy"), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil skills, got %v", skills)
	}
}

func TestLoadAllSkipsSubdirectories(t *testing.T) {
	directory := t.TempDir()
	_ = os.MkdirAll(filepath.Join(directory, "subdir.md"), 0755)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil skills, got %v", skills)
	}
}

func TestLoadAllShellSkill(t *testing.T) {
	directory := t.TempDir()
	content := `---
name: sysinfo
description: System information tools
tools:
  - name: uptime
    description: Show system uptime
    type: shell
    command: ["uptime"]
---

Use these tools to inspect the system.
`
	_ = os.WriteFile(filepath.Join(directory, "sysinfo.md"), []byte(content), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "sysinfo" {
		t.Errorf("name = %q, want %q", skills[0].Name, "sysinfo")
	}
	if skills[0].Description != "System information tools" {
		t.Errorf("description = %q, want %q", skills[0].Description, "System information tools")
	}
	if skills[0].Prompt != "Use these tools to inspect the system." {
		t.Errorf("prompt = %q, want %q", skills[0].Prompt, "Use these tools to inspect the system.")
	}
	if len(skills[0].Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(skills[0].Tools))
	}
	tool := skills[0].Tools[0]
	if tool.Name != "uptime" {
		t.Errorf("tool name = %q, want %q", tool.Name, "uptime")
	}
	if tool.Type != "shell" {
		t.Errorf("tool type = %q, want %q", tool.Type, "shell")
	}
	if len(tool.Command) != 1 || tool.Command[0] != "uptime" {
		t.Errorf("tool command = %v, want [uptime]", tool.Command)
	}
}

func TestLoadAllHTTPSkillWithEmptyBody(t *testing.T) {
	directory := t.TempDir()
	content := `---
name: weather
tools:
  - name: get_weather
    description: Get the current weather
    type: http
    method: GET
    url: "https://api.example.com/weather?city={{city}}"
    headers:
      Authorization: "Bearer secret"
    timeout: 10
---`
	_ = os.WriteFile(filepath.Join(directory, "weather.md"), []byte(content), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Prompt != "" {
		t.Errorf("prompt = %q, want empty", skills[0].Prompt)
	}
	tool := skills[0].Tools[0]
	if tool.Type != "http" {
		t.Errorf("tool type = %q, want %q", tool.Type, "http")
	}
	if tool.Method != "GET" {
		t.Errorf("tool method = %q, want %q", tool.Method, "GET")
	}
	if tool.URL != "https://api.example.com/weather?city={{city}}" {
		t.Errorf("tool url = %q", tool.URL)
	}
	if tool.Headers["Authorization"] != "Bearer secret" {
		t.Errorf("tool header = %q, want %q", tool.Headers["Authorization"], "Bearer secret")
	}
	if tool.Timeout != 10 {
		t.Errorf("tool timeout = %d, want 10", tool.Timeout)
	}
}

func TestLoadAllParsesCRLFMarkdownFrontmatter(t *testing.T) {
	directory := t.TempDir()
	content := "---\r\nname: crlf\r\ntools:\r\n  - name: ping\r\n    description: Ping\r\n    type: shell\r\n    command: [\"echo\", \"ok\"]\r\n---\r\nPrompt line.\r\n"
	_ = os.WriteFile(filepath.Join(directory, "crlf.md"), []byte(content), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "crlf" {
		t.Fatalf("name = %q, want crlf", skills[0].Name)
	}
	if skills[0].Prompt != "Prompt line." {
		t.Fatalf("prompt = %q, want %q", skills[0].Prompt, "Prompt line.")
	}
}

func TestLoadAllWorkflowSkill(t *testing.T) {
	directory := t.TempDir()
	content := `---
name: orchestrator
tools:
  - name: check_release
    description: Run multi-step checks
    type: workflow
    steps:
      - name: ping
        type: http
        url: "https://example.com/health"
      - name: summarize
        type: shell
        command: ["echo", "health={{steps.ping}}"]
---`
	_ = os.WriteFile(filepath.Join(directory, "orchestrator.md"), []byte(content), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if len(skills[0].Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(skills[0].Tools))
	}
	tool := skills[0].Tools[0]
	if tool.Type != "workflow" {
		t.Fatalf("tool type = %q, want workflow", tool.Type)
	}
	if len(tool.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(tool.Steps))
	}
	if tool.Steps[0].Name != "ping" || tool.Steps[1].Name != "summarize" {
		t.Fatalf("unexpected step names: %#v", tool.Steps)
	}
}

func TestLoadAllSkipsIncompatibleRuntimeMinVersion(t *testing.T) {
	directory := t.TempDir()
	content := `---
name: future-skill
runtimeMinVersion: 9999.0.0
tools:
  - name: do_nothing
    description: noop
    type: shell
    command: ["echo", "ok"]
---`
	_ = os.WriteFile(filepath.Join(directory, "future.md"), []byte(content), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected incompatible skill to be skipped, got %d", len(skills))
	}
}

func TestLoadAllSkipsUnknownAuthProfileReference(t *testing.T) {
	directory := t.TempDir()
	content := `---
name: auth-skill
tools:
  - name: get_secure
    description: secure request
    type: http
    url: https://example.com
    auth: missing_profile
---`
	_ = os.WriteFile(filepath.Join(directory, "auth.md"), []byte(content), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected skill to be skipped for missing auth profile, got %d", len(skills))
	}
}

func TestLoadAllSkipsMissingName(t *testing.T) {
	directory := t.TempDir()
	content := `---
tools:
  - name: test
    description: test tool
    type: shell
    command: ["echo"]
---
`
	_ = os.WriteFile(filepath.Join(directory, "noname.md"), []byte(content), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil for skill without name, got %v", skills)
	}
}

func TestLoadAllSkipsInvalidMarkdownFrontmatter(t *testing.T) {
	directory := t.TempDir()
	_ = os.WriteFile(filepath.Join(directory, "bad.md"), []byte("name: bad\n---"), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil for invalid markdown frontmatter, got %v", skills)
	}
}

func TestLoadAllFiltersInvalidTools(t *testing.T) {
	directory := t.TempDir()
	content := `---
name: mixed
tools:
  - name: good
    description: valid tool
    type: shell
    command: ["echo", "hello"]
  - name: bad_shell
    description: missing command
    type: shell
  - name: bad_http
    description: missing url
    type: http
  - name: ""
    description: missing name
    type: shell
    command: ["echo"]
  - name: bad_type
    description: unknown type
    type: ftp
---
Prompt text.
`
	_ = os.WriteFile(filepath.Join(directory, "mixed.md"), []byte(content), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if len(skills[0].Tools) != 1 {
		t.Fatalf("expected 1 valid tool, got %d", len(skills[0].Tools))
	}
	if skills[0].Tools[0].Name != "good" {
		t.Errorf("tool name = %q, want %q", skills[0].Tools[0].Name, "good")
	}
}

func TestLoadAllSkipsSkillWithNoValidTools(t *testing.T) {
	directory := t.TempDir()
	content := `---
name: broken
tools:
  - name: bad
    description: unknown type
    type: ftp
---
`
	_ = os.WriteFile(filepath.Join(directory, "broken.md"), []byte(content), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil for skill with no valid tools, got %v", skills)
	}
}

func TestLoadAllMultipleFiles(t *testing.T) {
	directory := t.TempDir()
	skill1 := `---
name: alpha
tools:
  - name: tool_a
    description: tool A
    type: shell
    command: ["echo", "a"]
---
alpha prompt
`
	skill2 := `---
name: beta
tools:
  - name: tool_b
    description: tool B
    type: http
    url: "http://example.com"
---
beta prompt
`
	_ = os.WriteFile(filepath.Join(directory, "alpha.md"), []byte(skill1), 0644)
	_ = os.WriteFile(filepath.Join(directory, "beta.md"), []byte(skill2), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	names := map[string]bool{}
	for _, skill := range skills {
		names[skill.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("expected alpha and beta, got %v", names)
	}
}

func TestLoadAllInstalledSkillsAndLocalPrecedence(t *testing.T) {
	directory := t.TempDir()

	local := `---
name: git
tools:
  - name: local_tool
    description: local
    type: shell
    command: ["echo", "local"]
---
local prompt
`
	installedOld := `---
name: weather
tools:
  - name: weather_old
    description: old
    type: http
    url: "https://old.example"
---
old
`
	installedNew := `---
name: weather
tools:
  - name: weather_new
    description: new
    type: http
    url: "https://new.example"
---
new
`
	installedConflict := `---
name: git
tools:
  - name: installed_tool
    description: installed
    type: shell
    command: ["echo", "installed"]
---
installed prompt
`

	_ = os.WriteFile(filepath.Join(directory, "git.md"), []byte(local), 0644)
	_ = os.MkdirAll(filepath.Join(directory, ".installed", "weather", "1.0.0"), 0755)
	_ = os.MkdirAll(filepath.Join(directory, ".installed", "weather", "1.2.0"), 0755)
	_ = os.MkdirAll(filepath.Join(directory, ".installed", "git", "9.9.9"), 0755)
	_ = os.WriteFile(filepath.Join(directory, ".installed", "weather", "1.0.0", "skill.md"), []byte(installedOld), 0644)
	_ = os.WriteFile(filepath.Join(directory, ".installed", "weather", "1.2.0", "skill.md"), []byte(installedNew), 0644)
	_ = os.WriteFile(filepath.Join(directory, ".installed", "git", "9.9.9", "skill.md"), []byte(installedConflict), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills (local git + installed weather), got %d", len(skills))
	}

	byName := map[string]SkillDefinition{}
	for _, skill := range skills {
		byName[skill.Name] = skill
	}
	if byName["git"].Tools[0].Name != "local_tool" {
		t.Fatalf("expected local git skill to win, got %q", byName["git"].Tools[0].Name)
	}
	if byName["weather"].Tools[0].Name != "weather_new" {
		t.Fatalf("expected newest installed version, got %q", byName["weather"].Tools[0].Name)
	}
}

func TestValidateTool(t *testing.T) {
	tests := []struct {
		name    string
		tool    ToolDefinition
		wantErr bool
	}{
		{
			name:    "valid shell tool",
			tool:    ToolDefinition{Name: "test", Type: "shell", Command: []string{"echo"}},
			wantErr: false,
		},
		{
			name:    "valid http tool",
			tool:    ToolDefinition{Name: "test", Type: "http", URL: "http://example.com"},
			wantErr: false,
		},
		{
			name: "valid workflow tool",
			tool: ToolDefinition{
				Name: "test",
				Type: "workflow",
				Steps: []ActionDefinition{
					{Type: "shell", Command: []string{"echo", "ok"}},
				},
			},
			wantErr: false,
		},
		{
			name: "valid workflow tool with actions map",
			tool: ToolDefinition{
				Name: "test",
				Type: "workflow",
				Actions: map[string][]ActionDefinition{
					"ping": {
						{Type: "shell", Command: []string{"echo", "ok"}},
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "missing name",
			tool:    ToolDefinition{Type: "shell", Command: []string{"echo"}},
			wantErr: true,
		},
		{
			name:    "shell with empty command",
			tool:    ToolDefinition{Name: "test", Type: "shell"},
			wantErr: true,
		},
		{
			name:    "http with empty url",
			tool:    ToolDefinition{Name: "test", Type: "http"},
			wantErr: true,
		},
		{
			name:    "unknown type",
			tool:    ToolDefinition{Name: "test", Type: "ftp"},
			wantErr: true,
		},
		{
			name:    "workflow with no steps",
			tool:    ToolDefinition{Name: "test", Type: "workflow"},
			wantErr: true,
		},
		{
			name: "workflow with bad step",
			tool: ToolDefinition{
				Name: "test",
				Type: "workflow",
				Steps: []ActionDefinition{
					{Type: "http"},
				},
			},
			wantErr: true,
		},
		{
			name: "workflow with invalid onError",
			tool: ToolDefinition{
				Name: "test",
				Type: "workflow",
				Steps: []ActionDefinition{
					{Type: "shell", Command: []string{"echo", "ok"}, OnError: "ignore"},
				},
			},
			wantErr: true,
		},
		{
			name: "workflow with invalid result",
			tool: ToolDefinition{
				Name: "test",
				Type: "workflow",
				Steps: []ActionDefinition{
					{Type: "shell", Command: []string{"echo", "ok"}, Result: "yaml"},
				},
			},
			wantErr: true,
		},
		{
			name: "workflow with negative retries",
			tool: ToolDefinition{
				Name: "test",
				Type: "workflow",
				Steps: []ActionDefinition{
					{Type: "shell", Command: []string{"echo", "ok"}, Retries: -1},
				},
			},
			wantErr: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateTool(testCase.tool)
			if (err != nil) != testCase.wantErr {
				t.Errorf("validateTool() error = %v, wantErr = %v", err, testCase.wantErr)
			}
		})
	}
}
