package assertions

import (
	"testing"

	"github.com/teanode/teanode/test/voicee2e/internal/model"
)

func TestEvaluateRequireBargeIn(t *testing.T) {
	t.Parallel()
	scenario := model.ScenarioSpecification{
		ID:   "s",
		Name: "scenario",
		Expect: model.ScenarioExpectations{
			RequireBargeIn: true,
		},
	}
	metrics := map[string]any{
		"transcriptCount": int64(1),
		"responseCount":   int64(1),
		"bargeInCount":    int64(0),
	}
	failures, _ := Evaluate(scenario, nil, metrics)
	if len(failures) == 0 {
		t.Fatalf("expected barge-in failure")
	}
}
