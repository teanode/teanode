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
			// Ensure directories exist.
			if err := configpkg.EnsureDirs(); err != nil {
				return err
			}

			// Seed workspace with default files.
			if err := configpkg.SeedWorkspace(); err != nil {
				return err
			}

			// Load config.
			config, err := configpkg.Load()
			if err != nil {
				return err
			}

			// CLI flag overrides config.
			if cmd.IsSet("port") {
				config.Gateway.Port = int(cmd.Int("port"))
			}

			// Build provider registry.
			buildRegistry := func(cfg *configpkg.Config) *provider.Registry {
				registry := provider.NewRegistry(cfg.Models.DefaultProviderName())
				for name, pc := range cfg.Models.ResolvedProviders() {
					registry.Register(name, provider.NewClient(pc.BaseURL, pc.APIKey))
				}
				return registry
			}
			providers := buildRegistry(config)

			// Validate that at least one provider has an API key.
			hasKey := false
			for _, pc := range config.Models.ResolvedProviders() {
				if pc.APIKey != "" {
					hasKey = true
					break
				}
			}
			if !hasKey {
				log.Warning("no API key configured (set OPENAI_API_KEY or models.apiKey in config)")
			}

			sessions, err := session.NewStoreDefault()
			if err != nil {
				return err
			}

			// Set up tool registry with memory tools.
			tools := agent.NewToolRegistry()
			workspaceDirectory, err := configpkg.WorkspaceDir()
			if err != nil {
				return err
			}
			agent.RegisterMemoryTools(tools, workspaceDirectory)

			// Browser: always create relay for extension connections, optionally
			// add headless CDP backend. Both can be active simultaneously.
			browserRelay := browser.NewRelay()
			backends := []browser.Browser{browserRelay}

			var headlessBrowser *browser.Headless
			if config.Browser != nil && config.Browser.CDPEndpoint != "" {
				log.Infof("browser: headless CDP connecting to %s", config.Browser.CDPEndpoint)
				headlessBrowser = browser.NewHeadless(config.Browser.CDPEndpoint)
				if err := headlessBrowser.Connect(ctx); err != nil {
					log.Errorf("headless browser failed to connect: %v", err)
				}
				defer headlessBrowser.Close()
				backends = append(backends, headlessBrowser)
			}
			log.Info("browser: relay accepting extension connections on /browser")

			browserBackend := browser.NewCompositeBrowser(backends...)
			browser.RegisterBrowserTools(tools, browserBackend)

			termRelay := tterminal.NewRelay()
			tterminal.RegisterTerminalTools(tools, termRelay)

			agent.RegisterSearchTools(tools, config.Tools.BraveAPIKey)
			agent.RegisterSessionTools(tools, sessions)

			skillsDir, err := configpkg.SkillsDir()
			if err != nil {
				return err
			}
			skillPrompts := skill.RegisterSkills(tools, skillsDir)

			mediaDirectory, err := configpkg.MediaDir()
			if err != nil {
				return err
			}
			mediaStore := media.NewStore(mediaDirectory)

			runner := &agent.Runner{
				Providers:    providers,
				Sessions:     sessions,
				Config:       config,
				Tools:        tools,
				MediaStore:   mediaStore,
				WorkspaceDir: workspaceDirectory,
				SkillPrompts: skillPrompts,
			}

			// Set up cron scheduler.
			cronStore, err := cron.NewStore()
			if err != nil {
				return err
			}
			scheduler := cron.NewScheduler(cronStore, runner)
			cron.RegisterCronTools(tools, scheduler)

			server := &gateway.Server{
				Config:        config,
				Agent:         runner,
				Sessions:      sessions,
				BrowserRelay:  browserRelay,
				TerminalRelay: termRelay,
				Scheduler:     scheduler,
				MediaStore:    mediaStore,
			}

			scheduler.Broadcast = server.Broadcast
			scheduler.SetActiveRun = server.SetActiveRun
			scheduler.ClearActiveRun = server.ClearActiveRun

			// Set up Discord bot.
			if config.Discord != nil && config.Discord.Token != "" {
				discordBot := discord.New(config.Discord, runner, sessions)
				discordBot.Broadcast = server.Broadcast
				discordBot.SetActiveRun = server.SetActiveRun
				discordBot.ClearActiveRun = server.ClearActiveRun
				if err := discordBot.Start(); err != nil {
					log.Errorf("discord bot failed to start: %v", err)
				} else {
					defer discordBot.Stop()
				}
			}

			// Set up Telegram bot.
			if config.Telegram != nil && config.Telegram.Token != "" {
				telegramBot := telegram.New(config.Telegram, runner, sessions)
				telegramBot.Broadcast = server.Broadcast
				telegramBot.SetActiveRun = server.SetActiveRun
				telegramBot.ClearActiveRun = server.ClearActiveRun
				if err := telegramBot.Start(); err != nil {
					log.Errorf("telegram bot failed to start: %v", err)
				} else {
					defer telegramBot.Stop()
				}
			}

			// Set up file watcher for hot reloading.
			dataDir, err := configpkg.Dir()
			if err != nil {
				return err
			}

			// rebuildTools creates a fresh tool registry with all tools registered.
			rebuildTools := func(newConfig *configpkg.Config) (*agent.ToolRegistry, string) {
				newTools := agent.NewToolRegistry()
				agent.RegisterMemoryTools(newTools, workspaceDirectory)
				browser.RegisterBrowserTools(newTools, browserBackend)
				tterminal.RegisterTerminalTools(newTools, termRelay)
				agent.RegisterSearchTools(newTools, newConfig.Tools.BraveAPIKey)
				agent.RegisterSessionTools(newTools, sessions)
				cron.RegisterCronTools(newTools, scheduler)
				skillPrompts := skill.RegisterSkills(newTools, skillsDir)
				return newTools, skillPrompts
			}

			fileWatcher := watcher.New(dataDir)

			fileWatcher.OnConfigReload = func() {
				log.Info("hot-reloading config")
				newConfig, err := configpkg.Load()
				if err != nil {
					log.Errorf("failed to reload config: %v", err)
					return
				}
				// Preserve CLI port override.
				if cmd.IsSet("port") {
					newConfig.Gateway.Port = int(cmd.Int("port"))
				}
				newProviders := buildRegistry(newConfig)
				newTools, newSkillPrompts := rebuildTools(newConfig)
				runner.Reconfigure(newConfig, newProviders, newTools, newSkillPrompts)
				server.Config = newConfig
				log.Info("config reloaded successfully")
			}

			fileWatcher.OnSkillsReload = func() {
				log.Info("hot-reloading skills")
				config, providers, _, _, _ := runner.Snapshot()
				newTools, newSkillPrompts := rebuildTools(config)
				runner.Reconfigure(config, providers, newTools, newSkillPrompts)
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
