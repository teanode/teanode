package browser

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/integrations/browsers"
)

// scriptStep represents one step in a multi-step browser script.
type scriptStep struct {
	Action      string   `json:"action"`
	URL         string   `json:"url,omitempty"`
	Ref         *int     `json:"ref,omitempty"`
	Selector    string   `json:"selector,omitempty"`
	Text        string   `json:"text,omitempty"`
	Key         string   `json:"key,omitempty"`
	Expression  string   `json:"expression,omitempty"`
	X           *float64 `json:"x,omitempty"`
	Y           *float64 `json:"y,omitempty"`
	WaitMode    string   `json:"waitMode,omitempty"`
	TimeoutMs   *int     `json:"timeoutMs,omitempty"`
	ClearFirst  bool     `json:"clearFirst,omitempty"`
	OptionValue string   `json:"optionValue,omitempty"`
	OptionIndex *int     `json:"optionIndex,omitempty"`
}

// scriptStepResult holds the output of one step.
type scriptStepResult struct {
	Step   int             `json:"step"`
	Action string          `json:"action"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// executeScript runs a sequence of browser actions atomically on the same
// session. Execution stops at the first error. Returns results for all
// executed steps.
func executeScript(ctx context.Context, browser browsers.Browser, connectionId string, steps []scriptStep) (string, error) {
	if len(steps) == 0 {
		return "", fmt.Errorf("script requires at least one step")
	}
	if len(steps) > 50 {
		return "", fmt.Errorf("script exceeds maximum of 50 steps")
	}

	results := make([]scriptStepResult, 0, len(steps))

	for index, step := range steps {
		result, err := executeSingleStep(ctx, browser, connectionId, step)

		stepResult := scriptStepResult{
			Step:   index + 1,
			Action: step.Action,
		}

		if err != nil {
			stepResult.Error = err.Error()
			results = append(results, stepResult)
			// Stop on first error.
			break
		}

		stepResult.Result = json.RawMessage(result)
		results = append(results, stepResult)
	}

	output, _ := json.Marshal(map[string]interface{}{
		"stepsExecuted": len(results),
		"totalSteps":    len(steps),
		"results":       results,
	})
	return string(output), nil
}

// executeSingleStep dispatches a single script step to the appropriate handler.
func executeSingleStep(ctx context.Context, browser browsers.Browser, connectionId string, step scriptStep) (string, error) {
	switch step.Action {
	case "navigate":
		return executeNavigate(ctx, browser, connectionId, step.URL)
	case "screenshot":
		return executeScreenshot(ctx, browser, connectionId)
	case "snapshot":
		return executeEnhancedSnapshot(ctx, browser, connectionId)
	case "click":
		return executeClick(ctx, browser, connectionId, step.X, step.Y, step.Selector)
	case "click_ref":
		if step.Ref == nil {
			return "", fmt.Errorf("ref is required for click_ref")
		}
		return executeClickRef(ctx, browser, connectionId, *step.Ref)
	case "type":
		return executeType(ctx, browser, connectionId, step.Text, step.Selector)
	case "type_ref":
		if step.Ref == nil {
			return "", fmt.Errorf("ref is required for type_ref")
		}
		return executeTypeRef(ctx, browser, connectionId, *step.Ref, step.Text, step.ClearFirst)
	case "hover_ref":
		if step.Ref == nil {
			return "", fmt.Errorf("ref is required for hover_ref")
		}
		return executeHoverRef(ctx, browser, connectionId, *step.Ref)
	case "select_option":
		if step.Ref == nil {
			return "", fmt.Errorf("ref is required for select_option")
		}
		return executeSelectOption(ctx, browser, connectionId, *step.Ref, step.OptionValue, step.OptionIndex)
	case "press_key":
		return executePressKey(ctx, browser, connectionId, step.Key)
	case "evaluate":
		return executeEvaluate(ctx, browser, connectionId, step.Expression)
	case "wait":
		mode := step.WaitMode
		if mode == "" {
			mode = "navigation"
		}
		return executeWait(ctx, browser, connectionId, mode, step.Selector, step.TimeoutMs)
	default:
		return "", fmt.Errorf("unknown script action: %s", step.Action)
	}
}
