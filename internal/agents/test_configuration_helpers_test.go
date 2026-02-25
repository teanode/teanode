package agents

import "github.com/teanode/teanode/internal/models"

// modelRuntimeLimitsStub provides test defaults matching the hardcoded
// constants used by truncateOldToolResults. This is a temporary stub until
// the ModelRuntimeLimits type lands in models.
type modelRuntimeLimitsStub struct {
	MinKeepMessages   int
	MaxToolResultChars int
}

func defaultModelRuntimeLimits() modelRuntimeLimitsStub {
	return modelRuntimeLimitsStub{
		MinKeepMessages:   10,
		MaxToolResultChars: 8000,
	}
}

func testConfiguration(defaultModel string, providerName string, providerBaseURL string) *models.Configuration {
	configuration := &models.Configuration{
		Models: &models.ModelsConfiguration{},
	}
	if defaultModel != "" {
		configuration.Models.Default = &defaultModel
	}
	if providerName != "" {
		providers := []*models.ProviderConfiguration{
			{
				Name:    &providerName,
				BaseURL: &providerBaseURL,
			},
		}
		configuration.Models.Providers = &providers
	}
	return configuration
}
