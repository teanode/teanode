package skills

// SkillDefinition is the YAML structure of a skill file.
type SkillDefinition struct {
	Name        string           `json:"name" yaml:"name"`
	Description string           `json:"description,omitempty" yaml:"description,omitempty"`
	Prompt      string           `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	Tools       []ToolDefinition `json:"tools" yaml:"tools"`
}

// ToolDefinition is one tool inside a skill.
type ToolDefinition struct {
	Name        string      `json:"name" yaml:"name"`
	Description string      `json:"description" yaml:"description"`
	Type        string      `json:"type" yaml:"type"`             // "shell" or "http"
	Parameters  interface{} `json:"parameters" yaml:"parameters"` // JSON schema for LLM

	// Shell fields
	Command          []string `json:"command,omitempty" yaml:"command,omitempty"` // command + args
	WorkingDirectory string   `json:"workdir,omitempty" yaml:"workdir,omitempty"` // working directory

	// HTTP fields
	Method  string            `json:"method,omitempty" yaml:"method,omitempty"` // GET, POST, etc.
	URL     string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body    string            `json:"body,omitempty" yaml:"body,omitempty"` // template for request body

	// Common
	Timeout int `json:"timeout,omitempty" yaml:"timeout,omitempty"` // seconds, default 30
}
