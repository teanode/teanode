//go:build ignore

package main

import (
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/util/datastruct"
)

func main() {
	generator := datastruct.NewGenerator("models", "github.com/teanode/teanode/internal/util/datastruct")

	// Entity types
	generator.GenerateUpdate(new(models.Agent))
	generator.GenerateUpdate(new(models.Conversation))
	generator.GenerateUpdate(new(models.ConversationMessage))
	generator.GenerateUpdate(new(models.Job))
	generator.GenerateUpdate(new(models.Media))
	generator.GenerateUpdate(new(models.MemoryItem))
	generator.GenerateUpdate(new(models.Project))
	generator.GenerateUpdate(new(models.Session))
	generator.GenerateUpdate(new(models.Skill))
	generator.GenerateUpdate(new(models.Token))
	generator.GenerateUpdate(new(models.Todo))
	generator.GenerateUpdate(new(models.Usage))
	generator.GenerateUpdate(new(models.User))
	generator.GenerateUpdate(new(models.WorkspaceFile))

	// Configuration types
	generator.GenerateUpdate(new(models.Configuration))
	generator.GenerateUpdate(new(models.NodeConfiguration))
	generator.GenerateUpdate(new(models.CertificateConfiguration))
	generator.GenerateUpdate(new(models.ModelsConfiguration))
	generator.GenerateUpdate(new(models.ProviderConfiguration))
	generator.GenerateUpdate(new(models.ToolsConfiguration))

	generator.GenerateUpdate(new(models.GoogleConfiguration))
	generator.GenerateUpdate(new(models.GitHubConfiguration))
	generator.GenerateUpdate(new(models.GitLabConfiguration))
	generator.GenerateUpdate(new(models.ClaudeCodeConfiguration))
	generator.GenerateUpdate(new(models.CodexConfiguration))
	generator.GenerateUpdate(new(models.HomeAssistantConfiguration))
	generator.GenerateUpdate(new(models.UniFiProtectConfiguration))
	generator.GenerateUpdate(new(models.IntegrationsConfiguration))
	generator.GenerateUpdate(new(models.BrowserConfiguration))
	generator.GenerateUpdate(new(models.TerminalConfiguration))
	generator.GenerateUpdate(new(models.ChannelsConfiguration))
	generator.GenerateUpdate(new(models.DiscordConfiguration))
	generator.GenerateUpdate(new(models.TelegramConfiguration))
	generator.GenerateUpdate(new(models.SecretConfiguration))
	generator.GenerateUpdate(new(models.ToolPolicyConfiguration))

	// Getter methods for entity types
	generator.GenerateGetters(new(models.Agent))
	generator.GenerateGetters(new(models.Conversation))
	generator.GenerateGetters(new(models.ConversationMessage))
	generator.GenerateGetters(new(models.Job))
	generator.GenerateGetters(new(models.Media))
	generator.GenerateGetters(new(models.MemoryItem))
	generator.GenerateGetters(new(models.Project))
	generator.GenerateGetters(new(models.Session))
	generator.GenerateGetters(new(models.Skill))
	generator.GenerateGetters(new(models.Todo))
	generator.GenerateGetters(new(models.Token))
	generator.GenerateGetters(new(models.Usage))
	generator.GenerateGetters(new(models.User))
	generator.GenerateGetters(new(models.WorkspaceFile))

	// Getter methods for configuration types
	generator.GenerateGetters(new(models.Configuration))
	generator.GenerateGetters(new(models.NodeConfiguration))
	generator.GenerateGetters(new(models.CertificateConfiguration))
	generator.GenerateGetters(new(models.ModelsConfiguration))
	generator.GenerateGetters(new(models.ProviderConfiguration))
	generator.GenerateGetters(new(models.ToolsConfiguration))

	generator.GenerateGetters(new(models.GoogleConfiguration))
	generator.GenerateGetters(new(models.GitHubConfiguration))
	generator.GenerateGetters(new(models.GitLabConfiguration))
	generator.GenerateGetters(new(models.ClaudeCodeConfiguration))
	generator.GenerateGetters(new(models.CodexConfiguration))
	generator.GenerateGetters(new(models.HomeAssistantConfiguration))
	generator.GenerateGetters(new(models.UniFiProtectConfiguration))
	generator.GenerateGetters(new(models.IntegrationsConfiguration))
	generator.GenerateGetters(new(models.BrowserConfiguration))
	generator.GenerateGetters(new(models.TerminalConfiguration))
	generator.GenerateGetters(new(models.ChannelsConfiguration))
	generator.GenerateGetters(new(models.DiscordConfiguration))
	generator.GenerateGetters(new(models.TelegramConfiguration))
	generator.GenerateGetters(new(models.SecretConfiguration))
	generator.GenerateGetters(new(models.ToolPolicyConfiguration))

	generator.MustWriteFile("models_gen.go")
}
