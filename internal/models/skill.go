package models

import "time"

type SkillToolType string

const (
	SkillToolTypeShell    SkillToolType = "shell"
	SkillToolTypeHTTP     SkillToolType = "http"
	SkillToolTypeWorkflow SkillToolType = "workflow"
)

type SkillActionType string

const (
	SkillActionTypeShell   SkillActionType = "shell"
	SkillActionTypeHTTP    SkillActionType = "http"
	SkillActionTypeForEach SkillActionType = "forEach"
	SkillActionTypeSwitch  SkillActionType = "switch"
)

type SkillAuthenticationType string

const (
	SkillAuthenticationTypeBearer SkillAuthenticationType = "bearer"
	SkillAuthenticationTypeBasic  SkillAuthenticationType = "basic"
	SkillAuthenticationTypeAPIKey SkillAuthenticationType = "apiKey"
)

type SkillResultFormat string

const (
	SkillResultFormatText SkillResultFormat = "text"
	SkillResultFormatJSON SkillResultFormat = "json"
)

type SkillErrorPolicy string

const (
	SkillErrorPolicyFail     SkillErrorPolicy = "fail"
	SkillErrorPolicyContinue SkillErrorPolicy = "continue"
)

type SkillSecret struct {
	Key         string `json:"key" yaml:"key"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type Skill struct {
	ID                     string                                  `json:"id,omitempty" yaml:"id,omitempty"`
	Name                   *string                                 `json:"name,omitempty" yaml:"name,omitempty"`
	Description            *string                                 `json:"description,omitempty" yaml:"description,omitempty"`
	Version                *string                                 `json:"version,omitempty" yaml:"version,omitempty"`
	AuthenticationProfiles *map[string]SkillAuthenticationProfiles `json:"authenticationProfiles,omitempty" yaml:"authenticationProfiles,omitempty"`
	Secrets                *[]*SkillSecret                         `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Tools                  *[]*SkillTool                           `json:"tools,omitempty" yaml:"tools,omitempty"`
	Enabled                *bool                                   `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Source                 *string                                 `json:"source,omitempty" yaml:"source,omitempty"`
	Publisher              *string                                 `json:"publisher,omitempty" yaml:"publisher,omitempty"`
	Prompt                 *string                                 `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	CreatedAt              *time.Time                              `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt             *time.Time                              `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`
}

// SkillAuthenticationProfiles defines authentication credentials for HTTP skill tools.
type SkillAuthenticationProfiles struct {
	Type       SkillAuthenticationType `json:"type" yaml:"type"` // "bearer", "basic", "apiKey"
	Token      string                  `json:"token,omitempty" yaml:"token,omitempty"`
	Username   string                  `json:"username,omitempty" yaml:"username,omitempty"`
	Password   string                  `json:"password,omitempty" yaml:"password,omitempty"`
	Header     string                  `json:"header,omitempty" yaml:"header,omitempty"`         // for apiKey
	QueryParam string                  `json:"queryParam,omitempty" yaml:"queryParam,omitempty"` // for apiKey
	Value      string                  `json:"value,omitempty" yaml:"value,omitempty"`           // for apiKey
	Prefix     string                  `json:"prefix,omitempty" yaml:"prefix,omitempty"`         // optional value prefix
}

// SkillTool is one tool inside a skill.
type SkillTool struct {
	Name        string        `json:"name" yaml:"name"`
	Description string        `json:"description" yaml:"description"`
	Type        SkillToolType `json:"type" yaml:"type"`             // "shell", "http", or "workflow"
	Parameters  interface{}   `json:"parameters" yaml:"parameters"` // JSON schema for LLM

	// Shell fields
	Command          []string `json:"command,omitempty" yaml:"command,omitempty"`                   // command + arguments
	WorkingDirectory string   `json:"workingDirectory,omitempty" yaml:"workingDirectory,omitempty"` // working directory

	// HTTP fields
	Method   string            `json:"method,omitempty" yaml:"method,omitempty"` // GET, POST, etc.
	URL      string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers  map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body     string            `json:"body,omitempty" yaml:"body,omitempty"`         // template for request body
	MaxBytes *int              `json:"maxBytes,omitempty" yaml:"maxBytes,omitempty"` // response size limit (bytes)

	// Common
	Timeout      int                    `json:"timeout,omitempty" yaml:"timeout,omitempty"` // seconds, default 30
	Result       SkillResultFormat      `json:"result,omitempty" yaml:"result,omitempty"`   // "text"(default) | "json"
	Extract      string                 `json:"extract,omitempty" yaml:"extract,omitempty"` // path into JSON result
	Select       map[string]string      `json:"select,omitempty" yaml:"select,omitempty"`   // output key -> path into JSON result
	OutputSchema map[string]interface{} `json:"outputSchema,omitempty" yaml:"outputSchema,omitempty"`
	Auth         string                 `json:"auth,omitempty" yaml:"auth,omitempty"` // auth profile name

	// Workflow fields
	Steps       []*SkillAction            `json:"steps,omitempty" yaml:"steps,omitempty"`
	Finally     []*SkillAction            `json:"finally,omitempty" yaml:"finally,omitempty"`
	ActionField string                    `json:"actionField,omitempty" yaml:"actionField,omitempty"` // default "action"
	Actions     map[string][]*SkillAction `json:"actions,omitempty" yaml:"actions,omitempty"`
}

// SkillAction is one step in a workflow tool.
type SkillAction struct {
	Name string          `json:"name,omitempty" yaml:"name,omitempty"`
	Type SkillActionType `json:"type" yaml:"type"` // "shell", "http", "forEach", "switch"
	If   string          `json:"if,omitempty" yaml:"if,omitempty"`

	// Shell fields
	Command          []string `json:"command,omitempty" yaml:"command,omitempty"`
	WorkingDirectory string   `json:"workingDirectory,omitempty" yaml:"workingDirectory,omitempty"`

	// HTTP fields
	Method   string            `json:"method,omitempty" yaml:"method,omitempty"`
	URL      string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers  map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body     string            `json:"body,omitempty" yaml:"body,omitempty"`
	MaxBytes *int              `json:"maxBytes,omitempty" yaml:"maxBytes,omitempty"` // per-step response size limit (bytes)

	// Common
	Timeout      int                    `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retries      int                    `json:"retries,omitempty" yaml:"retries,omitempty"`
	RetryDelay   int                    `json:"retryDelay,omitempty" yaml:"retryDelay,omitempty"`
	OnError      SkillErrorPolicy       `json:"onError,omitempty" yaml:"onError,omitempty"` // "fail"(default) | "continue"
	Result       SkillResultFormat      `json:"result,omitempty" yaml:"result,omitempty"`   // "text"(default) | "json"
	SaveAs       string                 `json:"saveAs,omitempty" yaml:"saveAs,omitempty"`
	Extract      string                 `json:"extract,omitempty" yaml:"extract,omitempty"` // path into JSON result
	Select       map[string]string      `json:"select,omitempty" yaml:"select,omitempty"`   // output key -> path into JSON result
	OutputSchema map[string]interface{} `json:"outputSchema,omitempty" yaml:"outputSchema,omitempty"`
	Auth         string                 `json:"auth,omitempty" yaml:"auth,omitempty"` // auth profile name

	// forEach fields
	ForEach string         `json:"forEach,omitempty" yaml:"forEach,omitempty"`
	As      string         `json:"as,omitempty" yaml:"as,omitempty"`
	Steps   []*SkillAction `json:"steps,omitempty" yaml:"steps,omitempty"`

	// switch fields
	Switch  string         `json:"switch,omitempty" yaml:"switch,omitempty"`
	Cases   []*SkillCase   `json:"cases,omitempty" yaml:"cases,omitempty"`
	Default []*SkillAction `json:"default,omitempty" yaml:"default,omitempty"`
}

// SkillCase is one case in a switch action.
type SkillCase struct {
	Match string         `json:"match" yaml:"match"`
	Steps []*SkillAction `json:"steps" yaml:"steps"`
}
