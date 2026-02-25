package fs

import (
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func (self *transaction) GetConfiguration(options *store.Option) (*models.Configuration, error) {
	return self.getConfiguration(options)
}

func (self *transaction) ModifyConfiguration(modifier func(*models.Configuration) error, options *store.Option) (*models.Configuration, error) {
	return self.modifyConfiguration(modifier, options)
}
func (self *transaction) getConfiguration(options *store.Option) (*models.Configuration, error) {
	configuration, err := self.loadConfigRecord()
	if err != nil {
		return nil, err
	}
	return configToModel(configuration), nil
}

func (self *transaction) modifyConfiguration(modifier func(*models.Configuration) error, options *store.Option) (*models.Configuration, error) {
	modelConfiguration, err := self.GetConfiguration(options)
	if err != nil {
		return nil, err
	}
	if err := modifier(modelConfiguration); err != nil {
		return nil, err
	}
	configuration := modelToConfig(modelConfiguration)
	if err := self.saveConfigRecord(configuration); err != nil {
		return nil, err
	}
	return modelConfiguration, nil
}

func configToModel(configuration *storeConfigRecord) *models.Configuration {
	if configuration == nil {
		return &models.Configuration{}
	}
	result := &models.Configuration{}
	gatewayConfiguration := &models.GatewayConfiguration{}
	gatewayConfiguration.Port = ptrto.Value(configuration.Gateway.Port)
	gatewayConfiguration.Bind = ptrto.TrimmedString(configuration.Gateway.Bind)
	gatewayConfiguration.PublicURL = ptrto.TrimmedString(configuration.Gateway.PublicURL)
	gatewayConfiguration.Security = &models.GatewaySecurityConfiguration{}
	if configuration.Gateway.Auth != nil {
		gatewayConfiguration.Security.SessionMaxAgeDays = ptrto.Value(configuration.Gateway.Auth.SessionMaxAgeDays)
	}
	gatewayConfiguration.Security.ForwarderKey = ptrto.TrimmedString(configuration.Gateway.ForwarderKey)
	result.Gateway = gatewayConfiguration

	modelsConfiguration := &models.ModelsConfiguration{}
	modelsConfiguration.Default = ptrto.TrimmedString(configuration.Models.Default)
	modelsConfiguration.SummarizerModel = ptrto.TrimmedString(configuration.Models.SummarizerModel)
	modelsConfiguration.ContextWindow = ptrto.Value(configuration.Models.ContextWindow)
	providerConfigurations := make([]models.ProviderConfiguration, 0, len(configuration.Models.Providers))
	for _, providerConfiguration := range configuration.Models.Providers {
		providerConfigurations = append(providerConfigurations, models.ProviderConfiguration{
			Name:    ptrto.TrimmedString(providerConfiguration.Name),
			BaseURL: ptrto.TrimmedString(providerConfiguration.BaseURL),
			APIKey:  ptrto.TrimmedString(providerConfiguration.APIKey),
		})
	}
	modelsConfiguration.Providers = &providerConfigurations
	result.Models = modelsConfiguration

	result.Tools = &models.ToolsConfiguration{BraveAPIKey: ptrto.TrimmedString(configuration.Tools.BraveAPIKey)}
	result.Integrations = &models.IntegrationsConfiguration{}
	if configuration.Integrations.Browser != nil {
		result.Integrations.Browser = &models.BrowserConfiguration{CDPEndpoint: ptrto.TrimmedString(configuration.Integrations.Browser.CDPEndpoint)}
	}
	result.Channels = &models.ChannelsConfiguration{}
	if configuration.Channels.Discord != nil {
		result.Channels.Discord = &models.DiscordConfiguration{Token: ptrto.TrimmedString(configuration.Channels.Discord.Token)}
	}
	if configuration.Channels.Telegram != nil {
		result.Channels.Telegram = &models.TelegramConfiguration{Token: ptrto.TrimmedString(configuration.Channels.Telegram.Token)}
	}
	secretConfigurations := make([]models.SecretConfiguration, 0, len(configuration.Secrets))
	for key, value := range configuration.Secrets {
		keyCopy := key
		valueCopy := value
		secretConfigurations = append(secretConfigurations, models.SecretConfiguration{Key: &keyCopy, Value: &valueCopy})
	}
	result.Secrets = &secretConfigurations
	skillRegistries := make([]models.SkillRegistryConfiguration, 0, len(configuration.SkillsRegistries))
	for _, registry := range configuration.SkillsRegistries {
		skillRegistries = append(skillRegistries, models.SkillRegistryConfiguration{
			ID:               ptrto.TrimmedString(registry.ID),
			Publisher:        ptrto.TrimmedString(registry.Publisher),
			IndexURL:         ptrto.TrimmedString(registry.IndexURL),
			PublicKeys:       ptrto.TrimmedStrings(registry.PublicKeys),
			IgnoreSignatures: ptrto.Value(registry.IgnoreSignatures),
			IgnoreUpdates:    ptrto.Value(registry.IgnoreUpdates),
		})
	}
	result.SkillsRegistries = &skillRegistries
	return result
}

func modelToConfig(configuration *models.Configuration) *storeConfigRecord {
	result := &storeConfigRecord{}
	if configuration == nil {
		return result
	}
	if configuration.Gateway != nil {
		result.Gateway.Port = intValue(configuration.Gateway.Port)
		result.Gateway.Bind = valueOrEmpty(configuration.Gateway.Bind)
		result.Gateway.PublicURL = valueOrEmpty(configuration.Gateway.PublicURL)
		if configuration.Gateway.Security != nil {
			result.Gateway.ForwarderKey = valueOrEmpty(configuration.Gateway.Security.ForwarderKey)
			result.Gateway.Auth = &storeGatewayAuthRecord{SessionMaxAgeDays: intValue(configuration.Gateway.Security.SessionMaxAgeDays)}
		}
	}
	if configuration.Models != nil {
		result.Models.Default = valueOrEmpty(configuration.Models.Default)
		result.Models.SummarizerModel = valueOrEmpty(configuration.Models.SummarizerModel)
		result.Models.ContextWindow = intValue(configuration.Models.ContextWindow)
		if configuration.Models.Providers != nil {
			for _, providerConfiguration := range *configuration.Models.Providers {
				result.Models.Providers = append(result.Models.Providers, storeProviderRecord{
					Name:    valueOrEmpty(providerConfiguration.Name),
					BaseURL: valueOrEmpty(providerConfiguration.BaseURL),
					APIKey:  valueOrEmpty(providerConfiguration.APIKey),
				})
			}
		}
	}
	if configuration.Tools != nil {
		result.Tools.BraveAPIKey = valueOrEmpty(configuration.Tools.BraveAPIKey)
	}
	if configuration.Integrations != nil && configuration.Integrations.Browser != nil {
		result.Integrations.Browser = &storeBrowserRecord{CDPEndpoint: valueOrEmpty(configuration.Integrations.Browser.CDPEndpoint)}
	}
	if configuration.Channels != nil {
		if configuration.Channels.Discord != nil {
			result.Channels.Discord = &storeDiscordRecord{Token: valueOrEmpty(configuration.Channels.Discord.Token)}
		}
		if configuration.Channels.Telegram != nil {
			result.Channels.Telegram = &storeTelegramRecord{Token: valueOrEmpty(configuration.Channels.Telegram.Token)}
		}
	}
	if configuration.Secrets != nil {
		result.Secrets = map[string]string{}
		for _, secret := range *configuration.Secrets {
			result.Secrets[valueOrEmpty(secret.Key)] = valueOrEmpty(secret.Value)
		}
	}
	if configuration.SkillsRegistries != nil {
		for _, registry := range *configuration.SkillsRegistries {
			result.SkillsRegistries = append(result.SkillsRegistries, storeSkillRegistryRecord{
				ID:               valueOrEmpty(registry.ID),
				Publisher:        valueOrEmpty(registry.Publisher),
				IndexURL:         valueOrEmpty(registry.IndexURL),
				PublicKeys:       sliceValue(registry.PublicKeys),
				IgnoreSignatures: boolValue(registry.IgnoreSignatures),
				IgnoreUpdates:    boolValue(registry.IgnoreUpdates),
			})
		}
	}
	return result
}
