package voice

import "strings"

const (
	balancedBargeInMinScore    = 0.12
	balancedBargeInMinSpeechMs = 120
	balancedCommitMaxSilenceMs = 700
	balancedCommitMidSilenceMs = 300
	balancedCommitMinSilenceMs = 120
)

var danglingConjunctions = []string{
	"and", "or", "but", "so", "because", "if", "then",
}

// BalancedStrategy reduces premature barge-ins and early commits.
type BalancedStrategy struct{}

func (BalancedStrategy) EvaluateBargeIn(ctx TurnContext) TurnDecision {
	if !ctx.RunActive && !ctx.ResponseActive {
		return TurnDecisionIgnore
	}
	if ctx.VADScore < balancedBargeInMinScore {
		return TurnDecisionIgnore
	}
	if ctx.SpeechDurationMs < balancedBargeInMinSpeechMs {
		return TurnDecisionCandidate
	}
	return TurnDecisionTrigger
}

func (BalancedStrategy) ShouldCommitTurn(ctx TurnContext) bool {
	if ctx.SilenceDurationMs >= balancedCommitMaxSilenceMs {
		return true
	}
	text := strings.TrimSpace(ctx.InterimText)
	if text == "" {
		return false
	}
	if endsWithSentenceTerminator(text) && ctx.SilenceDurationMs >= balancedCommitMinSilenceMs {
		return true
	}
	if endsWithDanglingConjunction(text) {
		return false
	}
	return ctx.SilenceDurationMs >= balancedCommitMidSilenceMs
}

func endsWithSentenceTerminator(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasSuffix(trimmed, ".") || strings.HasSuffix(trimmed, "!") || strings.HasSuffix(trimmed, "?")
}

func endsWithDanglingConjunction(text string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(text))
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	last := fields[len(fields)-1]
	for _, conjunction := range danglingConjunctions {
		if last == conjunction {
			return true
		}
	}
	return false
}
