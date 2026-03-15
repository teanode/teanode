package fsstore

import (
	"context"
	"time"

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
	configuration, err := self.loadConfigurationRecord()
	if err != nil {
		return nil, err
	}
	return configurationToModel(configuration), nil
}

func (self *fileSystemTransaction) modifyConfiguration(ctx context.Context, modifier func(*models.Configuration) error, options *store.Option) (*models.Configuration, error) {
	modelConfiguration, err := self.GetConfiguration(ctx, options)
	if err != nil {
		return nil, err
	}
	if err := modifier(modelConfiguration); err != nil {
		return nil, err
	}
	configuration := modelToConfiguration(modelConfiguration)
	if err := self.saveConfigurationRecord(configuration); err != nil {
		return nil, err
	}
	return modelConfiguration, nil
}

func configurationToModel(configuration *storeConfigurationRecord) *models.Configuration {
	if configuration == nil {
		return &models.Configuration{}
	}
	result := &models.Configuration{}
	nodeConfiguration := &models.NodeConfiguration{}
	nodeConfiguration.Port = ptrto.Value(configuration.Node.Port)
	nodeConfiguration.Bind = ptrto.Trimmed[models.BindMode](configuration.Node.Bind)
	nodeConfiguration.PublicURL = ptrto.TrimmedString(configuration.Node.PublicURL)
	nodeConfiguration.TLS = ptrto.Value(configuration.Node.TLS)
	result.Node = nodeConfiguration

	if configuration.Certificate != nil {
		certificateConfiguration := &models.CertificateConfiguration{
			ACMEEmail:      ptrto.TrimmedString(configuration.Certificate.ACMEEmail),
			ACMEAccountKey: ptrto.TrimmedString(configuration.Certificate.ACMEAccountKey),
			Domain:         ptrto.TrimmedString(configuration.Certificate.Domain),
			Certificate:    ptrto.TrimmedString(configuration.Certificate.Certificate),
			PrivateKey:     ptrto.TrimmedString(configuration.Certificate.PrivateKey),
		}
		if configuration.Certificate.IssuedAt != "" {
			if parsedTime, err := time.Parse(time.RFC3339, configuration.Certificate.IssuedAt); err == nil {
				certificateConfiguration.IssuedAt = &parsedTime
			}
		}
		if configuration.Certificate.ExpiresAt != "" {
			if parsedTime, err := time.Parse(time.RFC3339, configuration.Certificate.ExpiresAt); err == nil {
				certificateConfiguration.ExpiresAt = &parsedTime
			}
		}
		result.Certificate = certificateConfiguration
	}

	modelsConfiguration := &models.ModelsConfiguration{}
	modelsConfiguration.Default = ptrto.TrimmedString(configuration.Models.Default)
	modelsConfiguration.SummarizerProviderModelName = ptrto.TrimmedString(configuration.Models.SummarizerProviderModelName)
	modelsConfiguration.EmbeddingProviderModelName = ptrto.TrimmedString(configuration.Models.EmbeddingProviderModelName)
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
			ModelName:             ptrto.TrimmedString(configuration.Tools.ClaudeCode.ModelName),
			MaxTurnTimeoutSeconds: ptrto.Value(configuration.Tools.ClaudeCode.MaxTurnTimeoutSeconds),
		}
	}
	if configuration.Tools.Codex != nil {
		toolsConfiguration.Codex = &models.CodexConfiguration{
			BinaryPath:            ptrto.TrimmedString(configuration.Tools.Codex.BinaryPath),
			AllowedTools:          ptrto.TrimmedStrings(configuration.Tools.Codex.AllowedTools),
			ModelName:             ptrto.TrimmedString(configuration.Tools.Codex.ModelName),
			ExtraArguments:        ptrto.TrimmedStrings(configuration.Tools.Codex.ExtraArguments),
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
	if len(configuration.ToolPolicies) > 0 {
		toolPolicies := make([]*models.ToolPolicyConfiguration, 0, len(configuration.ToolPolicies))
		for _, tp := range configuration.ToolPolicies {
			tool := tp.Tool
			group := models.ToolPolicyGroup(tp.Group)
			level := models.ToolPolicyLevel(tp.Level)
			toolPolicies = append(toolPolicies, &models.ToolPolicyConfiguration{
				Tool:  &tool,
				Group: &group,
				Level: &level,
			})
		}
		result.ToolPolicies = &toolPolicies
	}
	return result
}

func modelToConfiguration(configuration *models.Configuration) *storeConfigurationRecord {
	result := &storeConfigurationRecord{}
	if configuration == nil {
		return result
	}
	if configuration.Node != nil {
		result.Node.Port = configuration.Node.GetPort()
		result.Node.Bind = string(configuration.Node.GetBind())
		result.Node.PublicURL = configuration.Node.GetPublicURL()
		result.Node.TLS = configuration.Node.GetTLS()
	}
	if configuration.Certificate != nil {
		record := &storeCertificateRecord{
			ACMEEmail:      configuration.Certificate.GetACMEEmail(),
			ACMEAccountKey: configuration.Certificate.GetACMEAccountKey(),
			Domain:         configuration.Certificate.GetDomain(),
			Certificate:    configuration.Certificate.GetCertificate(),
			PrivateKey:     configuration.Certificate.GetPrivateKey(),
		}
		if configuration.Certificate.IssuedAt != nil {
			record.IssuedAt = configuration.Certificate.IssuedAt.Format(time.RFC3339)
		}
		if configuration.Certificate.ExpiresAt != nil {
			record.ExpiresAt = configuration.Certificate.ExpiresAt.Format(time.RFC3339)
		}
		result.Certificate = record
	}
	if configuration.Models != nil {
		result.Models.Default = configuration.Models.GetDefault()
		result.Models.SummarizerProviderModelName = configuration.Models.GetSummarizerProviderModelName()
		result.Models.EmbeddingProviderModelName = configuration.Models.GetEmbeddingProviderModelName()
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
				ModelName:             configuration.Tools.ClaudeCode.GetModelName(),
				MaxTurnTimeoutSeconds: configuration.Tools.ClaudeCode.GetMaxTurnTimeoutSeconds(),
			}
		}
		if configuration.Tools.Codex != nil {
			result.Tools.Codex = &storeCodexToolRecord{
				BinaryPath:            configuration.Tools.Codex.GetBinaryPath(),
				AllowedTools:          sliceValue(configuration.Tools.Codex.AllowedTools),
				ModelName:             configuration.Tools.Codex.GetModelName(),
				ExtraArguments:        sliceValue(configuration.Tools.Codex.ExtraArguments),
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
	if configuration.ToolPolicies != nil {
		for _, tp := range *configuration.ToolPolicies {
			var tool, group, level string
			if tp.Tool != nil {
				tool = *tp.Tool
			}
			if tp.Group != nil {
				group = string(*tp.Group)
			}
			if tp.Level != nil {
				level = string(*tp.Level)
			}
			result.ToolPolicies = append(result.ToolPolicies, storeToolPolicyRecord{
				Tool:  tool,
				Group: group,
				Level: level,
			})
		}
	}
	return result
}
