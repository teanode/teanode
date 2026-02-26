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

	"github.com/teanode/teanode/internal/api/v1api"
	"github.com/teanode/teanode/internal/channels/discord"
	"github.com/teanode/teanode/internal/channels/telegram"
	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/frontend"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/integrations/browsers"
	"github.com/teanode/teanode/internal/integrations/browsers/headlessbrowser"
	"github.com/teanode/teanode/internal/integrations/browsers/relaybrowser"
	"github.com/teanode/teanode/internal/integrations/terminals"
	"github.com/teanode/teanode/internal/lifecycle"
	toolbrowser "github.com/teanode/teanode/internal/tools/browser"
	toolterminal "github.com/teanode/teanode/internal/tools/terminal"
	"github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/skills"
	"github.com/teanode/teanode/internal/store"
	storedb "github.com/teanode/teanode/internal/store/dbstore"
	storefs "github.com/teanode/teanode/internal/store/fsstore"
	summarizerpackage "github.com/teanode/teanode/internal/summarizer"
	toolregistry "github.com/teanode/teanode/internal/tools"
	toolagent "github.com/teanode/teanode/internal/tools/agent"
	"github.com/teanode/teanode/internal/tools/claudecode"
	"github.com/teanode/teanode/internal/tools/codex"
	toolconfigs "github.com/teanode/teanode/internal/tools/configs"
	toolconversation "github.com/teanode/teanode/internal/tools/conversation"
	"github.com/teanode/teanode/internal/tools/datetime"
	"github.com/teanode/teanode/internal/tools/fetch"
	"github.com/teanode/teanode/internal/tools/filesystem"
	toolgateway "github.com/teanode/teanode/internal/tools/gateway"
	"github.com/teanode/teanode/internal/tools/github"
	"github.com/teanode/teanode/internal/tools/gitlab"
	"github.com/teanode/teanode/internal/tools/google"
	"github.com/teanode/teanode/internal/tools/homeassistant"
	tooljobs "github.com/teanode/teanode/internal/tools/jobs"
	"github.com/teanode/teanode/internal/tools/projects"
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
			ctx = browsers.ContextWithBrowser(ctx, browser)

			terminalRelay := terminals.NewRelay()
			ctx = terminals.ContextWithTerminal(ctx, terminalRelay)

			// --- Coordinator + Default Conversation Manager ---

			// gateway is declared here so buildToolRegistry can capture it via closure.
			// It is assigned after the coordinator is created, but tools are never called until
			// bots and the API server start, which happens after assignment.
			var gateway gw.Gateway
			var scheduler *jobs.Scheduler

			// buildToolRegistryForCoordinator is called by the coordinator each time a new
			// per-conversation runner is created. It reads the current configuration from
			// store so that tool options are always up to date.
			buildToolRegistryForCoordinator := func(ctx context.Context, agent models.Agent) (*toolregistry.ToolRegistry, string) {
				return buildToolRegistry(buildToolRegistryOptions{
					Context: ctx,
					Agent:   agent,
				})
			}

			coordinator := coordinators.New(providers, buildToolRegistryForCoordinator)
			defaults := runners.NewDefaultConversationManager(ctx)

			// Set up job scheduler.
			scheduler = jobs.NewScheduler(ctx)
			ctx = jobs.ContextWithScheduler(ctx, scheduler)

			// Ensure at least one agent exists in the store.
			if transactionError := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
				listedAgents, listError := transaction.ListAgents(ctx, nil)
				if listError != nil {
					return listError
				}
				if len(listedAgents) == 0 {
					mainAgentName := "Tea"
					_, createError := transaction.CreateAgent(ctx, &models.Agent{
						ID:   "main",
						Name: &mainAgentName,
					}, nil, nil)
					if createError != nil {
						return createError
					}
				}
				return nil
			}); transactionError != nil {
				return transactionError
			}

			// --- Gateway + API + Frontend ---

			summarizer := summarizerpackage.New(ctx, providers)
			lifecycleManager := lifecycle.New()
			ctx = lifecycle.ContextWithLifecycle(ctx, lifecycleManager)

			gateway = gw.New(ctx, configuration, coordinator, defaults, browserRelay, terminalRelay, summarizer)
			api := v1api.New(gateway)
			frontendComponent := frontend.New()

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
				discordBot := discord.New(configuration.Channels.Discord.GetToken(), ctx, gateway)
				if err := discordBot.Start(); err != nil {
					log.Errorf("discord bot failed to start: %v", err)
				} else {
					defer discordBot.Stop()
				}
			}

			// --- Telegram bot ---

			if configuration.Channels.Telegram != nil && configuration.Channels.Telegram.GetToken() != "" {
				telegramBot := telegram.New(ctx, configuration.Channels.Telegram.GetToken(), gateway)
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
			address := listenAddress(configuration)
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
			runningHttp := make(chan struct{}, 1)
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				defer close(runningHttp)

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
				case <-runningHttp:
					quit = true
				case action := <-lifecycleManager.Channel():
					quit = true
					restart = action == lifecycle.Restart
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
	Context context.Context
	Agent   models.Agent
}

func buildToolRegistry(options buildToolRegistryOptions) (*toolregistry.ToolRegistry, string) {
	registry := toolregistry.NewToolRegistry()
	agent := options.Agent

	workspace.RegisterTools(registry)
	registry.Register(projects.NewProjectsTool())
	registry.Register(projects.NewProjectWorkspaceTool())
	toolbrowser.RegisterTools(registry)
	toolterminal.RegisterTools(registry)
	search.RegisterTools(registry)
	fetch.RegisterTools(registry)
	filesystem.RegisterTools(registry)
	shell.RegisterTools(registry)
	google.RegisterTools(registry)
	github.RegisterTools(registry)
	gitlab.RegisterTools(registry)
	claudecode.RegisterTools(registry)
	codex.RegisterTools(registry)
	datetime.RegisterTools(registry)
	homeassistant.RegisterTools(registry)
	unifiprotect.RegisterTools(registry)
	toolconversation.RegisterTools(registry)
	tooljobs.RegisterTools(registry)
	registry.Register(toolconfigs.NewConfigTool())
	toolgateway.RegisterTools(registry)
	skillPrompts := skills.RegisterSkillsFiltered(options.Context, registry, agent.GetSkills())
	toolagent.RegisterTools(registry)
	registry.ApplyFilter(agent.GetTools())
	return registry, skillPrompts
}

// listenAddress returns the host:port string derived from the gateway configuration.
func listenAddress(configuration *models.Configuration) string {
	host := "127.0.0.1"
	bind := ""
	port := 8833
	if configuration != nil && configuration.Gateway != nil {
		bind = configuration.Gateway.GetBind()
		if configuration.Gateway.GetPort() > 0 {
			port = configuration.Gateway.GetPort()
		}
	}
	if bind == "lan" {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}

