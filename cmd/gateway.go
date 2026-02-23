package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/api/v1api"
	"github.com/teanode/teanode/internal/channels/discord"
	"github.com/teanode/teanode/internal/channels/telegram"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/frontend"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/integrations/browsers"
	"github.com/teanode/teanode/internal/integrations/browsers/headlessbrowser"
	"github.com/teanode/teanode/internal/integrations/browsers/relaybrowser"
	"github.com/teanode/teanode/internal/integrations/terminals"
	"github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/migrations"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/sessions"
	"github.com/teanode/teanode/internal/skills"
	"github.com/teanode/teanode/internal/tools/claudecode"
	"github.com/teanode/teanode/internal/tools/codex"
	"github.com/teanode/teanode/internal/tools/datetime"
	"github.com/teanode/teanode/internal/tools/fetch"
	"github.com/teanode/teanode/internal/tools/filesystem"
	"github.com/teanode/teanode/internal/tools/github"
	"github.com/teanode/teanode/internal/tools/gitlab"
	"github.com/teanode/teanode/internal/tools/google"
	"github.com/teanode/teanode/internal/tools/homeassistant"
	"github.com/teanode/teanode/internal/tools/projects"
	"github.com/teanode/teanode/internal/tools/search"
	"github.com/teanode/teanode/internal/tools/shell"
	"github.com/teanode/teanode/internal/tools/unifiprotect"
	"github.com/teanode/teanode/internal/tools/workspace"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/version"
	"github.com/teanode/teanode/internal/watcher"
	"github.com/teanode/teanode/internal/web"
	"github.com/urfave/cli/v3"
)

// ErrRestart is returned from the gateway command when a restart was requested.
var ErrRestart = errors.New("restart requested")

func NewGatewayCommand() *cli.Command {
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
		Action: func(ctx context.Context, command *cli.Command) error {
			// Ensure base directories exist.
			if err := configs.EnsureDirectories(); err != nil {
				return err
			}
			if err := migrations.MigrateMultiUserV2(); err != nil {
				return err
			}
			if err := migrations.MigrateLegacyTrashDirectories(); err != nil {
				return err
			}
			pidGuard, err := acquireGatewayPIDGuard()
			if err != nil {
				return err
			}
			defer func() {
				if err := pidGuard.Release(); err != nil {
					log.Errorf("failed to release gateway pid file: %v", err)
				}
			}()

			// Load config.
			configuration, err := configs.Load()
			if err != nil {
				return err
			}

			// CLI flag overrides config.
			if command.IsSet("port") {
				configuration.Gateway.Port = int(command.Int("port"))
			}

			// Load security config (token + password hash).
			securityConfig, err := configs.LoadSecurity()
			if err != nil {
				return err
			}
			// Auto-generate one auth token for any user missing tokens.
			securityChanged := false
			for userId, user := range securityConfig.Users {
				if len(user.Tokens) > 0 {
					continue
				}
				generated := security.GenerateRandomString(48, security.LowerAlphaNumeric)
				user.Tokens = append(user.Tokens, configs.SecurityToken{
					ID:        security.NewULID(),
					Token:     generated,
					CreatedAt: time.Now(),
				})
				securityConfig.Users[userId] = user
				securityChanged = true
			}
			if securityChanged {
				if err := configs.SaveSecurity(securityConfig); err != nil {
					log.Errorf("failed to save security config: %v", err)
				}
			}

			// Create session store.
			sessionsDirectory, err := configs.SessionsDirectory()
			if err != nil {
				return err
			}
			sessionStore := sessions.NewStore(sessionsDirectory)

			// Build provider registry.
			buildProviderRegistry := func(configuration *configs.Config) *providers.Registry {
				registry := providers.NewRegistry(configuration.Models.DefaultProviderName())
				for _, providerConfig := range configuration.Models.ResolvedProviders() {
					registry.Register(providerConfig.Name, providers.NewProvider(providerConfig.Name, providerConfig.BaseURL, providerConfig.APIKey))
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
			browserRelay := relaybrowser.NewRelay()
			backends := []browsers.Browser{browserRelay}

			var headlessBrowser *headlessbrowser.Headless
			if configuration.Integrations.Browser != nil && configuration.Integrations.Browser.CDPEndpoint != "" {
				log.Infof("browser: headless CDP connecting to %s", configuration.Integrations.Browser.CDPEndpoint)
				headlessBrowser = headlessbrowser.NewHeadless(configuration.Integrations.Browser.CDPEndpoint)
				if err := headlessBrowser.Connect(ctx); err != nil {
					log.Errorf("headless browser failed to connect: %v", err)
				}
				headlessBrowser.StartReconnectLoop(ctx)
				defer headlessBrowser.Close()
				backends = append(backends, headlessBrowser)
			}
			log.Info("browser: relay accepting extension connections on /api/v1/browser")
			browser := browsers.NewCompositeBrowser(backends...)

			terminalRelay := terminals.NewRelay()

			skillsDirectory, err := configs.SkillsDirectory()
			if err != nil {
				return err
			}

			mediaDirectory, err := configs.MediaDirectory()
			if err != nil {
				return err
			}
			mediaStore := media.NewStore(mediaDirectory)

			// --- Agent Registry: create a runner per agent ---

			agentRegistry := agents.NewAgentRegistry()

			// gateway is declared here so buildToolsForAgent can capture it via closure.
			// It is assigned after runners are created, but tools are never called until
			// bots and the API server start, which happens after assignment.
			var gateway gw.Gateway
			var reloadSkills func()
			var scheduler *jobs.Scheduler
			var conversationStoresMutex sync.Mutex
			conversationStores := map[string]*conversations.Store{}
			resolveConversationStore := func(userId, agentId string) *conversations.Store {
				if userId == "" || agentId == "" {
					return nil
				}
				key := userId + ":" + agentId
				conversationStoresMutex.Lock()
				defer conversationStoresMutex.Unlock()
				if store, ok := conversationStores[key]; ok {
					return store
				}
				directory, err := configs.UserAgentConversationsDirectory(userId, agentId)
				if err != nil {
					return nil
				}
				if err := os.MkdirAll(directory, 0755); err != nil {
					return nil
				}
				store := conversations.NewStore(directory)
				conversationStores[key] = store
				return store
			}

			// buildToolsForAgent creates a fresh tool registry for the given agents.
			buildToolsForAgent := func(
				configuration *configs.Config,
				agentConfig configs.AgentConfig,
				workspaceDirectory string,
				conversations *conversations.Store,
				scheduler *jobs.Scheduler,
			) (*agents.ToolRegistry, string) {
				tools := agents.NewToolRegistry()
				workspace.RegisterTools(tools, workspaceDirectory)
				projects.RegisterTools(tools)
				browsers.RegisterBrowserTools(tools, browser)
				terminals.RegisterTerminalTools(tools, terminalRelay)
				search.RegisterTools(tools, configuration.Tools.BraveAPIKey)
				fetch.RegisterTools(tools)
				filesystem.RegisterTools(tools)
				shell.RegisterTools(tools)
				google.RegisterTools(tools, configuration.Tools.Google)
				github.RegisterTools(tools, configuration.Tools.GitHub)
				gitlab.RegisterTools(tools, configuration.Tools.GitLab)
				claudecode.RegisterTools(tools, configuration.Tools.ClaudeCode)
				codex.RegisterTools(tools, configuration.Tools.Codex)
				datetime.RegisterTools(tools)
				homeassistant.RegisterTools(tools, configuration.Tools.HomeAssistant)
				unifiprotect.RegisterTools(tools, configuration.Tools.UniFiProtect)
				skills.RegisterTools(tools, configuration.SkillsRegistries, reloadSkills)
				skills.SetRuntimeSecrets(configuration.Secrets)
				agents.RegisterConversationTools(tools, conversations, providers, configuration)
				if scheduler != nil {
					jobs.RegisterTools(tools, scheduler)
				}
				tools.Register(&configs.ConfigTool{Config: configuration})
				gw.RegisterTools(tools, func(action gw.LifecycleAction) { gateway.ScheduleLifecycle(action) })
				skillPrompts := skills.RegisterSkillsFiltered(tools, skillsDirectory, agentConfig.Skills)
				agents.RegisterInterAgentTools(tools, agentConfig.ID, agentRegistry, configuration)
				tools.ApplyFilter(agentConfig.Tools)
				return tools, skillPrompts
			}
			reloadSkills = func() {
				log.Info("hot-reloading skills")
				agentRegistry.ForEach(func(agentId string, runner *agents.Runner) {
					currentConfig, currentProviders, _, _, _ := runner.Snapshot()
					agentConfig := currentConfig.AgentByID(agentId)
					if agentConfig == nil {
						return
					}
					workspaceDirectory, err := configs.AgentWorkspaceDirectory(agentId)
					if err != nil {
						return
					}
					tools, skillPrompts := buildToolsForAgent(currentConfig, *agentConfig, workspaceDirectory, nil, scheduler)
					runner.Reconfigure(currentConfig, currentProviders, tools, skillPrompts)
				})
				log.Info("skills reloaded successfully")
			}

			// Set up job scheduler (needs agent registry).
			jobStore, err := jobs.NewStore()
			if err != nil {
				return err
			}
			scheduler = jobs.NewScheduler(jobStore, agentRegistry)

			// Create a runner for each configured agents.
			for _, agentConfig := range configuration.ResolveAgents() {
				if err := configs.EnsureAgentDirectories(agentConfig.ID); err != nil {
					return err
				}
				if err := configs.SeedAgentWorkspace(agentConfig.ID); err != nil {
					return err
				}

				workspaceDirectory, err := configs.AgentWorkspaceDirectory(agentConfig.ID)
				if err != nil {
					return err
				}
				tools, skillPrompts := buildToolsForAgent(configuration, agentConfig, workspaceDirectory, nil, scheduler)

				runner := &agents.Runner{
					AgentID:              agentConfig.ID,
					Providers:            providers,
					ResolveConversations: resolveConversationStore,
					ResolveUserProfile: func(userId string) (*configs.UserProfile, error) {
						return configs.LoadUserProfile(userId)
					},
					Config:             configuration,
					Tools:              tools,
					MediaStore:         mediaStore,
					WorkspaceDirectory: workspaceDirectory,
					SkillPrompts:       skillPrompts,
				}
				agentRegistry.Register(agentConfig.ID, runner)
			}

			// Set the default agent ID from config and restore persisted state.
			agentRegistry.SetDefault(configuration.ResolveDefaultAgent())
			agentRegistry.LoadState()

			// --- Gateway + API + Frontend ---

			summarizer := agents.NewSummarizer(agentRegistry, configuration)
			describer := agents.NewDescriber(agentRegistry)

			gateway = gw.New(configuration, securityConfig, agentRegistry, browserRelay, terminalRelay, scheduler, summarizer, mediaStore, sessionStore)
			api := v1api.New(gateway, reloadSkills)
			frontendComponent := frontend.New()

			agentRegistry.SetCreateAgentFunc(func(agentConfig configs.AgentConfig) error {
				if agentConfig.ID == "" {
					return errors.New("agent id is required")
				}
				if agentRegistry.Get(agentConfig.ID) != nil {
					return fmt.Errorf("agent already exists: %s", agentConfig.ID)
				}

				if err := configs.SaveAgent(agentConfig); err != nil {
					return err
				}
				if err := configs.EnsureAgentDirectories(agentConfig.ID); err != nil {
					return err
				}
				if err := configs.SeedAgentWorkspace(agentConfig.ID); err != nil {
					return err
				}

				workspaceDirectory, err := configs.AgentWorkspaceDirectory(agentConfig.ID)
				if err != nil {
					return err
				}
				currentConfiguration := gateway.Config()
				if currentConfiguration.AgentByID(agentConfig.ID) == nil {
					currentConfiguration.Agents = append(currentConfiguration.Agents, agentConfig)
				}
				currentProviders := buildProviderRegistry(currentConfiguration)
				tools, skillPrompts := buildToolsForAgent(currentConfiguration, agentConfig, workspaceDirectory, nil, scheduler)

				agentRegistry.Register(agentConfig.ID, &agents.Runner{
					AgentID:              agentConfig.ID,
					Providers:            currentProviders,
					ResolveConversations: resolveConversationStore,
					ResolveUserProfile: func(userId string) (*configs.UserProfile, error) {
						return configs.LoadUserProfile(userId)
					},
					Config:             currentConfiguration,
					Tools:              tools,
					MediaStore:         mediaStore,
					WorkspaceDirectory: workspaceDirectory,
					SkillPrompts:       skillPrompts,
				})
				describer.Notify()

				return nil
			})

			// Wire summarizer to gateway.
			summarizer.IsConversationActive = func(conversationId string) bool {
				return gateway.GetActiveRun(conversationId) != ""
			}
			summarizer.Broadcast = func(event string, payload interface{}) {
				gateway.Broadcast(gw.EventType(event), payload)
			}

			// Wire scheduler to gateway via closure.
			scheduler.Broadcast = func(event string, payload interface{}) {
				gateway.Broadcast(gw.EventType(event), payload)
			}
			scheduler.NewConversation = func(userId, agentId, model string) string {
				return gateway.NewConversation(userId, agentId, model)
			}
			scheduler.RunMessage = func(ctx context.Context, userId, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
				handle := gateway.SendMessage(ctx, gw.SendMessageParameters{
					UserContext:    &gw.UserContext{UserID: userId},
					AgentID:        agentId,
					ConversationID: conversationId,
					Message:        message,
					Model:          model,
				}, nil)
				return handle.RunID, handle.Done, func() error { return handle.Outcome().Error }
			}

			// --- Discord bot ---

			if configuration.Channels.Discord != nil && configuration.Channels.Discord.Token != "" {
				discordBot := discord.New(configuration.Channels.Discord, agentRegistry, gateway)
				if err := discordBot.Start(); err != nil {
					log.Errorf("discord bot failed to start: %v", err)
				} else {
					defer discordBot.Stop()
				}
			}

			// --- Telegram bot ---

			if configuration.Channels.Telegram != nil && configuration.Channels.Telegram.Token != "" {
				telegramBot := telegram.New(configuration.Channels.Telegram, agentRegistry, gateway)
				if err := telegramBot.Start(); err != nil {
					log.Errorf("telegram bot failed to start: %v", err)
				} else {
					defer telegramBot.Stop()
				}
			}

			// --- File watcher for hot reloading ---

			dataDirectory, err := configs.Directory()
			if err != nil {
				return err
			}

			fileWatcher := watcher.New(dataDirectory)

			// reloadAgents reconfigures agent runners from the current gateway config.
			reloadAgents := func() {
				currentConfiguration := gateway.Config()
				currentProviders := buildProviderRegistry(currentConfiguration)

				for _, agentConfig := range currentConfiguration.ResolveAgents() {
					runner := agentRegistry.Get(agentConfig.ID)
					if runner == nil {
						// New agent appeared — create it.
						if err := configs.EnsureAgentDirectories(agentConfig.ID); err != nil {
							log.Errorf("failed to create dirs for new agent %s: %v", agentConfig.ID, err)
							continue
						}
						if err := configs.SeedAgentWorkspace(agentConfig.ID); err != nil {
							log.Errorf("failed to seed workspace for new agent %s: %v", agentConfig.ID, err)
							continue
						}
						workspaceDirectory, err := configs.AgentWorkspaceDirectory(agentConfig.ID)
						if err != nil {
							continue
						}
						tools, skillPrompts := buildToolsForAgent(currentConfiguration, agentConfig, workspaceDirectory, nil, scheduler)
						runner = &agents.Runner{
							AgentID:              agentConfig.ID,
							Providers:            currentProviders,
							ResolveConversations: resolveConversationStore,
							ResolveUserProfile: func(userId string) (*configs.UserProfile, error) {
								return configs.LoadUserProfile(userId)
							},
							Config:             currentConfiguration,
							Tools:              tools,
							MediaStore:         mediaStore,
							WorkspaceDirectory: workspaceDirectory,
							SkillPrompts:       skillPrompts,
						}
						agentRegistry.Register(agentConfig.ID, runner)
						continue
					}

					// Existing agent — rebuild tools and reconfigure.
					workspaceDirectory, err := configs.AgentWorkspaceDirectory(agentConfig.ID)
					if err != nil {
						continue
					}
					tools, skillPrompts := buildToolsForAgent(currentConfiguration, agentConfig, workspaceDirectory, nil, scheduler)
					runner.Reconfigure(currentConfiguration, currentProviders, tools, skillPrompts)
				}
			}

			fileWatcher.OnConfigReload = func() {
				log.Info("hot-reloading config")
				newConfiguration, err := configs.Load()
				if err != nil {
					log.Errorf("failed to reload config: %v", err)
					return
				}
				// Preserve CLI port override.
				if command.IsSet("port") {
					newConfiguration.Gateway.Port = int(command.Int("port"))
				}

				gateway.SetConfig(newConfiguration)
				gateway.InvalidateModelsCache()
				reloadAgents()
				describer.Notify()
				log.Info("config reloaded successfully")
			}

			fileWatcher.OnAgentsReload = func() {
				log.Info("hot-reloading agents")
				newConfiguration, err := configs.Load()
				if err != nil {
					log.Errorf("failed to reload agents: %v", err)
					return
				}
				// Preserve CLI port override.
				if command.IsSet("port") {
					newConfiguration.Gateway.Port = int(command.Int("port"))
				}
				gateway.SetConfig(newConfiguration)
				reloadAgents()
				describer.Notify()
				log.Info("agents reloaded successfully")
			}

			fileWatcher.OnSkillsReload = reloadSkills

			fileWatcher.OnJobsReload = func() {
				log.Info("hot-reloading jobs")
				if err := scheduler.Reload(); err != nil {
					log.Errorf("failed to reload jobs: %v", err)
				} else {
					log.Info("jobs reloaded successfully")
				}
			}

			if err := fileWatcher.Start(); err != nil {
				log.Errorf("file watcher failed to start: %v", err)
			} else {
				defer fileWatcher.Stop()
			}

			// --- Create web server with components ---

			webServer, err := web.NewServer(&web.Settings{}, api, frontendComponent)
			if err != nil {
				return err
			}

			// Apply middleware stack (innermost first → outermost last).
			handler := web.ApplyMiddlewares(webServer,
				gateway.AuthMiddleware(),
				web.CompressionMiddleware,
				web.MakeServerNameMiddleware(version.ServerName()),
				web.LoggingMiddleware,
				web.MakeForwarderMiddleware(configuration.Gateway.ForwarderKey),
			)

			// Create HTTP listener upfront so binding errors surface immediately.
			address := gateway.ListenAddress()
			httpListener, err := net.Listen("tcp", address)
			if err != nil {
				return err
			}

			httpServer := &http.Server{
				Handler: handler,
			}

			// Start scheduler, summarizer, and describer.
			if scheduler != nil {
				if err := scheduler.Start(); err != nil {
					return err
				}
			}
			if summarizer != nil {
				summarizer.Start()
			}
			if describer != nil {
				describer.Start()
			}

			// --- Run ---

			var quit bool
			var restart bool
			var waitGroup sync.WaitGroup

			// Serve HTTP in a goroutine.
			runningHTTP := make(chan struct{}, 1)
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				defer close(runningHTTP)

				log.Infof("TeaNode gateway listening on %s", address)
				if err := httpServer.Serve(httpListener); err != nil && err != http.ErrServerClosed {
					log.Errorf("http server exited with error: %v", err)
				}
			}()

			// Wait for exit signal or server failure.
			signaling := make(chan os.Signal, 4)
			signal.Notify(signaling, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)

			for !quit {
				select {
				case sig := <-signaling:
					log.Warningf("received signal %v", sig)
					if sig == syscall.SIGQUIT {
						buffer := make([]byte, 1<<20)
						length := runtime.Stack(buffer, true)
						log.Warningf("%s", buffer[:length])
					}
					if sig == syscall.SIGHUP {
						restart = true
					}
					quit = true
				case <-runningHTTP:
					quit = true
				case action := <-gateway.LifecycleChannel():
					quit = true
					restart = action == gw.LifecycleRestart
				}
			}

			// Enforce a hard shutdown deadline.
			time.AfterFunc(30*time.Second, func() {
				log.Fatalf("graceful shutdown timed out, forcing exit")
				os.Exit(1)
			})

			// Graceful shutdown.
			log.Info("shutting down")

			if summarizer != nil {
				summarizer.Stop()
			}
			if describer != nil {
				describer.Stop()
			}
			if scheduler != nil {
				scheduler.Stop()
			}

			// Gracefully drain HTTP connections.
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				if err := httpServer.Shutdown(context.Background()); err != nil {
					log.Errorf("failed to shutdown http server: %v", err)
				}
			}()

			waitGroup.Wait()

			if restart {
				return ErrRestart
			}
			return nil
		},
	}
}
