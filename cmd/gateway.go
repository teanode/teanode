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
	"github.com/teanode/teanode/internal/frontend"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/integrations/browsers"
	"github.com/teanode/teanode/internal/integrations/browsers/headlessbrowser"
	"github.com/teanode/teanode/internal/integrations/browsers/relaybrowser"
	"github.com/teanode/teanode/internal/integrations/terminals"
	"github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/skills"
	"github.com/teanode/teanode/internal/store"
	summarizerpackage "github.com/teanode/teanode/internal/summarizer"
	storedb "github.com/teanode/teanode/internal/store/dbstore"
	storefs "github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/tools/claudecode"
	"github.com/teanode/teanode/internal/tools/codex"
	toolconfigs "github.com/teanode/teanode/internal/tools/configs"
	toolgateway "github.com/teanode/teanode/internal/tools/gateway"
	"github.com/teanode/teanode/internal/tools/datetime"
	"github.com/teanode/teanode/internal/tools/fetch"
	"github.com/teanode/teanode/internal/tools/filesystem"
	"github.com/teanode/teanode/internal/tools/github"
	"github.com/teanode/teanode/internal/tools/gitlab"
	"github.com/teanode/teanode/internal/tools/google"
	"github.com/teanode/teanode/internal/tools/homeassistant"
	toolconversation "github.com/teanode/teanode/internal/tools/conversation"
	tooljobs "github.com/teanode/teanode/internal/tools/jobs"
	"github.com/teanode/teanode/internal/tools/projects"
	toolregistry "github.com/teanode/teanode/internal/tools"
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
				Value:   string(store.BackendFilesystem),
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
			dataDirectory, dataDirectoryError := DataDirectoryFromContext(ctx)
			if dataDirectoryError != nil {
				return dataDirectoryError
			}

			storeBackend := store.BackendType(command.String("store"))
			var postgresSettings *storedb.Settings
			if storeBackend == store.BackendPostgres {
				postgresSettings = &storedb.Settings{
					Host:     command.String("store-postgres-host"),
					Port:     uint16(command.Uint("store-postgres-port")),
					User:     command.String("store-postgres-user"),
					Password: command.String("store-postgres-password"),
					Database: command.String("store-postgres-database"),
					SSLMode:  command.String("store-postgres-sslmode"),
				}
			}
			var openedStore store.Store
			var err error
			switch storeBackend {
			case "", store.BackendFilesystem:
				openedStore, err = storefs.Open(storefs.Options{DataDirectory: dataDirectory})
			case store.BackendPostgres:
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
			if migrateError := openedStore.Migrate(ctx); migrateError != nil {
				return migrateError
			}
			ctx = store.ContextWithStore(ctx, openedStore)
			defer func() {
				if err := openedStore.Close(); err != nil {
					log.Errorf("failed to close store: %v", err)
				}
			}()
			pidGuard, err := acquirePidGuard(ctx)
			if err != nil {
				return err
			}
			defer func() {
				if err := pidGuard.Release(); err != nil {
					log.Errorf("failed to release gateway pid file: %v", err)
				}
			}()

			configuration := &models.Configuration{}
			if transactionError := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
				loadedConfiguration, getError := transaction.GetConfiguration(ctx, nil)
				if getError != nil {
					return getError
				}
				configuration = loadedConfiguration
				return nil
			}); transactionError != nil {
				return transactionError
			}
			if configuration.Gateway == nil {
				configuration.Gateway = &models.GatewayConfiguration{}
			}
			if configuration.Models == nil {
				configuration.Models = &models.ModelsConfiguration{}
			}
			if configuration.Tools == nil {
				configuration.Tools = &models.ToolsConfiguration{}
			}
			if configuration.Integrations == nil {
				configuration.Integrations = &models.IntegrationsConfiguration{}
			}
			if configuration.Channels == nil {
				configuration.Channels = &models.ChannelsConfiguration{}
			}
			if configuration.Gateway.Port == nil {
				configuration.Gateway.Port = ptrto.Value(8833)
			}
			if configuration.Gateway.Bind == nil {
				configuration.Gateway.Bind = ptrto.Value("loopback")
			}
			if configuration.Models.GetDefault() == "" {
				configuration.Models.Default = ptrto.Value("openai:gpt-5.2")
			}
			if configuration.Models.Providers == nil || len(*configuration.Models.Providers) == 0 {
				defaultProviderName := "openai"
				defaultProviderBaseURL := "https://api.openai.com/v1"
				defaultProviderKey := os.Getenv("OPENAI_API_KEY")
				defaultProviders := []*models.ProviderConfiguration{
					{
						Name:    &defaultProviderName,
						BaseURL: &defaultProviderBaseURL,
						APIKey:  &defaultProviderKey,
					},
				}
				configuration.Models.Providers = &defaultProviders
			}

			// CLI flag overrides config.
			if command.IsSet("port") {
				configuration.Gateway.Port = ptrto.Value(int(command.Int("port")))
			}

			// Auto-generate one auth token for any user missing tokens.
			if err := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
				users, err := transaction.ListUsers(ctx, nil)
				if err != nil {
					return err
				}
				for _, user := range users {
					tokens, listError := transaction.ListTokens(ctx, user.ID, nil)
					if listError != nil {
						return listError
					}
					if len(tokens) > 0 {
						continue
					}
					generated := security.GenerateRandomString(48, security.LowerAlphaNumeric)
					if _, createError := transaction.CreateToken(ctx, &models.Token{
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
			buildProviderRegistry := func(configuration *models.Configuration) *providers.Registry {
				defaultProviderName := "openai"
				if configuration.Models != nil && configuration.Models.GetDefault() != "" {
					resolvedProviderName, _ := providers.ParseQualifiedModel(configuration.Models.GetDefault(), defaultProviderName)
					defaultProviderName = resolvedProviderName
				}
				registry := providers.NewRegistry(defaultProviderName)
				if configuration.Models == nil || configuration.Models.Providers == nil {
					return registry
				}
				for _, providerConfiguration := range *configuration.Models.Providers {
					providerName := providerConfiguration.GetName()
					if providerName == "" {
						continue
					}
					registry.Register(providerName, providers.NewProvider(
						providerName,
						providerConfiguration.GetBaseURL(),
						providerConfiguration.GetAPIKey(),
					))
				}
				return registry
			}
			providers := buildProviderRegistry(configuration)

			// Validate that at least one provider has an API key.
			hasKey := false
			if configuration.Models != nil && configuration.Models.Providers != nil {
				for _, providerConfiguration := range *configuration.Models.Providers {
					if providerConfiguration.GetAPIKey() != "" {
						hasKey = true
						break
					}
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
			if configuration.Integrations.Browser != nil && configuration.Integrations.Browser.GetCDPEndpoint() != "" {
				cdpEndpoint := configuration.Integrations.Browser.GetCDPEndpoint()
				log.Infof("browser: headless CDP connecting to %s", cdpEndpoint)
				headlessBrowser = headlessbrowser.NewHeadless(cdpEndpoint)
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

			// --- Agent Registry: create a runner per agent ---

			agentRegistry := agents.NewAgentRegistry(ctx)

			// gateway is declared here so buildToolsForAgent can capture it via closure.
			// It is assigned after runners are created, but tools are never called until
			// bots and the API server start, which happens after assignment.
			var gateway gw.Gateway
			var scheduler *jobs.Scheduler
			resolveUser := func(userId string) (*models.User, error) {
				var profile *models.User
				if err := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
					user, err := transaction.GetUser(ctx, userId, nil)
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
				configuration *models.Configuration,
				agent models.Agent,
				scheduler *jobs.Scheduler,
			) (*toolregistry.ToolRegistry, string) {
				return buildToolRegistry(buildToolRegistryOptions{
					Context:           ctx,
					Configuration:     configuration,
					Agent:             agent,
					Scheduler:         scheduler,
					Providers:         providers,
					Browser:           browser,
					TerminalRelay:     terminalRelay,
					AgentRegistry:     agentRegistry,
					ScheduleLifecycle: func(action gw.LifecycleAction) { gateway.ScheduleLifecycle(action) },
				})
			}
			
			// Set up job scheduler (needs agent registry).
			scheduler = jobs.NewScheduler(ctx)
			ctx = jobs.ContextWithScheduler(ctx, scheduler)

			// Create a runner for each stored agent.
			agentModels := make([]*models.Agent, 0)
			if transactionError := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
				listedAgents, listError := transaction.ListAgents(ctx, nil)
				if listError != nil {
					return listError
				}
				agentModels = listedAgents
				if len(agentModels) == 0 {
					mainAgentName := "Tea"
					mainAgent, createError := transaction.CreateAgent(ctx, &models.Agent{
						ID:   "main",
						Name: &mainAgentName,
					}, nil, nil)
					if createError != nil {
						return createError
					}
					agentModels = append(agentModels, mainAgent)
				}
				return nil
			}); transactionError != nil {
				return transactionError
			}
			for _, agentModel := range agentModels {
				tools, skillPrompts := buildToolsForAgent(configuration, *agentModel, scheduler)

				runner := &agents.Runner{
					AgentID:            agentModel.ID,
					Providers:          providers,
					ResolveUser:        resolveUser,
					Config:             configuration,
					Tools:              tools,
					WorkspaceDirectory: "",
					SkillPrompts:       skillPrompts,
				}
				agentRegistry.Register(agentModel.ID, runner)
			}

			// Restore persisted per-user state.
			agentRegistry.LoadState()

			// --- Gateway + API + Frontend ---

			summarizer := summarizerpackage.New(ctx, providers)

			gateway = gw.New(ctx, configuration, agentRegistry, browserRelay, terminalRelay, summarizer)
			api := v1api.New(gateway)
			frontendComponent := frontend.New()

			agentRegistry.SetCreateAgentFunc(func(agentId string, name string) error {
				if agentId == "" {
					return errors.New("agent id is required")
				}
				if agentRegistry.GetRunner(agentId) != nil {
					return fmt.Errorf("agent already exists: %s", agentId)
				}

				var createdAgent *models.Agent
				if transactionError := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
					agentModel, createError := transaction.CreateAgent(ctx, &models.Agent{
						ID:   agentId,
						Name: ptrto.TrimmedString(name),
					}, nil, nil)
					if createError != nil {
						return createError
					}
					createdAgent = agentModel
					return nil
				}); transactionError != nil {
					return transactionError
				}

				currentProviders := buildProviderRegistry(configuration)
				tools, skillPrompts := buildToolsForAgent(configuration, *createdAgent, scheduler)

				agentRegistry.Register(agentId, &agents.Runner{
					AgentID:            agentId,
					Providers:          currentProviders,
					ResolveUser:        resolveUser,
					Config:             configuration,
					Tools:              tools,
					WorkspaceDirectory: "",
					SkillPrompts:       skillPrompts,
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
				transactionError := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
					existingUser, getError := transaction.GetUser(ctx, userId, nil)
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

				runContext := models.ContextWithUserSessionToken(ctx, user, nil, nil)
				handle := gateway.SendMessage(runContext, gw.SendMessageParameters{
					AgentID:        agentId,
					ConversationID: conversationId,
					Message:        message,
					Model:          model,
				}, nil)
				return handle.RunID, handle.Done, func() error { return handle.Outcome().Error }
			}

			// --- Discord bot ---

			if configuration.Channels.Discord != nil && configuration.Channels.Discord.GetToken() != "" {
				discordBot := discord.New(configuration.Channels.Discord.GetToken(), ctx, agentRegistry, gateway)
				if err := discordBot.Start(); err != nil {
					log.Errorf("discord bot failed to start: %v", err)
				} else {
					defer discordBot.Stop()
				}
			}

			// --- Telegram bot ---

			if configuration.Channels.Telegram != nil && configuration.Channels.Telegram.GetToken() != "" {
				telegramBot := telegram.New(ctx, configuration.Channels.Telegram.GetToken(), agentRegistry, gateway)
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
			handler := web.ApplyMiddlewares(webServer,
				web.MakeAuthenticationMiddleware(),
				web.CompressionMiddleware,
				web.MakeServerNameMiddleware(version.ServerName()),
				web.LoggingMiddleware,
				web.MakeForwarderMiddleware(""),
			)

			// Create HTTP listener upfront so binding errors surface immediately.
			address := gateway.ListenAddress()
			httpListener, err := net.Listen("tcp", address)
			if err != nil {
				return err
			}

			httpServer := &http.Server{
				Handler: handler,
				BaseContext: func(net.Listener) context.Context {
					return ctx
				},
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

type buildToolRegistryOptions struct {
	Context           context.Context
	Configuration     *models.Configuration
	Agent             models.Agent
	Scheduler         *jobs.Scheduler
	Providers         *providers.Registry
	Browser           browsers.Browser
	TerminalRelay     *terminals.Relay
	AgentRegistry     *agents.AgentRegistry
	ScheduleLifecycle func(gw.LifecycleAction)
}

func buildToolRegistry(options buildToolRegistryOptions) (*toolregistry.ToolRegistry, string) {
	registry := toolregistry.NewToolRegistry()
	configuration := options.Configuration
	agent := options.Agent

	workspace.RegisterTools(registry, agent.ID)
	registry.Register(projects.NewProjectsTool())
	registry.Register(projects.NewProjectWorkspaceTool())
	browsers.RegisterBrowserTools(registry, options.Browser)
	terminals.RegisterTerminalTools(registry, options.TerminalRelay)
	search.RegisterTools(registry, configuration.Tools.GetBraveAPIKey())
	fetch.RegisterTools(registry)
	filesystem.RegisterTools(registry)
	shell.RegisterTools(registry)
	google.RegisterTools(registry, buildGoogleToolOptions(configuration.Tools.Google))
	github.RegisterTools(registry, buildGitHubToolOptions(configuration.Tools.GitHub))
	gitlab.RegisterTools(registry, buildGitLabToolOptions(configuration.Tools.GitLab))
	claudecode.RegisterTools(registry, buildClaudeCodeToolOptions(configuration.Tools.ClaudeCode))
	codex.RegisterTools(registry, buildCodexToolOptions(configuration.Tools.Codex))
	datetime.RegisterTools(registry)
	homeassistant.RegisterTools(registry, buildHomeAssistantToolOptions(configuration.Tools.HomeAssistant))
	unifiprotect.RegisterTools(registry, buildUniFiProtectToolOptions(configuration.Tools.UniFiProtect))
	toolconversation.RegisterTools(registry)
	if options.Scheduler != nil {
		tooljobs.RegisterTools(registry)
	}
	registry.Register(toolconfigs.NewConfigTool())
	toolgateway.RegisterTools(registry, options.ScheduleLifecycle)
	skillPrompts := skills.RegisterSkillsFiltered(options.Context, registry, agent.GetSkills())
	agents.RegisterInterAgentTools(registry, agent.ID, options.AgentRegistry)
	registry.ApplyFilter(agent.GetTools())
	return registry, skillPrompts
}

func buildGoogleToolOptions(configuration *models.GoogleConfiguration) *google.RegistrationOptions {
	if configuration == nil {
		return nil
	}
	return &google.RegistrationOptions{
		BinaryPath: configuration.GetBinaryPath(),
		Account:    configuration.GetAccount(),
		Services:   configuration.GetServices(),
	}
}

func buildGitHubToolOptions(configuration *models.GitHubConfiguration) *github.RegistrationOptions {
	if configuration == nil {
		return nil
	}
	return &github.RegistrationOptions{
		BinaryPath: configuration.GetBinaryPath(),
		Services:   configuration.GetServices(),
	}
}

func buildGitLabToolOptions(configuration *models.GitLabConfiguration) *gitlab.RegistrationOptions {
	if configuration == nil {
		return nil
	}
	return &gitlab.RegistrationOptions{
		BinaryPath: configuration.GetBinaryPath(),
		Services:   configuration.GetServices(),
	}
}

func buildClaudeCodeToolOptions(configuration *models.ClaudeCodeConfiguration) *claudecode.RegistrationOptions {
	if configuration == nil {
		return nil
	}
	return &claudecode.RegistrationOptions{
		BinaryPath:            configuration.GetBinaryPath(),
		AllowedTools:          configuration.GetAllowedTools(),
		Model:                 configuration.GetModel(),
		MaxTurnTimeoutSeconds: configuration.GetMaxTurnTimeoutSeconds(),
	}
}

func buildCodexToolOptions(configuration *models.CodexConfiguration) *codex.RegistrationOptions {
	if configuration == nil {
		return nil
	}
	return &codex.RegistrationOptions{
		BinaryPath:            configuration.GetBinaryPath(),
		AllowedTools:          configuration.GetAllowedTools(),
		ExtraArgs:             configuration.GetExtraArgs(),
		Model:                 configuration.GetModel(),
		MaxTurnTimeoutSeconds: configuration.GetMaxTurnTimeoutSeconds(),
	}
}

func buildHomeAssistantToolOptions(configuration *models.HomeAssistantConfiguration) *homeassistant.RegistrationOptions {
	if configuration == nil {
		return nil
	}
	return &homeassistant.RegistrationOptions{
		BaseURL:         configuration.GetBaseURL(),
		Token:           configuration.GetToken(),
		ReadOnly:        configuration.GetReadOnly(),
		AllowedDomains:  configuration.GetAllowedDomains(),
		BlockedDomains:  configuration.GetBlockedDomains(),
		AllowedEntities: configuration.GetAllowedEntities(),
		TimeoutSeconds:  configuration.GetTimeoutSeconds(),
	}
}

func buildUniFiProtectToolOptions(configuration *models.UniFiProtectConfiguration) *unifiprotect.RegistrationOptions {
	if configuration == nil {
		return nil
	}
	return &unifiprotect.RegistrationOptions{
		BaseURL:               configuration.GetBaseURL(),
		APIKey:                configuration.GetAPIKey(),
		Username:              configuration.GetUsername(),
		Password:              configuration.GetPassword(),
		VerifyTLS:             configuration.GetVerifyTLS(),
		ReadOnly:              configuration.GetReadOnly(),
		AllowedCameras:        configuration.GetAllowedCameras(),
		AllowDangerousActions: configuration.GetAllowDangerousActions(),
		TimeoutSeconds:        configuration.GetTimeoutSeconds(),
	}
}
