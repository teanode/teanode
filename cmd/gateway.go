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
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/projects"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/skills"
	datastore "github.com/teanode/teanode/internal/store"
	storedb "github.com/teanode/teanode/internal/store/db"
	storefs "github.com/teanode/teanode/internal/store/fs"
	"github.com/teanode/teanode/internal/tools/claudecode"
	"github.com/teanode/teanode/internal/tools/codex"
	"github.com/teanode/teanode/internal/tools/datetime"
	"github.com/teanode/teanode/internal/tools/fetch"
	"github.com/teanode/teanode/internal/tools/filesystem"
	"github.com/teanode/teanode/internal/tools/github"
	"github.com/teanode/teanode/internal/tools/gitlab"
	"github.com/teanode/teanode/internal/tools/google"
	"github.com/teanode/teanode/internal/tools/homeassistant"
	tooljobs "github.com/teanode/teanode/internal/tools/jobs"
	"github.com/teanode/teanode/internal/tools/search"
	"github.com/teanode/teanode/internal/tools/shell"
	"github.com/teanode/teanode/internal/tools/unifiprotect"
	"github.com/teanode/teanode/internal/tools/workspace"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/version"
	"github.com/teanode/teanode/internal/web"
	"github.com/urfave/cli/v3"
)

// ErrRestart is returned from the gateway command when a restart was requested.
var ErrRestart = errors.New("restart requested")

func valueOrEmptyString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

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
			&cli.StringFlag{
				Name:    "store",
				Usage:   "store backend: filesystem or postgres",
				Value:   string(datastore.BackendFilesystem),
				Sources: cli.EnvVars("TEANODE_STORE"),
			},
			&cli.StringFlag{
				Name:    "store-postgres-host",
				Usage:   "postgres host",
				Value:   "127.0.0.1",
				Sources: cli.EnvVars("TEANODE_STORE_POSTGRES_HOST"),
			},
			&cli.UintFlag{
				Name:    "store-postgres-port",
				Usage:   "postgres port",
				Value:   5432,
				Sources: cli.EnvVars("TEANODE_STORE_POSTGRES_PORT"),
			},
			&cli.StringFlag{
				Name:    "store-postgres-user",
				Usage:   "postgres user",
				Value:   "teanode",
				Sources: cli.EnvVars("TEANODE_STORE_POSTGRES_USER"),
			},
			&cli.StringFlag{
				Name:    "store-postgres-password",
				Usage:   "postgres password",
				Value:   "teanode",
				Sources: cli.EnvVars("TEANODE_STORE_POSTGRES_PASSWORD"),
			},
			&cli.StringFlag{
				Name:    "store-postgres-database",
				Usage:   "postgres database",
				Value:   "teanode",
				Sources: cli.EnvVars("TEANODE_STORE_POSTGRES_DATABASE"),
			},
			&cli.StringFlag{
				Name:    "store-postgres-sslmode",
				Usage:   "postgres sslmode",
				Value:   "disable",
				Sources: cli.EnvVars("TEANODE_STORE_POSTGRES_SSLMODE"),
			},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			// Ensure base directories exist.
			if err := configs.EnsureDirectories(); err != nil {
				return err
			}
			storeBackend := datastore.BackendType(command.String("store"))
			var postgresSettings *storedb.Settings
			if storeBackend == datastore.BackendPostgres {
				postgresSettings = &storedb.Settings{
					Host:     command.String("store-postgres-host"),
					Port:     uint16(command.Uint("store-postgres-port")),
					User:     command.String("store-postgres-user"),
					Password: command.String("store-postgres-password"),
					Database: command.String("store-postgres-database"),
					SSLMode:  command.String("store-postgres-sslmode"),
				}
			}
			var openedStore datastore.Store
			var err error
			switch storeBackend {
			case "", datastore.BackendFilesystem:
				openedStore, err = storefs.Open(storefs.Options{DataDirectory: configs.Directory()})
			case datastore.BackendPostgres:
				if postgresSettings == nil {
					return fmt.Errorf("postgres settings are required")
				}
				openedStore, err = storedb.Open(*postgresSettings)
			default:
				return fmt.Errorf("unsupported store backend: %s", storeBackend)
			}
			if err != nil {
				return err
			}
			if migrateError := openedStore.Migrate(); migrateError != nil {
				return migrateError
			}
			ctx = datastore.ContextWithStore(ctx, openedStore)
			defer func() {
				if err := openedStore.Close(); err != nil {
					log.Errorf("failed to close store: %v", err)
				}
			}()
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
			if err := openedStore.Transaction(func(transaction datastore.Transaction) error {
				users, err := transaction.ListUsers(nil)
				if err != nil {
					return err
				}
				for _, user := range users {
					tokens, listError := transaction.ListTokens(user.ID, nil)
					if listError != nil {
						return listError
					}
					if len(tokens) > 0 {
						continue
					}
					generated := security.GenerateRandomString(48, security.LowerAlphaNumeric)
					if _, createError := transaction.CreateToken(&models.Token{
						ID:     security.NewULID(),
						UserID: ptrto.Value(user.ID),
						Token:  ptrto.Value(generated),
					}, nil); createError != nil {
						return createError
					}
				}
				return nil
			}); err != nil {
				log.Errorf("failed to bootstrap auth tokens: %v", err)
			}

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

			skillsDirectory := configs.SkillsDirectory()

			// --- Agent Registry: create a runner per agent ---

			agentRegistry := agents.NewAgentRegistry(ctx)

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
				conversationStore := conversations.NewStore(ctx, userId, agentId)
				conversationStores[key] = conversationStore
				return conversationStore
			}
			resolveUser := func(userId string) (*models.User, error) {
				var profile *models.User
				if err := openedStore.Transaction(func(transaction datastore.Transaction) error {
					user, err := transaction.GetUser(userId, nil)
					if err != nil {
						return err
					}
					profile = user
					return nil
				}); err != nil {
					return nil, err
				}
				return profile, nil
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
				tools.Register(projects.NewProjectsTool())
				tools.Register(projects.NewProjectWorkspaceTool())
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
					tooljobs.RegisterTools(tools)
				}
				tools.Register(&configs.ConfigTool{Config: configuration})
				gw.RegisterTools(tools, func(action gw.LifecycleAction) { gateway.ScheduleLifecycle(action) })
				skillPrompts := skills.RegisterSkillsFiltered(ctx, tools, skillsDirectory, agentConfig.Skills)
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
					workspaceDirectory := configs.AgentWorkspaceDirectory(agentId)
					tools, skillPrompts := buildToolsForAgent(currentConfig, *agentConfig, workspaceDirectory, nil, scheduler)
					runner.Reconfigure(currentConfig, currentProviders, tools, skillPrompts)
				})
				log.Info("skills reloaded successfully")
			}

			// Set up job scheduler (needs agent registry).
			scheduler = jobs.NewScheduler(ctx)
			ctx = jobs.ContextWithScheduler(ctx, scheduler)

			// Create a runner for each configured agents.
			for _, agentConfig := range configuration.Agents() {
				if err := configs.EnsureAgentDirectories(agentConfig.ID); err != nil {
					return err
				}
				if err := configs.SeedAgentWorkspace(agentConfig.ID); err != nil {
					return err
				}

				workspaceDirectory := configs.AgentWorkspaceDirectory(agentConfig.ID)
				tools, skillPrompts := buildToolsForAgent(configuration, agentConfig, workspaceDirectory, nil, scheduler)

				runner := &agents.Runner{
					AgentID:              agentConfig.ID,
					Providers:            providers,
					ResolveConversations: resolveConversationStore,
					ResolveUser:          resolveUser,
					Config:               configuration,
					Tools:                tools,
					WorkspaceDirectory:   workspaceDirectory,
					SkillPrompts:         skillPrompts,
				}
				agentRegistry.Register(agentConfig.ID, runner)
			}

			// Restore persisted per-user state.
			agentRegistry.LoadState()

			// --- Gateway + API + Frontend ---

			summarizer := agents.NewSummarizer(ctx, agentRegistry, configuration)

			gateway = gw.New(ctx, configuration, securityConfig, agentRegistry, browserRelay, terminalRelay, summarizer)
			api := v1api.New(gateway, reloadSkills)
			frontendComponent := frontend.New()

			agentRegistry.SetCreateAgentFunc(func(agentConfig configs.AgentConfig) error {
				if agentConfig.ID == "" {
					return errors.New("agent id is required")
				}
				if agentRegistry.GetRunner(agentConfig.ID) != nil {
					return fmt.Errorf("agent already exists: %s", agentConfig.ID)
				}

				if err := configs.SaveAgentConfig(agentConfig.ID, &agentConfig); err != nil {
					return err
				}
				if err := configs.EnsureAgentDirectories(agentConfig.ID); err != nil {
					return err
				}
				if err := configs.SeedAgentWorkspace(agentConfig.ID); err != nil {
					return err
				}

				workspaceDirectory := configs.AgentWorkspaceDirectory(agentConfig.ID)
				currentConfiguration := gateway.Config()
				if currentConfiguration.AgentByID(agentConfig.ID) == nil {
					currentConfiguration.AgentConfigs = append(currentConfiguration.AgentConfigs, agentConfig)
				}
				currentProviders := buildProviderRegistry(currentConfiguration)
				tools, skillPrompts := buildToolsForAgent(currentConfiguration, agentConfig, workspaceDirectory, nil, scheduler)

				agentRegistry.Register(agentConfig.ID, &agents.Runner{
					AgentID:              agentConfig.ID,
					Providers:            currentProviders,
					ResolveConversations: resolveConversationStore,
					ResolveUser:          resolveUser,
					Config:               currentConfiguration,
					Tools:                tools,
					WorkspaceDirectory:   workspaceDirectory,
					SkillPrompts:         skillPrompts,
				})
				summarizer.Notify()

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
			scheduler.RunMessage = func(ctx context.Context, userId, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
				var user *models.User
				transactionError := openedStore.Transaction(func(transaction datastore.Transaction) error {
					existingUser, getError := transaction.GetUser(userId, nil)
					if getError != nil {
						return getError
					}
					user = existingUser
					return nil
				})
				if transactionError != nil || user == nil {
					doneChannel := make(chan struct{})
					close(doneChannel)
					return "", doneChannel, func() error {
						if transactionError != nil {
							return transactionError
						}
						return fmt.Errorf("user not found: %s", userId)
					}
				}

				runContext := gw.ContextWithUserAndSession(ctx, user, nil)
				handle := gateway.SendMessage(runContext, gw.SendMessageParameters{
					AgentID:        agentId,
					ConversationID: conversationId,
					Message:        message,
					Model:          model,
				}, nil)
				return handle.RunID, handle.Done, func() error { return handle.Outcome().Error }
			}

			// --- Discord bot ---

			if configuration.Channels.Discord != nil && configuration.Channels.Discord.Token != "" {
				discordBot := discord.New(configuration.Channels.Discord, ctx, agentRegistry, gateway)
				if err := discordBot.Start(); err != nil {
					log.Errorf("discord bot failed to start: %v", err)
				} else {
					defer discordBot.Stop()
				}
			}

			// --- Telegram bot ---

			if configuration.Channels.Telegram != nil && configuration.Channels.Telegram.Token != "" {
				telegramBot := telegram.New(ctx, configuration.Channels.Telegram, agentRegistry, gateway)
				if err := telegramBot.Start(); err != nil {
					log.Errorf("telegram bot failed to start: %v", err)
				} else {
					defer telegramBot.Stop()
				}
			}

			// --- Create web server with components ---

			webServer, err := web.NewServer(&web.Settings{}, api, frontendComponent)
			if err != nil {
				return err
			}

			// Apply middleware stack (innermost first → outermost last).
			storeMiddleware := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					requestContext := datastore.ContextWithStore(request.Context(), openedStore)
					requestContext = jobs.ContextWithScheduler(requestContext, scheduler)
					next.ServeHTTP(writer, request.WithContext(requestContext))
				})
			}
			handler := web.ApplyMiddlewares(webServer,
				storeMiddleware,
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

			// Start scheduler and summarizer.
			if scheduler != nil {
				if err := scheduler.Start(); err != nil {
					return err
				}
			}
			if summarizer != nil {
				summarizer.Start()
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
