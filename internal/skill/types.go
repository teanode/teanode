package skill

// SkillDef is the JSON structure of a skill file.
type SkillDef struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Prompt      string    `json:"prompt,omitempty"`
	Tools       []ToolDef `json:"tools"`
}

// ToolDef is one tool inside a skill.
type ToolDef struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Type        string            `json:"type"`              // "shell" or "http"
	Parameters  interface{}       `json:"parameters"`        // JSON schema for LLM

	// Shell fields
	Command []string `json:"command,omitempty"` // command + args
	WorkingDirectory string   `json:"workdir,omitempty"` // working directory

	// HTTP fields
	Method  string            `json:"method,omitempty"` // GET, POST, etc.
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"` // template for request body

	// Common
	Timeout int `json:"timeout,omitempty"` // seconds, default 30
}
