package runner

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/teanode/teanode/test/voicee2e/internal/assertions"
	"github.com/teanode/teanode/test/voicee2e/internal/config"
	"github.com/teanode/teanode/test/voicee2e/internal/model"
	"github.com/teanode/teanode/test/voicee2e/internal/protocol"
	"github.com/teanode/teanode/test/voicee2e/internal/scoring"
)

type Runner struct {
	client *protocol.Client
}

func New(gatewayUrl string) *Runner {
	return &Runner{
		client: protocol.NewClient(gatewayUrl),
	}
}

func (self *Runner) RunSuite(ctx context.Context, configuration model.RunnerConfiguration) (*model.RunReport, error) {
	start := time.Now()
	suite, err := config.LoadSuite(configuration.SuitePath)
	if err != nil {
		return nil, err
	}
	suite, err = config.FilterScenario(suite, configuration.Scenario)
	if err != nil {
		return nil, err
	}
	if configuration.PromptPath != "" {
		raw, err := os.ReadFile(configuration.PromptPath)
		if err != nil {
			return nil, fmt.Errorf("read prompt file: %w", err)
		}
		self.client.SetPromptSuffix(string(raw))
	}
	self.client.SetConfigJSON(configuration.ConfigJSON)

	results := make([]model.ScenarioResult, 0, len(suite.Scenarios))
	for _, scenario := range suite.Scenarios {
		result := self.runScenario(ctx, scenario, configuration)
		results = append(results, result)
	}

	report := &model.RunReport{
		Version:       "v1",
		SuiteName:     suite.Name,
		GatewayURL:    configuration.GatewayURL,
		StartedAt:     start,
		EndedAt:       time.Now(),
		ScenarioCount: len(results),
		Results:       results,
	}
	report.DurationMS = report.EndedAt.Sub(report.StartedAt).Milliseconds()
	for _, result := range results {
		if result.Passed {
			report.PassedCount++
		} else {
			report.FailedCount++
		}
	}
	return report, nil
}

func (self *Runner) runScenario(ctx context.Context, scenario model.ScenarioSpecification, configuration model.RunnerConfiguration) model.ScenarioResult {
	start := time.Now()
	result := model.ScenarioResult{
		ID:            scenario.ID,
		Name:          scenario.Name,
		StartedAt:     start,
		Metrics:       map[string]any{},
		PromptVariant: configuration.PromptPath,
	}

	timeout := 90 * time.Second
	if scenario.TimeoutSeconds > 0 {
		timeout = time.Duration(scenario.TimeoutSeconds) * time.Second
	}
	scenarioContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	timeline, runErr := self.client.RunScenario(scenarioContext, scenario)
	result.Timeline = timeline
	result.TurnMetrics = collectTurnMetrics(timeline)
	if runErr != nil {
		result.Failures = append(result.Failures, fmt.Sprintf("scenario run failed: %v", runErr))
	}

	metrics := scoring.Compute(result.Timeline)
	assertions.EnrichMetrics(scenario, result.Timeline, metrics)
	for key, value := range metrics {
		result.Metrics[key] = value
	}
	failures, warnings := assertions.Evaluate(scenario, result.Timeline, metrics)
	result.Failures = append(result.Failures, failures...)
	result.Warnings = append(result.Warnings, warnings...)

	result.EndedAt = time.Now()
	result.DurationMS = result.EndedAt.Sub(start).Milliseconds()
	result.Passed = len(result.Failures) == 0
	return result
}

func collectTurnMetrics(timeline []model.TimelineEvent) []model.TurnMetrics {
	metrics := make([]model.TurnMetrics, 0, 8)
	for _, event := range timeline {
		if event.Type != model.EventTurnMetrics {
			continue
		}
		raw := event.Raw
		metric := model.TurnMetrics{
			TurnID:              toString(raw["turnId"]),
			ResponseID:          toString(raw["responseId"]),
			SpeechStartedMS:     toInt64(raw["speechStartedMs"]),
			SpeechEndedMS:       toInt64(raw["speechEndedMs"]),
			TranscriptFinalMS:   toInt64(raw["transcriptFinalMs"]),
			TurnCommittedMS:     toInt64(raw["turnCommittedMs"]),
			ResponseStartedMS:   toInt64(raw["responseStartedMs"]),
			ResponseCompletedMS: toInt64(raw["responseCompletedMs"]),
			STTMS:               toInt64(raw["sttMs"]),
			LLMTTFBMS:           toInt64(raw["llmTtfbMs"]),
			TTSMS:               toInt64(raw["ttsMs"]),
			E2EMS:               toInt64(raw["e2eMs"]),
		}
		metrics = append(metrics, metric)
	}
	return metrics
}

func toInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}

func toString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
