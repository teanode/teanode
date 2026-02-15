package cmd

import (
	"context"

	"github.com/teanode/teanode/internal/agent"
	"github.com/teanode/teanode/internal/browser"
	configpkg "github.com/teanode/teanode/internal/config"
	"github.com/teanode/teanode/internal/cron"
	"github.com/teanode/teanode/internal/discord"
	"github.com/teanode/teanode/internal/gateway"
	"github.com/teanode/teanode/internal/logging"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/provider"
	"github.com/teanode/teanode/internal/session"
	"github.com/teanode/teanode/internal/skill"
	"github.com/teanode/teanode/internal/telegram"
	tterminal "github.com/teanode/teanode/internal/terminal"
	"github.com/teanode/teanode/internal/tools/fetch"
	"github.com/teanode/teanode/internal/tools/search"
	"github.com/teanode/teanode/internal/tools/workspace"
	"github.com/teanode/teanode/internal/watcher"
	"github.com/urfave/cli/v3"
)

func GatewayCmd() *cli.Command {
	return &cli.Command{
		Name:  "gateway",
		Usage: "Start the TeaNode gateway server",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "port",
				Aliases: []string{"p"},
				Usage:   "port to listen on (overrides config)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			log := logging.Get("cmd")

			// Run directory migrations before anything else.
			if err := configpkg.MigrateToAgentDirs(); err != nil {
				log.Errorf("migration error (non-fatal): %v", err)
			}
			if err := configpkg.MigrateAgentsToFiles(); err != nil {
				log.Errorf("agent migration error (non-fatal): %v", err)
			}

			// Ensure base directories exist.
			if err := configpkg.EnsureDirs(); err != nil {
				return err
			}

			// Load config.
			configuration, err := configpkg.Load()
			if err != nil {
				return err
			}

			// CLI flag overrides config.
			if cmd.IsSet("port") {
				configuration.Gateway.Port = int(cmd.Int("port"))
			}

			// Build provider registry.
			buildProviderRegistry := func(configuration *configpkg.Config) *provider.Registry {
				registry := provider.NewRegistry(configuration.Models.DefaultProviderName())
				for name, providerConfig := range configuration.Models.ResolvedProviders() {
					registry.Register(name, provider.NewClient(providerConfig.BaseURL, providerConfig.APIKey))
				}
				return registry
			}
			providers := buildProviderRegistry(configuration)

			// Validate that at least one provider has an API key.
			hasKey := false
			for _, providerConfig := range configuration.Models.ResolvedProviders() {
				if providerConfig.APIKey != "" {
					hasKey = true
					break
				}
			}
			if !hasKey {
				log.Warning("no API key configured (set OPENAI_API_KEY or models.apiKey in config)")
			}

			// --- Shared resources ---

			// Browser: always create relay for extension connections, optionally
			// add headless CDP backend. Both can be active simultaneously.
			browserRelay := browser.NewRelay()
			backends := []browser.Browser{browserRelay}

			var headlessBrowser *browser.Headless
			if configuration.Browser != nil && configuration.Browser.CDPEndpoint != "" {
				log.Infof("browser: headless CDP connecting to %s", configuration.Browser.CDPEndpoint)
				headlessBrowser = browser.NewHeadless(configuration.Browser.CDPEndpoint)
				if err := headlessBrowser.Connect(ctx); err != nil {
					log.Errorf("headless browser failed to connect: %v", err)
				}
				headlessBrowser.StartReconnectLoop(ctx)
				defer headlessBrowser.Close()
				backends = append(backends, headlessBrowser)
			}
			log.Info("browser: relay accepting extension connections on /api/browser")
			browserBackend := browser.NewCompositeBrowser(backends...)

			terminalRelay := tterminal.NewRelay()

			skillsDirectory, err := configpkg.SkillsDir()
			if err != nil {
				return err
			}

			mediaDirectory, err := configpkg.MediaDir()
			if err != nil {
				return err
			}
			mediaStore := media.NewStore(mediaDirectory)

			// --- Agent Registry: create a runner per agent ---

			agentRegistry := agent.NewAgentRegistry()

			// buildToolsForAgent creates a fresh tool registry for the given agent.
			buildToolsForAgent := func(
				configuration *configpkg.Config,
				agentConfig configpkg.AgentConfig,
				workspaceDirectory string,
				sessions *session.Store,
				scheduler *cron.Scheduler,
			) (*agent.ToolRegistry, string) {
				tools := agent.NewToolRegistry()
				workspace.RegisterTools(tools, workspaceDirectory)
				browser.RegisterBrowserTools(tools, browserBackend)
				tterminal.RegisterTerminalTools(tools, terminalRelay)
				search.RegisterTools(tools, configuration.Tools.BraveAPIKey)
				fetch.RegisterTools(tools)
				agent.RegisterSessionTools(tools, sessions)
				if scheduler != nil {
					cron.RegisterCronTools(tools, scheduler)
				}
				skillPrompts := skill.RegisterSkillsFiltered(tools, skillsDirectory, agentConfig.Skills)
				agent.RegisterInterAgentTools(tools, agentConfig.ID, agentRegistry, configuration)
				tools.ApplyFilter(agentConfig.Tools)
				return tools, skillPrompts
			}

			// Set up cron scheduler (needs agent registry).
			cronStore, err := cron.NewStore()
			if err != nil {
				return err
			}
			scheduler := cron.NewScheduler(cronStore, agentRegistry)

			// Create a runner for each configured agent.
			for _, agentConfig := range configuration.ResolveAgents() {
				if err := configpkg.EnsureAgentDirs(agentConfig.ID); err != nil {
					return err
				}
				if err := configpkg.SeedAgentWorkspace(agentConfig.ID); err != nil {
					return err
				}

				workspaceDirectory, err := configpkg.AgentWorkspaceDir(agentConfig.ID)
				if err != nil {
					return err
				}
				sessionsDirectory, err := configpkg.AgentSessionsDir(agentConfig.ID)
				if err != nil {
					return err
				}
				sessions := session.NewStore(sessionsDirectory)

				tools, skillPrompts := buildToolsForAgent(configuration, agentConfig, workspaceDirectory, sessions, scheduler)

				runner := &agent.Runner{
					AgentID:      agentConfig.ID,
					Providers:    providers,
					Sessions:     sessions,
					Config:       configuration,
					Tools:        tools,
					MediaStore:   mediaStore,
					WorkspaceDir: workspaceDirectory,
					SkillPrompts: skillPrompts,
				}
				agentRegistry.Register(agentConfig.ID, runner)
			}

			// Set the default agent ID from config.
			agentRegistry.SetDefault(configuration.ResolveDefaultAgent())

			// --- Server ---

			summarizer := agent.NewSummarizer(agentRegistry, configuration)

			server := &gateway.Server{
				Config:        configuration,
				AgentRegistry: agentRegistry,
				BrowserRelay:  browserRelay,
				TerminalRelay: terminalRelay,
				Scheduler:     scheduler,
				Summarizer:    summarizer,
				MediaStore:    mediaStore,
			}

			summarizer.IsSessionActive = func(sessionKey string) bool {
				return server.GetActiveRun(sessionKey) != ""
			}
			summarizer.Broadcast = server.Broadcast

			scheduler.Broadcast = server.Broadcast
			scheduler.SetActiveRun = server.SetActiveRun
			scheduler.ClearActiveRun = server.ClearActiveRun

			// --- Discord bot ---

			if configuration.Discord != nil && configuration.Discord.Token != "" {
				discordBot, err := discord.New(configuration.Discord, agentRegistry)
				if err != nil {
					log.Errorf("discord bot: %v", err)
				} else {
					discordBot.Broadcast = server.Broadcast
					discordBot.SetActiveRun = server.SetActiveRun
					discordBot.ClearActiveRun = server.ClearActiveRun
					if err := discordBot.Start(); err != nil {
						log.Errorf("discord bot failed to start: %v", err)
					} else {
						defer discordBot.Stop()
					}
				}
			}

			// --- Telegram bot ---

			if configuration.Telegram != nil && configuration.Telegram.Token != "" {
				telegramBot, err := telegram.New(configuration.Telegram, agentRegistry)
				if err != nil {
					log.Errorf("telegram bot: %v", err)
				} else {
					telegramBot.Broadcast = server.Broadcast
					telegramBot.SetActiveRun = server.SetActiveRun
					telegramBot.ClearActiveRun = server.ClearActiveRun
					if err := telegramBot.Start(); err != nil {
						log.Errorf("telegram bot failed to start: %v", err)
					} else {
						defer telegramBot.Stop()
					}
				}
			}

			// --- File watcher for hot reloading ---

			dataDirectory, err := configpkg.Dir()
			if err != nil {
				return err
			}

			fileWatcher := watcher.New(dataDirectory)

			// reloadAgents reconfigures agent runners from the current server config.
			reloadAgents := func() {
				currentConfiguration := server.Config
				currentProviders := buildProviderRegistry(currentConfiguration)
				agentRegistry.SetDefault(currentConfiguration.ResolveDefaultAgent())

				for _, agentConfig := range currentConfiguration.ResolveAgents() {
					runner := agentRegistry.Get(agentConfig.ID)
					if runner == nil {
						// New agent appeared — create it.
						if err := configpkg.EnsureAgentDirs(agentConfig.ID); err != nil {
							log.Errorf("failed to create dirs for new agent %s: %v", agentConfig.ID, err)
							continue
						}
						if err := configpkg.SeedAgentWorkspace(agentConfig.ID); err != nil {
							log.Errorf("failed to seed workspace for new agent %s: %v", agentConfig.ID, err)
							continue
						}
						workspaceDirectory, err := configpkg.AgentWorkspaceDir(agentConfig.ID)
						if err != nil {
							continue
						}
						sessionsDirectory, err := configpkg.AgentSessionsDir(agentConfig.ID)
						if err != nil {
							continue
						}
						sessions := session.NewStore(sessionsDirectory)
						tools, skillPrompts := buildToolsForAgent(currentConfiguration, agentConfig, workspaceDirectory, sessions, scheduler)
						runner = &agent.Runner{
							AgentID:      agentConfig.ID,
							Providers:    currentProviders,
							Sessions:     sessions,
							Config:       currentConfiguration,
							Tools:        tools,
							MediaStore:   mediaStore,
							WorkspaceDir: workspaceDirectory,
							SkillPrompts: skillPrompts,
						}
						agentRegistry.Register(agentConfig.ID, runner)
						continue
					}

					// Existing agent — rebuild tools and reconfigure.
					workspaceDirectory, err := configpkg.AgentWorkspaceDir(agentConfig.ID)
					if err != nil {
						continue
					}
					tools, skillPrompts := buildToolsForAgent(currentConfiguration, agentConfig, workspaceDirectory, runner.Sessions, scheduler)
					runner.Reconfigure(currentConfiguration, currentProviders, tools, skillPrompts)
				}
			}

			fileWatcher.OnConfigReload = func() {
				log.Info("hot-reloading config")
				newConfiguration, err := configpkg.Load()
				if err != nil {
					log.Errorf("failed to reload config: %v", err)
					return
				}
				// Preserve CLI port override.
				if cmd.IsSet("port") {
					newConfiguration.Gateway.Port = int(cmd.Int("port"))
				}

				server.Config = newConfiguration
				summarizer.SetConfig(newConfiguration)
				server.InvalidateModelsCache()
				reloadAgents()
				log.Info("config reloaded successfully")
			}

			fileWatcher.OnAgentsReload = func() {
				log.Info("hot-reloading agents")
				newConfiguration, err := configpkg.Load()
				if err != nil {
					log.Errorf("failed to reload agents: %v", err)
					return
				}
				// Preserve CLI port override.
				if cmd.IsSet("port") {
					newConfiguration.Gateway.Port = int(cmd.Int("port"))
				}
				server.Config = newConfiguration
				summarizer.SetConfig(newConfiguration)
				reloadAgents()
				log.Info("agents reloaded successfully")
			}

			fileWatcher.OnSkillsReload = func() {
				log.Info("hot-reloading skills")
				agentRegistry.ForEach(func(agentId string, runner *agent.Runner) {
					currentConfig, currentProviders, _, _, _ := runner.Snapshot()
					agentConfig := currentConfig.AgentByID(agentId)
					if agentConfig == nil {
						return
					}
					workspaceDirectory, err := configpkg.AgentWorkspaceDir(agentId)
					if err != nil {
						return
					}
					tools, skillPrompts := buildToolsForAgent(currentConfig, *agentConfig, workspaceDirectory, runner.Sessions, scheduler)
					runner.Reconfigure(currentConfig, currentProviders, tools, skillPrompts)
				})
				log.Info("skills reloaded successfully")
			}

			fileWatcher.OnCronsReload = func() {
				log.Info("hot-reloading cron jobs")
				if err := scheduler.Reload(); err != nil {
					log.Errorf("failed to reload cron jobs: %v", err)
				} else {
					log.Info("cron jobs reloaded successfully")
				}
			}

			if err := fileWatcher.Start(); err != nil {
				log.Errorf("file watcher failed to start: %v", err)
			} else {
				defer fileWatcher.Stop()
			}

			return server.Start(ctx)
		},
	}
}
