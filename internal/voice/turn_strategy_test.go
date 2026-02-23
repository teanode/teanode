package voice

import "testing"

func TestLegacyStrategy_BargeIn_Trigger(t *testing.T) {
	strategy := LegacyStrategy{}
	decision := strategy.EvaluateBargeIn(TurnContext{
		VADScore:       0.07,
		RunActive:      true,
		ResponseActive: true,
	})
	if decision != TurnDecisionTrigger {
		t.Fatalf("expected trigger, got %v", decision)
	}
}

func TestLegacyStrategy_BargeIn_Ignore(t *testing.T) {
	strategy := LegacyStrategy{}
	decision := strategy.EvaluateBargeIn(TurnContext{
		VADScore:       0.05,
		RunActive:      true,
		ResponseActive: true,
	})
	if decision != TurnDecisionIgnore {
		t.Fatalf("expected ignore, got %v", decision)
	}
}

func TestLegacyStrategy_ShouldCommit(t *testing.T) {
	strategy := LegacyStrategy{}
	if !strategy.ShouldCommitTurn(TurnContext{SilenceDurationMs: 1}) {
		t.Fatal("legacy strategy should always commit")
	}
}

func TestBalancedStrategy_BargeIn_Debounce(t *testing.T) {
	strategy := BalancedStrategy{}
	if decision := strategy.EvaluateBargeIn(TurnContext{
		VADScore:         0.15,
		SpeechDurationMs: 50,
		RunActive:        true,
	}); decision != TurnDecisionCandidate {
		t.Fatalf("expected candidate at 50ms, got %v", decision)
	}
	if decision := strategy.EvaluateBargeIn(TurnContext{
		VADScore:         0.15,
		SpeechDurationMs: 150,
		RunActive:        true,
	}); decision != TurnDecisionTrigger {
		t.Fatalf("expected trigger at 150ms, got %v", decision)
	}
}

func TestBalancedStrategy_BargeIn_ScoreDrop(t *testing.T) {
	strategy := BalancedStrategy{}
	if decision := strategy.EvaluateBargeIn(TurnContext{
		VADScore:         0.1,
		SpeechDurationMs: 200,
		RunActive:        true,
	}); decision != TurnDecisionIgnore {
		t.Fatalf("expected ignore below threshold, got %v", decision)
	}
}

func TestBalancedStrategy_ShouldCommit_DanglingConjunction(t *testing.T) {
	strategy := BalancedStrategy{}
	if strategy.ShouldCommitTurn(TurnContext{
		SilenceDurationMs: 300,
		InterimText:       "I want to and",
	}) {
		t.Fatal("expected dangling conjunction to delay commit")
	}
	if !strategy.ShouldCommitTurn(TurnContext{
		SilenceDurationMs: 700,
		InterimText:       "I want to and",
	}) {
		t.Fatal("expected max silence to force commit")
	}
}

func TestBalancedStrategy_ShouldCommit_SentenceTerminator(t *testing.T) {
	strategy := BalancedStrategy{}
	if !strategy.ShouldCommitTurn(TurnContext{
		SilenceDurationMs: 150,
		InterimText:       "Hello world.",
	}) {
		t.Fatal("expected sentence terminator to commit")
	}
}

func TestBalancedStrategy_ShouldCommit_MaxSilence(t *testing.T) {
	strategy := BalancedStrategy{}
	if !strategy.ShouldCommitTurn(TurnContext{
		SilenceDurationMs: 700,
		InterimText:       "anything",
	}) {
		t.Fatal("expected max silence to commit")
	}
}
