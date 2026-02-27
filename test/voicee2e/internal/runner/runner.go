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

func (r *Runner) RunSuite(ctx context.Context, cfg model.RunnerConfig) (*model.RunReport, error) {
	start := time.Now()
	suite, err := config.LoadSuite(cfg.SuitePath)
	if err != nil {
		return nil, err
	}
	suite, err = config.FilterScenario(suite, cfg.Scenario)
	if err != nil {
		return nil, err
	}
	if cfg.PromptPath != "" {
		raw, err := os.ReadFile(cfg.PromptPath)
		if err != nil {
			return nil, fmt.Errorf("read prompt file: %w", err)
		}
		r.client.SetPromptSuffix(string(raw))
	}

	results := make([]model.ScenarioResult, 0, len(suite.Scenarios))
	for _, scenario := range suite.Scenarios {
		result := r.runScenario(ctx, scenario, cfg)
		results = append(results, result)
	}

	report := &model.RunReport{
		Version:       "v1",
		SuiteName:     suite.Name,
		GatewayURL:    cfg.GatewayURL,
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

func (r *Runner) runScenario(ctx context.Context, scenario model.ScenarioSpec, cfg model.RunnerConfig) model.ScenarioResult {
	start := time.Now()
	result := model.ScenarioResult{
		ID:            scenario.ID,
		Name:          scenario.Name,
		StartedAt:     start,
		Metrics:       map[string]any{},
		PromptVariant: cfg.PromptPath,
	}

	timeout := 90 * time.Second
	if scenario.TimeoutSec > 0 {
		timeout = time.Duration(scenario.TimeoutSec) * time.Second
	}
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	timeline, runErr := r.client.RunScenario(sctx, scenario)
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
