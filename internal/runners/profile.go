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

// staticPrefixBudgetFraction is the share of the context window that the
// static prefix — system prompt plus tool definitions — may take before a run
// switches to the compact profile. The prefix is paid on every request and
// compaction cannot reclaim it, so the rest of the window has to hold the
// whole conversation.
//
// This is a budget rather than a fixed context-window cutoff because the
// prefix varies by an order of magnitude with the number of registered tools.
// A node with a handful of tools stays on the full profile even at 32k, while
// a node with every integration enabled goes compact well above it.
const staticPrefixBudgetFraction = 0.25

// Workspace files are capped harder under the compact profile: four files at
// the full cap would on their own exceed a small context window.
const (
	fullWorkspaceFileCharacters    = 8000
	compactWorkspaceFileCharacters = 2000
)

// resolvePromptProfile picks the profile by checking the full-profile static
// prefix against the share of the context window it is allowed to take.
func resolvePromptProfile(contextWindow int, staticPrefixTokens int) PromptProfile {
	if contextWindow <= 0 || staticPrefixTokens <= 0 {
		return PromptProfileFull
	}
	if float64(staticPrefixTokens) > float64(contextWindow)*staticPrefixBudgetFraction {
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
