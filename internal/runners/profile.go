package runners

// PromptProfile selects how much static text a run spends on the system
// prompt and tool definitions.
type PromptProfile string

const (
	// PromptProfileFull sends the full system prompt and every tool definition.
	PromptProfileFull PromptProfile = "full"
	// PromptProfileCompact shortens the system prompt, drops the non-standard
	// tool `returns` schemas, and defers all but the core tools behind
	// tool_search. It is selected automatically for models with a small
	// context window, where the static prefix would otherwise crowd out the
	// conversation.
	PromptProfileCompact PromptProfile = "compact"
)

// compactProfileContextWindow is the largest context window that still gets
// the compact profile. Locally hosted models are typically served at 8k-32k,
// where a ~23k token static prefix leaves no usable room; hosted models start
// well above this.
const compactProfileContextWindow = 32000

// Workspace files are capped harder under the compact profile: four files at
// the full cap would on their own exceed a small context window.
const (
	fullWorkspaceFileCharacters    = 8000
	compactWorkspaceFileCharacters = 2000
)

// resolvePromptProfile picks the profile for a model's context window.
func resolvePromptProfile(contextWindow int) PromptProfile {
	if contextWindow > 0 && contextWindow <= compactProfileContextWindow {
		return PromptProfileCompact
	}
	return PromptProfileFull
}

// workspaceFileCharacters returns the per-file truncation cap for a profile.
func workspaceFileCharacters(profile PromptProfile) int {
	if profile == PromptProfileCompact {
		return compactWorkspaceFileCharacters
	}
	return fullWorkspaceFileCharacters
}
