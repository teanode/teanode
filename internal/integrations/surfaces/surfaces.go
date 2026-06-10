// Package surfaces provides the schema-driven generative-UI surface and
// unified interrupt model, plus the in-memory broker that routes surface
// actions from the WebSocket RPC layer back to the conversation runtime.
package surfaces

import (
	"fmt"

	"github.com/op/go-logging"
)

// Per-package logger declaration (mulint_log).
var log = logging.MustGetLogger("surfaces") //nolint:unused

// SchemaVersion is the current surface schema version. Clients use this to
// guard against rendering payloads they do not understand.
const SchemaVersion = 1

// SurfaceLocation identifies where a surface should render in the UI.
type SurfaceLocation string

const (
	// SurfaceLocationInline renders the surface in the conversation column,
	// near the associated conversation content.
	SurfaceLocationInline SurfaceLocation = "inline"

	// SurfaceLocationRightPanel renders the surface in a dedicated side panel.
	SurfaceLocationRightPanel SurfaceLocation = "right_panel"
)

// Component type identifiers. The catalog is intentionally small for the MVP.
const (
	ComponentTypeSection      = "Section"
	ComponentTypeMarkdown     = "Markdown"
	ComponentTypeKeyValueList = "KeyValueList"
	ComponentTypeTable        = "Table"
	ComponentTypeStatusBadge  = "StatusBadge"
	ComponentTypeButtonRow    = "ButtonRow"
	ComponentTypeForm         = "Form"
	ComponentTypeCodeBlock    = "CodeBlock"
	ComponentTypeTimeline     = "Timeline"
)

// Form field type identifiers.
const (
	FieldTypeTextInput = "TextInput"
	FieldTypeTextarea  = "Textarea"
	FieldTypeSelect    = "Select"
	FieldTypeCheckbox  = "Checkbox"
)

// InterruptKind identifies the kind of a unified interrupt.
type InterruptKind string

const (
	InterruptKindQuestion InterruptKind = "question"
	InterruptKindApproval InterruptKind = "approval"
	InterruptKindChoice   InterruptKind = "choice"
	InterruptKindForm     InterruptKind = "form"
	InterruptKindReview   InterruptKind = "review"
)

// Surface is a schema-driven, declaratively rendered UI fragment emitted by the
// backend. It carries no executable code — only typed components the client
// knows how to render.
type Surface struct {
	SurfaceID     string             `json:"surfaceId"`
	SchemaVersion int                `json:"schemaVersion"`
	Location      SurfaceLocation    `json:"location"`
	Title         string             `json:"title,omitempty"`
	Components    []SurfaceComponent `json:"components"`

	// Conversation scoping (set by the emitter, used for routing/rehydration).
	ConversationID string `json:"conversationId,omitempty"`
	AgentID        string `json:"agentId,omitempty"`
	RunID          string `json:"runId,omitempty"`
}

// SurfaceComponent is a flattened tagged union: Type selects which fields are
// meaningful. Keeping it a single struct keeps the JSON shape easy for emitters
// (tools, debug RPC) to build and for the client to render.
type SurfaceComponent struct {
	Type string `json:"type"`

	// Section: a titled group wrapping nested components.
	Title    string             `json:"title,omitempty"`
	Children []SurfaceComponent `json:"children,omitempty"`

	// Markdown / CodeBlock: text body. CodeBlock also uses Language.
	Text     string `json:"text,omitempty"`
	Language string `json:"language,omitempty"`

	// KeyValueList: rows of key/value pairs.
	Items []KeyValueItem `json:"items,omitempty"`

	// Table: column headers and string rows.
	Columns []string   `json:"columns,omitempty"`
	Rows    [][]string `json:"rows,omitempty"`

	// StatusBadge: a coloured status label. Status is success|warning|error|info|neutral.
	Status string `json:"status,omitempty"`
	Label  string `json:"label,omitempty"`

	// ButtonRow: a row of action buttons.
	Buttons []SurfaceButton `json:"buttons,omitempty"`

	// Form: input fields plus a submit action.
	Fields         []FormField `json:"fields,omitempty"`
	SubmitLabel    string      `json:"submitLabel,omitempty"`
	SubmitActionID string      `json:"submitActionId,omitempty"`

	// Timeline: ordered events.
	Events []TimelineEvent `json:"events,omitempty"`
}

// KeyValueItem is a single key/value row in a KeyValueList.
type KeyValueItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// SurfaceButton is a clickable action inside a ButtonRow.
type SurfaceButton struct {
	Label    string `json:"label"`
	ActionID string `json:"actionId"`
	Style    string `json:"style,omitempty"` // primary|secondary|danger
	Value    string `json:"value,omitempty"` // optional value sent with the action
}

// FormField is a single input field inside a Form.
type FormField struct {
	Type         string   `json:"type"`
	Name         string   `json:"name"`
	Label        string   `json:"label,omitempty"`
	Placeholder  string   `json:"placeholder,omitempty"`
	Options      []string `json:"options,omitempty"` // Select choices
	Required     bool     `json:"required,omitempty"`
	DefaultValue string   `json:"defaultValue,omitempty"`
}

// TimelineEvent is a single entry in a Timeline component.
type TimelineEvent struct {
	Title       string `json:"title"`
	Timestamp   string `json:"timestamp,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
}

// Interrupt is the generalized, kind-tagged request for user input. It unifies
// the question/approval flows with new choice/form/review flows. For the MVP the
// backend emits choice/form/review interrupts; question/approval continue to use
// their dedicated brokers and are unified into this model on the client.
type Interrupt struct {
	InterruptID string        `json:"interruptId"`
	Kind        InterruptKind `json:"kind"`
	Title       string        `json:"title,omitempty"`
	Prompt      string        `json:"prompt,omitempty"`

	Choices []string    `json:"choices,omitempty"` // kind == choice
	Fields  []FormField `json:"fields,omitempty"`  // kind == form
	Surface *Surface    `json:"surface,omitempty"` // kind == review (rich content)

	// Round-trip routing: the surface whose action resolves this interrupt.
	SurfaceID string `json:"surfaceId,omitempty"`

	ConversationID string `json:"conversationId,omitempty"`
	AgentID        string `json:"agentId,omitempty"`
	RunID          string `json:"runId,omitempty"`
}

// RenderPayload is the structured payload broadcast over the conversationSurfaces
// event. Either field may be set.
type RenderPayload struct {
	Surface   *Surface   `json:"surface,omitempty"`
	Interrupt *Interrupt `json:"interrupt,omitempty"`
}

// SurfaceActionPayload describes a user action taken on a surface, sent from the
// client via the surfaces.action RPC.
type SurfaceActionPayload struct {
	SurfaceID string            `json:"surfaceId"`
	ActionID  string            `json:"actionId"`
	Value     string            `json:"value,omitempty"`    // single-value actions (button value)
	FormData  map[string]string `json:"formData,omitempty"` // form submissions
}

var validComponentTypes = map[string]struct{}{
	ComponentTypeSection:      {},
	ComponentTypeMarkdown:     {},
	ComponentTypeKeyValueList: {},
	ComponentTypeTable:        {},
	ComponentTypeStatusBadge:  {},
	ComponentTypeButtonRow:    {},
	ComponentTypeForm:         {},
	ComponentTypeCodeBlock:    {},
	ComponentTypeTimeline:     {},
}

var validFieldTypes = map[string]struct{}{
	FieldTypeTextInput: {},
	FieldTypeTextarea:  {},
	FieldTypeSelect:    {},
	FieldTypeCheckbox:  {},
}

// Validate checks that a surface is structurally sound and only uses known
// component and field types. It returns the first problem found.
func (self *Surface) Validate() error {
	if self == nil {
		return fmt.Errorf("surfaces: surface is nil")
	}
	if self.Location != SurfaceLocationInline && self.Location != SurfaceLocationRightPanel {
		return fmt.Errorf("surfaces: invalid location %q", self.Location)
	}
	if len(self.Components) == 0 {
		return fmt.Errorf("surfaces: surface must have at least one component")
	}
	for _, component := range self.Components {
		if err := validateComponent(component); err != nil {
			return err
		}
	}
	return nil
}

func validateComponent(component SurfaceComponent) error {
	if _, ok := validComponentTypes[component.Type]; !ok {
		return fmt.Errorf("surfaces: unknown component type %q", component.Type)
	}
	for _, child := range component.Children {
		if err := validateComponent(child); err != nil {
			return err
		}
	}
	for _, field := range component.Fields {
		if _, ok := validFieldTypes[field.Type]; !ok {
			return fmt.Errorf("surfaces: unknown form field type %q", field.Type)
		}
		if field.Name == "" {
			return fmt.Errorf("surfaces: form field requires a name")
		}
	}
	return nil
}

// Validate checks that an interrupt is structurally sound for its kind.
func (self *Interrupt) Validate() error {
	if self == nil {
		return fmt.Errorf("surfaces: interrupt is nil")
	}
	switch self.Kind {
	case InterruptKindQuestion, InterruptKindApproval, InterruptKindChoice,
		InterruptKindForm, InterruptKindReview:
	default:
		return fmt.Errorf("surfaces: invalid interrupt kind %q", self.Kind)
	}
	if self.Kind == InterruptKindChoice && len(self.Choices) == 0 {
		return fmt.Errorf("surfaces: choice interrupt requires choices")
	}
	if self.Kind == InterruptKindForm && len(self.Fields) == 0 {
		return fmt.Errorf("surfaces: form interrupt requires fields")
	}
	if self.Kind == InterruptKindReview && self.Surface != nil {
		return self.Surface.Validate()
	}
	return nil
}
