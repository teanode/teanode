package fsstore

import (
	"context"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func (self *fileSystemTransaction) GetConfiguration(ctx context.Context, options *store.Option) (*models.Configuration, error) {
	return self.getConfiguration(options)
}

func (self *fileSystemTransaction) ModifyConfiguration(ctx context.Context, modifier func(*models.Configuration) error, options *store.Option) (*models.Configuration, error) {
	return self.modifyConfiguration(ctx, modifier, options)
}
func (self *fileSystemTransaction) getConfiguration(options *store.Option) (*models.Configuration, error) {
	configuration, err := self.loadConfigRecord()
	if err != nil {
		return nil, err
	}
	return configToModel(configuration), nil
}

func (self *fileSystemTransaction) modifyConfiguration(ctx context.Context, modifier func(*models.Configuration) error, options *store.Option) (*models.Configuration, error) {
	modelConfiguration, err := self.GetConfiguration(ctx, options)
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
	gatewayConfiguration.Bind = ptrto.Trimmed[models.BindMode](configuration.Gateway.Bind)
	gatewayConfiguration.PublicURL = ptrto.TrimmedString(configuration.Gateway.PublicURL)
	result.Gateway = gatewayConfiguration

	modelsConfiguration := &models.ModelsConfiguration{}
	modelsConfiguration.Default = ptrto.TrimmedString(configuration.Models.Default)
	modelsConfiguration.SummarizerModel = ptrto.TrimmedString(configuration.Models.SummarizerModel)
	modelsConfiguration.ContextWindow = ptrto.Value(configuration.Models.ContextWindow)
	providerConfigurations := make([]*models.ProviderConfiguration, 0, len(configuration.Models.Providers))
	for _, providerConfiguration := range configuration.Models.Providers {
		providerConfigurations = append(providerConfigurations, &models.ProviderConfiguration{
			Name:    ptrto.TrimmedString(providerConfiguration.Name),
			BaseURL: ptrto.TrimmedString(providerConfiguration.BaseURL),
			APIKey:  ptrto.TrimmedString(providerConfiguration.APIKey),
		})
	}
	modelsConfiguration.Providers = &providerConfigurations
	result.Models = modelsConfiguration

	toolsConfiguration := &models.ToolsConfiguration{BraveAPIKey: ptrto.TrimmedString(configuration.Tools.BraveAPIKey)}
	if configuration.Tools.Google != nil {
		toolsConfiguration.Google = &models.GoogleConfiguration{
			BinaryPath: ptrto.TrimmedString(configuration.Tools.Google.BinaryPath),
			Account:    ptrto.TrimmedString(configuration.Tools.Google.Account),
			Services:   ptrto.TrimmedStrings(configuration.Tools.Google.Services),
		}
	}
	if configuration.Tools.GitHub != nil {
		toolsConfiguration.GitHub = &models.GitHubConfiguration{
			BinaryPath: ptrto.TrimmedString(configuration.Tools.GitHub.BinaryPath),
			Services:   ptrto.TrimmedStrings(configuration.Tools.GitHub.Services),
		}
	}
	if configuration.Tools.GitLab != nil {
		toolsConfiguration.GitLab = &models.GitLabConfiguration{
			BinaryPath: ptrto.TrimmedString(configuration.Tools.GitLab.BinaryPath),
			Services:   ptrto.TrimmedStrings(configuration.Tools.GitLab.Services),
		}
	}
	if configuration.Tools.ClaudeCode != nil {
		toolsConfiguration.ClaudeCode = &models.ClaudeCodeConfiguration{
			BinaryPath:            ptrto.TrimmedString(configuration.Tools.ClaudeCode.BinaryPath),
			AllowedTools:          ptrto.TrimmedStrings(configuration.Tools.ClaudeCode.AllowedTools),
			Model:                 ptrto.TrimmedString(configuration.Tools.ClaudeCode.Model),
			MaxTurnTimeoutSeconds: ptrto.Value(configuration.Tools.ClaudeCode.MaxTurnTimeoutSeconds),
		}
	}
	if configuration.Tools.Codex != nil {
		toolsConfiguration.Codex = &models.CodexConfiguration{
			BinaryPath:            ptrto.TrimmedString(configuration.Tools.Codex.BinaryPath),
			AllowedTools:          ptrto.TrimmedStrings(configuration.Tools.Codex.AllowedTools),
			Model:                 ptrto.TrimmedString(configuration.Tools.Codex.Model),
			ExtraArgs:             ptrto.TrimmedStrings(configuration.Tools.Codex.ExtraArgs),
			MaxTurnTimeoutSeconds: ptrto.Value(configuration.Tools.Codex.MaxTurnTimeoutSeconds),
		}
	}
	if configuration.Tools.HomeAssistant != nil {
		toolsConfiguration.HomeAssistant = &models.HomeAssistantConfiguration{
			BaseURL:         ptrto.TrimmedString(configuration.Tools.HomeAssistant.BaseURL),
			Token:           ptrto.TrimmedString(configuration.Tools.HomeAssistant.Token),
			ReadOnly:        ptrto.Value(configuration.Tools.HomeAssistant.ReadOnly),
			AllowedDomains:  ptrto.TrimmedStrings(configuration.Tools.HomeAssistant.AllowedDomains),
			BlockedDomains:  ptrto.TrimmedStrings(configuration.Tools.HomeAssistant.BlockedDomains),
			AllowedEntities: ptrto.TrimmedStrings(configuration.Tools.HomeAssistant.AllowedEntities),
			TimeoutSeconds:  ptrto.Value(configuration.Tools.HomeAssistant.TimeoutSeconds),
		}
	}
	if configuration.Tools.UniFiProtect != nil {
		toolsConfiguration.UniFiProtect = &models.UniFiProtectConfiguration{
			BaseURL:               ptrto.TrimmedString(configuration.Tools.UniFiProtect.BaseURL),
			APIKey:                ptrto.TrimmedString(configuration.Tools.UniFiProtect.APIKey),
			Username:              ptrto.TrimmedString(configuration.Tools.UniFiProtect.Username),
			Password:              ptrto.TrimmedString(configuration.Tools.UniFiProtect.Password),
			VerifyTLS:             ptrto.Value(configuration.Tools.UniFiProtect.VerifyTLS),
			ReadOnly:              ptrto.Value(configuration.Tools.UniFiProtect.ReadOnly),
			AllowedCameras:        ptrto.TrimmedStrings(configuration.Tools.UniFiProtect.AllowedCameras),
			AllowDangerousActions: ptrto.TrimmedStrings(configuration.Tools.UniFiProtect.AllowDangerousActions),
			TimeoutSeconds:        ptrto.Value(configuration.Tools.UniFiProtect.TimeoutSeconds),
		}
	}
	result.Tools = toolsConfiguration
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
	secretConfigurations := make([]*models.SecretConfiguration, 0, len(configuration.Secrets))
	for key, value := range configuration.Secrets {
		keyCopy := key
		valueCopy := value
		secretConfigurations = append(secretConfigurations, &models.SecretConfiguration{Key: &keyCopy, Value: &valueCopy})
	}
	result.Secrets = &secretConfigurations
	skillRegistries := make([]*models.SkillRegistryConfiguration, 0, len(configuration.SkillsRegistries))
	for _, registry := range configuration.SkillsRegistries {
		skillRegistries = append(skillRegistries, &models.SkillRegistryConfiguration{
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
		result.Gateway.Port = configuration.Gateway.GetPort()
		result.Gateway.Bind = string(configuration.Gateway.GetBind())
		result.Gateway.PublicURL = configuration.Gateway.GetPublicURL()
	}
	if configuration.Models != nil {
		result.Models.Default = configuration.Models.GetDefault()
		result.Models.SummarizerModel = configuration.Models.GetSummarizerModel()
		result.Models.ContextWindow = configuration.Models.GetContextWindow()
		if configuration.Models.Providers != nil {
			for _, providerConfiguration := range *configuration.Models.Providers {
				result.Models.Providers = append(result.Models.Providers, storeProviderRecord{
					Name:    providerConfiguration.GetName(),
					BaseURL: providerConfiguration.GetBaseURL(),
					APIKey:  providerConfiguration.GetAPIKey(),
				})
			}
		}
	}
	if configuration.Tools != nil {
		result.Tools.BraveAPIKey = configuration.Tools.GetBraveAPIKey()
		if configuration.Tools.Google != nil {
			result.Tools.Google = &storeGoogleToolRecord{
				BinaryPath: configuration.Tools.Google.GetBinaryPath(),
				Account:    configuration.Tools.Google.GetAccount(),
				Services:   sliceValue(configuration.Tools.Google.Services),
			}
		}
		if configuration.Tools.GitHub != nil {
			result.Tools.GitHub = &storeGitHubToolRecord{
				BinaryPath: configuration.Tools.GitHub.GetBinaryPath(),
				Services:   sliceValue(configuration.Tools.GitHub.Services),
			}
		}
		if configuration.Tools.GitLab != nil {
			result.Tools.GitLab = &storeGitLabToolRecord{
				BinaryPath: configuration.Tools.GitLab.GetBinaryPath(),
				Services:   sliceValue(configuration.Tools.GitLab.Services),
			}
		}
		if configuration.Tools.ClaudeCode != nil {
			result.Tools.ClaudeCode = &storeClaudeCodeToolRecord{
				BinaryPath:            configuration.Tools.ClaudeCode.GetBinaryPath(),
				AllowedTools:          sliceValue(configuration.Tools.ClaudeCode.AllowedTools),
				Model:                 configuration.Tools.ClaudeCode.GetModel(),
				MaxTurnTimeoutSeconds: configuration.Tools.ClaudeCode.GetMaxTurnTimeoutSeconds(),
			}
		}
		if configuration.Tools.Codex != nil {
			result.Tools.Codex = &storeCodexToolRecord{
				BinaryPath:            configuration.Tools.Codex.GetBinaryPath(),
				AllowedTools:          sliceValue(configuration.Tools.Codex.AllowedTools),
				Model:                 configuration.Tools.Codex.GetModel(),
				ExtraArgs:             sliceValue(configuration.Tools.Codex.ExtraArgs),
				MaxTurnTimeoutSeconds: configuration.Tools.Codex.GetMaxTurnTimeoutSeconds(),
			}
		}
		if configuration.Tools.HomeAssistant != nil {
			result.Tools.HomeAssistant = &storeHomeAssistantRecord{
				BaseURL:         configuration.Tools.HomeAssistant.GetBaseURL(),
				Token:           configuration.Tools.HomeAssistant.GetToken(),
				ReadOnly:        configuration.Tools.HomeAssistant.GetReadOnly(),
				AllowedDomains:  sliceValue(configuration.Tools.HomeAssistant.AllowedDomains),
				BlockedDomains:  sliceValue(configuration.Tools.HomeAssistant.BlockedDomains),
				AllowedEntities: sliceValue(configuration.Tools.HomeAssistant.AllowedEntities),
				TimeoutSeconds:  configuration.Tools.HomeAssistant.GetTimeoutSeconds(),
			}
		}
		if configuration.Tools.UniFiProtect != nil {
			result.Tools.UniFiProtect = &storeUniFiProtectRecord{
				BaseURL:               configuration.Tools.UniFiProtect.GetBaseURL(),
				APIKey:                configuration.Tools.UniFiProtect.GetAPIKey(),
				Username:              configuration.Tools.UniFiProtect.GetUsername(),
				Password:              configuration.Tools.UniFiProtect.GetPassword(),
				VerifyTLS:             configuration.Tools.UniFiProtect.GetVerifyTLS(),
				ReadOnly:              configuration.Tools.UniFiProtect.GetReadOnly(),
				AllowedCameras:        sliceValue(configuration.Tools.UniFiProtect.AllowedCameras),
				AllowDangerousActions: sliceValue(configuration.Tools.UniFiProtect.AllowDangerousActions),
				TimeoutSeconds:        configuration.Tools.UniFiProtect.GetTimeoutSeconds(),
			}
		}
	}
	if configuration.Integrations != nil && configuration.Integrations.Browser != nil {
		result.Integrations.Browser = &storeBrowserRecord{CDPEndpoint: configuration.Integrations.Browser.GetCDPEndpoint()}
	}
	if configuration.Channels != nil {
		if configuration.Channels.Discord != nil {
			result.Channels.Discord = &storeDiscordRecord{Token: configuration.Channels.Discord.GetToken()}
		}
		if configuration.Channels.Telegram != nil {
			result.Channels.Telegram = &storeTelegramRecord{Token: configuration.Channels.Telegram.GetToken()}
		}
	}
	if configuration.Secrets != nil {
		result.Secrets = map[string]string{}
		for _, secret := range *configuration.Secrets {
			result.Secrets[secret.GetKey()] = secret.GetValue()
		}
	}
	if configuration.SkillsRegistries != nil {
		for _, registry := range *configuration.SkillsRegistries {
			result.SkillsRegistries = append(result.SkillsRegistries, storeSkillRegistryRecord{
				ID:               registry.GetID(),
				Publisher:        registry.GetPublisher(),
				IndexURL:         registry.GetIndexURL(),
				PublicKeys:       sliceValue(registry.PublicKeys),
				IgnoreSignatures: registry.GetIgnoreSignatures(),
				IgnoreUpdates:    registry.GetIgnoreUpdates(),
			})
		}
	}
	return result
}
