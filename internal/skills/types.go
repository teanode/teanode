package skills

// SkillDefinition is the frontmatter structure of a markdown skill file.
type SkillDefinition struct {
	Name              string                     `json:"name" yaml:"name"`
	Description       string                     `json:"description,omitempty" yaml:"description,omitempty"`
	RuntimeMinVersion string                     `json:"runtimeMinVersion,omitempty" yaml:"runtimeMinVersion,omitempty"`
	HTTPAuth          map[string]HTTPAuthProfile `json:"httpAuth,omitempty" yaml:"httpAuth,omitempty"`
	Prompt            string                     `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	Tools             []ToolDefinition           `json:"tools" yaml:"tools"`
}

type HTTPAuthProfile struct {
	Type       string `json:"type" yaml:"type"` // "bearer", "basic", "apiKey"
	Token      string `json:"token,omitempty" yaml:"token,omitempty"`
	Username   string `json:"username,omitempty" yaml:"username,omitempty"`
	Password   string `json:"password,omitempty" yaml:"password,omitempty"`
	Header     string `json:"header,omitempty" yaml:"header,omitempty"`         // for apiKey
	QueryParam string `json:"queryParam,omitempty" yaml:"queryParam,omitempty"` // for apiKey
	Value      string `json:"value,omitempty" yaml:"value,omitempty"`           // for apiKey
	Prefix     string `json:"prefix,omitempty" yaml:"prefix,omitempty"`         // optional value prefix
}

// ToolDefinition is one tool inside a skill.
type ToolDefinition struct {
	Name        string      `json:"name" yaml:"name"`
	Description string      `json:"description" yaml:"description"`
	Type        string      `json:"type" yaml:"type"`             // "shell", "http", or "workflow"
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
	Timeout      int                    `json:"timeout,omitempty" yaml:"timeout,omitempty"` // seconds, default 30
	Result       string                 `json:"result,omitempty" yaml:"result,omitempty"`   // "text"(default) | "json"
	Extract      string                 `json:"extract,omitempty" yaml:"extract,omitempty"` // path into JSON result
	Select       map[string]string      `json:"select,omitempty" yaml:"select,omitempty"`   // output key -> path into JSON result
	OutputSchema map[string]interface{} `json:"outputSchema,omitempty" yaml:"outputSchema,omitempty"`
	Auth         string                 `json:"auth,omitempty" yaml:"auth,omitempty"` // auth profile name

	// Workflow fields
	Steps       []ActionDefinition            `json:"steps,omitempty" yaml:"steps,omitempty"`
	Finally     []ActionDefinition            `json:"finally,omitempty" yaml:"finally,omitempty"`
	ActionField string                        `json:"actionField,omitempty" yaml:"actionField,omitempty"` // default "action"
	Actions     map[string][]ActionDefinition `json:"actions,omitempty" yaml:"actions,omitempty"`
}

// ActionDefinition is one step in a workflow tool.
type ActionDefinition struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	Type string `json:"type" yaml:"type"` // "shell", "http", "forEach", "switch"
	If   string `json:"if,omitempty" yaml:"if,omitempty"`

	// Shell fields
	Command          []string `json:"command,omitempty" yaml:"command,omitempty"`
	WorkingDirectory string   `json:"workdir,omitempty" yaml:"workdir,omitempty"`

	// HTTP fields
	Method  string            `json:"method,omitempty" yaml:"method,omitempty"`
	URL     string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body    string            `json:"body,omitempty" yaml:"body,omitempty"`

	// Common
	Timeout      int                    `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retries      int                    `json:"retries,omitempty" yaml:"retries,omitempty"`
	RetryDelayMs int                    `json:"retryDelayMs,omitempty" yaml:"retryDelayMs,omitempty"`
	OnError      string                 `json:"onError,omitempty" yaml:"onError,omitempty"` // "fail"(default) | "continue"
	Result       string                 `json:"result,omitempty" yaml:"result,omitempty"`   // "text"(default) | "json"
	SaveAs       string                 `json:"saveAs,omitempty" yaml:"saveAs,omitempty"`
	Extract      string                 `json:"extract,omitempty" yaml:"extract,omitempty"` // path into JSON result
	Select       map[string]string      `json:"select,omitempty" yaml:"select,omitempty"`   // output key -> path into JSON result
	OutputSchema map[string]interface{} `json:"outputSchema,omitempty" yaml:"outputSchema,omitempty"`
	Auth         string                 `json:"auth,omitempty" yaml:"auth,omitempty"` // auth profile name

	// forEach fields
	ForEach string             `json:"forEach,omitempty" yaml:"forEach,omitempty"`
	As      string             `json:"as,omitempty" yaml:"as,omitempty"`
	Steps   []ActionDefinition `json:"steps,omitempty" yaml:"steps,omitempty"`

	// switch fields
	Switch  string             `json:"switch,omitempty" yaml:"switch,omitempty"`
	Cases   []SwitchCase       `json:"cases,omitempty" yaml:"cases,omitempty"`
	Default []ActionDefinition `json:"default,omitempty" yaml:"default,omitempty"`
}

type SwitchCase struct {
	Match string             `json:"match" yaml:"match"`
	Steps []ActionDefinition `json:"steps" yaml:"steps"`
}
