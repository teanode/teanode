package voice

import "strings"

var danglingConjunctions = []string{
	"and", "or", "but", "so", "because", "if", "then",
}

// BalancedStrategy reduces premature barge-ins and early commits.
type BalancedStrategy struct{}

func (BalancedStrategy) EvaluateBargeIn(ctx TurnContext) TurnDecision {
	if !ctx.RunActive && !ctx.ResponseActive {
		return TurnDecisionIgnore
	}
	if ctx.VADScore < 0.12 {
		return TurnDecisionIgnore
	}
	if ctx.SpeechDurationMs < 120 {
		return TurnDecisionCandidate
	}
	return TurnDecisionTrigger
}

func (BalancedStrategy) ShouldCommitTurn(ctx TurnContext) bool {
	if ctx.SilenceDurationMs >= 700 {
		return true
	}
	text := strings.TrimSpace(ctx.InterimText)
	if text == "" {
		return false
	}
	if endsWithSentenceTerminator(text) && ctx.SilenceDurationMs >= 120 {
		return true
	}
	if endsWithDanglingConjunction(text) {
		return false
	}
	return ctx.SilenceDurationMs >= 300
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
