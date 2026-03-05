package voice

// TurnDecision controls runtime barge-in handling.
type TurnDecision int

const (
	TurnDecisionIgnore TurnDecision = iota
	TurnDecisionCandidate
	TurnDecisionTrigger
)

// TurnContext contains runtime cues for turn decisioning.
type TurnContext struct {
	VADScore          float64
	SpeechDurationMs  int
	SilenceDurationMs int
	RunActive         bool
	ResponseActive    bool
	InterimText       string
}

// TurnStrategy controls barge-in and commit policy.
type TurnStrategy interface {
	EvaluateBargeIn(ctx TurnContext) TurnDecision
	ShouldCommitTurn(ctx TurnContext) bool
}

// LegacyStrategy preserves current behavior.
type LegacyStrategy struct{}

func (LegacyStrategy) EvaluateBargeIn(ctx TurnContext) TurnDecision {
	if !ctx.RunActive && !ctx.ResponseActive {
		return TurnDecisionIgnore
	}
	if ctx.VADScore >= bargeInTriggerMinScore {
		return TurnDecisionTrigger
	}
	return TurnDecisionIgnore
}

func (LegacyStrategy) ShouldCommitTurn(TurnContext) bool {
	return true
}
