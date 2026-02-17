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

func TestLoadAllSkipsNonYAMLFiles(t *testing.T) {
	directory := t.TempDir()
	os.WriteFile(filepath.Join(directory, "readme.txt"), []byte("not a skill"), 0644)
	os.WriteFile(filepath.Join(directory, "config.json"), []byte("{}"), 0644)

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
	os.MkdirAll(filepath.Join(directory, "subdir.yaml"), 0755)

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
	content := `
name: sysinfo
description: System information tools
prompt: Use these tools to inspect the system.
tools:
  - name: uptime
    description: Show system uptime
    type: shell
    command: ["uptime"]
`
	os.WriteFile(filepath.Join(directory, "sysinfo.yaml"), []byte(content), 0644)

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

func TestLoadAllHTTPSkill(t *testing.T) {
	directory := t.TempDir()
	content := `
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
`
	os.WriteFile(filepath.Join(directory, "weather.yaml"), []byte(content), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
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

func TestLoadAllSkipsMissingName(t *testing.T) {
	directory := t.TempDir()
	content := `
tools:
  - name: test
    description: test tool
    type: shell
    command: ["echo"]
`
	os.WriteFile(filepath.Join(directory, "noname.yaml"), []byte(content), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil for skill without name, got %v", skills)
	}
}

func TestLoadAllSkipsInvalidYAML(t *testing.T) {
	directory := t.TempDir()
	os.WriteFile(filepath.Join(directory, "bad.yaml"), []byte("{{{{not yaml"), 0644)

	skills, err := LoadAll(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil for invalid YAML, got %v", skills)
	}
}

func TestLoadAllFiltersInvalidTools(t *testing.T) {
	directory := t.TempDir()
	content := `
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
`
	os.WriteFile(filepath.Join(directory, "mixed.yaml"), []byte(content), 0644)

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
	content := `
name: broken
tools:
  - name: bad
    description: unknown type
    type: ftp
`
	os.WriteFile(filepath.Join(directory, "broken.yaml"), []byte(content), 0644)

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
	skill1 := `
name: alpha
tools:
  - name: tool_a
    description: tool A
    type: shell
    command: ["echo", "a"]
`
	skill2 := `
name: beta
tools:
  - name: tool_b
    description: tool B
    type: http
    url: "http://example.com"
`
	os.WriteFile(filepath.Join(directory, "alpha.yaml"), []byte(skill1), 0644)
	os.WriteFile(filepath.Join(directory, "beta.yaml"), []byte(skill2), 0644)

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
