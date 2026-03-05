package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/teanode/teanode/test/voicee2e/internal/model"
	"github.com/teanode/teanode/test/voicee2e/internal/report"
	"github.com/teanode/teanode/test/voicee2e/internal/runner"
)

func main() {
	var configuration model.RunnerConfiguration
	flag.StringVar(&configuration.GatewayURL, "gateway-url", "http://127.0.0.1:8833", "TeaNode gateway base URL")
	flag.StringVar(&configuration.SuitePath, "suite", "test/voicee2e/scenarios/suite.yaml", "Path to suite YAML")
	flag.StringVar(&configuration.Scenario, "scenario", "", "Run only one scenario ID")
	flag.StringVar(&configuration.OutputPath, "out", "", "Output JSON report path")
	flag.StringVar(&configuration.ConfigJSON, "config", "", "Inline JSON config overrides (e.g. feature flags)")
	flag.StringVar(&configuration.PromptPath, "prompt", "", "Prompt variant file path")
	flag.BoolVar(&configuration.Compare, "compare", false, "Compare mode")
	flag.StringVar(&configuration.PromptA, "prompt-a", "", "Prompt A report JSON path")
	flag.StringVar(&configuration.PromptB, "prompt-b", "", "Prompt B report JSON path")
	flag.Parse()

	if configuration.OutputPath == "" {
		configuration.OutputPath = filepath.Join("test", "voicee2e", "reports", fmt.Sprintf("report-%d.json", time.Now().Unix()))
	}

	if configuration.Compare {
		if err := compareReports(configuration); err != nil {
			fmt.Fprintf(os.Stderr, "compare failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx := context.Background()
	run := runner.New(configuration.GatewayURL)
	result, err := run.RunSuite(ctx, configuration)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run failed: %v\n", err)
		os.Exit(1)
	}
	if err := report.WriteJSON(configuration.OutputPath, result); err != nil {
		fmt.Fprintf(os.Stderr, "write report failed: %v\n", err)
		os.Exit(1)
	}
	markdownPath := configuration.OutputPath + ".md"
	if err := os.WriteFile(markdownPath, []byte(report.RenderMarkdown(result)), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write markdown report failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("voicee2e report: %s\n", configuration.OutputPath)
	if result.FailedCount > 0 {
		os.Exit(2)
	}
}

func compareReports(configuration model.RunnerConfiguration) error {
	if configuration.PromptA == "" || configuration.PromptB == "" {
		return fmt.Errorf("--compare requires --prompt-a and --prompt-b")
	}
	var base model.RunReport
	var candidate model.RunReport
	if err := readJson(configuration.PromptA, &base); err != nil {
		return fmt.Errorf("read prompt-a: %w", err)
	}
	if err := readJson(configuration.PromptB, &candidate); err != nil {
		return fmt.Errorf("read prompt-b: %w", err)
	}
	start := time.Now()
	summary := []string{
		fmt.Sprintf("base failed=%d/%d", base.FailedCount, base.ScenarioCount),
		fmt.Sprintf("candidate failed=%d/%d", candidate.FailedCount, candidate.ScenarioCount),
	}
	comparison := model.CompareReport{
		Version:       "v1",
		StartedAt:     start,
		EndedAt:       time.Now(),
		BasePath:      configuration.PromptA,
		CandidatePath: configuration.PromptB,
		Summary:       summary,
	}
	output := configuration.OutputPath
	if output == "" {
		output = filepath.Join("test", "voicee2e", "reports", fmt.Sprintf("compare-%d.json", time.Now().Unix()))
	}
	return report.WriteJSON(output, comparison)
}

func readJson(path string, output any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, output)
}
