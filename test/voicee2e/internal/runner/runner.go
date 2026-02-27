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
