package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/teanode/teanode/test/voicee2e/internal/model"
	"gopkg.in/yaml.v3"
)

func LoadSuite(path string) (*model.SuiteSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read suite: %w", err)
	}

	var suite model.SuiteSpec
	if err := yaml.Unmarshal(raw, &suite); err != nil {
		return nil, fmt.Errorf("parse suite yaml: %w", err)
	}
	if err := validateSuite(&suite); err != nil {
		return nil, err
	}
	return &suite, nil
}

func FilterScenario(suite *model.SuiteSpec, scenarioId string) (*model.SuiteSpec, error) {
	if scenarioId == "" {
		return suite, nil
	}
	for _, scenario := range suite.Scenarios {
		if scenario.ID == scenarioId {
			return &model.SuiteSpec{
				Name:      suite.Name,
				Scenarios: []model.ScenarioSpec{scenario},
			}, nil
		}
	}
	return nil, fmt.Errorf("scenario not found: %s", scenarioId)
}

func validateSuite(suite *model.SuiteSpec) error {
	if suite.Name == "" {
		return errors.New("suite.name is required")
	}
	if len(suite.Scenarios) == 0 {
		return errors.New("suite.scenarios must not be empty")
	}
	seen := make(map[string]bool, len(suite.Scenarios))
	for _, scenario := range suite.Scenarios {
		if scenario.ID == "" {
			return errors.New("scenario.id is required")
		}
		if seen[scenario.ID] {
			return fmt.Errorf("duplicate scenario.id: %s", scenario.ID)
		}
		seen[scenario.ID] = true
		if scenario.Name == "" {
			return fmt.Errorf("scenario.name is required: %s", scenario.ID)
		}
		if len(scenario.Audio) == 0 {
			return fmt.Errorf("scenario.audio must not be empty: %s", scenario.ID)
		}
		for _, step := range scenario.Audio {
			if step.Fixture == "" {
				return fmt.Errorf("scenario.audio.fixture is required: %s", scenario.ID)
			}
		}
		if scenario.Expect.MaxResponseSentences < 0 {
			return fmt.Errorf("expect.max_response_sentences must be >= 0: %s", scenario.ID)
		}
		if scenario.Expect.MinTranscriptSimilarity < 0 || scenario.Expect.MinTranscriptSimilarity > 1 {
			return fmt.Errorf("expect.min_transcript_similarity must be [0,1]: %s", scenario.ID)
		}
	}
	return nil
}
