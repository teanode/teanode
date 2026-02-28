package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/teanode/teanode/internal/api/v1api"
	"github.com/teanode/teanode/internal/channels/discord"
	"github.com/teanode/teanode/internal/channels/telegram"
	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/frontend"
	"github.com/teanode/teanode/internal/integrations/browsers"
	"github.com/teanode/teanode/internal/integrations/browsers/headlessbrowser"
	"github.com/teanode/teanode/internal/integrations/browsers/relaybrowser"
	"github.com/teanode/teanode/internal/integrations/terminals"
	"github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/lifecycle"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/dbstore"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/summarizers"
	"github.com/teanode/teanode/internal/util/debugutil"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/sessiontracker"
	"github.com/teanode/teanode/internal/version"
	"github.com/teanode/teanode/internal/web"
	"github.com/urfave/cli/v3"

	// Blank imports ensure each tool package's init() registers its tools.
	_ "github.com/teanode/teanode/internal/tools/agent"
	_ "github.com/teanode/teanode/internal/tools/browser"
	_ "github.com/teanode/teanode/internal/tools/claudecode"
	_ "github.com/teanode/teanode/internal/tools/codex"
	_ "github.com/teanode/teanode/internal/tools/configs"
	_ "github.com/teanode/teanode/internal/tools/conversation"
	_ "github.com/teanode/teanode/internal/tools/conversations"
	_ "github.com/teanode/teanode/internal/tools/datetime"
	_ "github.com/teanode/teanode/internal/tools/fetch"
	_ "github.com/teanode/teanode/internal/tools/filesystem"
	_ "github.com/teanode/teanode/internal/tools/gateway"
	_ "github.com/teanode/teanode/internal/tools/github"
	_ "github.com/teanode/teanode/internal/tools/gitlab"
	_ "github.com/teanode/teanode/internal/tools/google"
	_ "github.com/teanode/teanode/internal/tools/homeassistant"
	_ "github.com/teanode/teanode/internal/tools/jobs"
	_ "github.com/teanode/teanode/internal/tools/projects"
	_ "github.com/teanode/teanode/internal/tools/search"
	_ "github.com/teanode/teanode/internal/tools/shell"
	_ "github.com/teanode/teanode/internal/tools/terminal"
	_ "github.com/teanode/teanode/internal/tools/unifiprotect"
	_ "github.com/teanode/teanode/internal/tools/workspace"
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
			&cli.StringFlag{
				Name:    "debug-endpoint",
				Usage:   "address for the debug/pprof server (e.g. 127.0.0.1:6060)",
				Sources: cli.EnvVars("TEANODE_DEBUG_ENDPOINT"),
			},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			// --- Optional debug/pprof server ---
			if debugEndpoint := command.String("debug-endpoint"); debugEndpoint != "" {
				shutdownDebugServer, err := debugutil.RunDebugServer(ctx, debugEndpoint)
				if err != nil {
					return err
				}
				defer shutdownDebugServer()
			}

			pidGuard, err := acquirePidGuard(ctx)
			if err != nil {
				return err
			}
			defer func() {
				if err := pidGuard.Release(); err != nil {
					log.Errorf("failed to release gateway pid file: %v", err)
				}
			}()

			storeBackend := store.BackendType(command.String("store"))
			var postgresSettings *dbstore.Settings
			if storeBackend == store.BackendPostgres {
				postgresSettings = &dbstore.Settings{
					Host:     command.String("store-postgres-host"),
					Port:     uint16(command.Uint("store-postgres-port")),
					User:     command.String("store-postgres-user"),
					Password: command.String("store-postgres-password"),
					Database: command.String("store-postgres-database"),
					SSLMode:  command.String("store-postgres-sslmode"),
				}
			}
			var openedStore store.Store
			switch storeBackend {
			case "", store.BackendFilesystem:
				openedStore, err = fsstore.Open(fsstore.Options{DataDirectory: DataDirectoryFromContext(ctx)})
			case store.BackendPostgres:
				if postgresSettings == nil {
					return fmt.Errorf("postgres settings are required")
				}
				openedStore, err = dbstore.Open(*postgresSettings)
			default:
				return fmt.Errorf("unsupported store backend: %s", storeBackend)
			}
			if err != nil {
				return err
			}
			defer func() {
				if err := openedStore.Close(); err != nil {
					log.Errorf("failed to close store: %v", err)
				}
			}()

			if migrateError := openedStore.Migrate(ctx); migrateError != nil {
				return migrateError
			}

			ctx = store.ContextWithStore(ctx, openedStore)

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
			if configuration.Channels == nil {
				configuration.Channels = &models.ChannelsConfiguration{}
			}
			if configuration.Gateway.Port == nil {
				configuration.Gateway.Port = ptrto.Value(8833)
			}
			if configuration.Gateway.Bind == nil {
				configuration.Gateway.Bind = ptrto.Value(models.BindModeLoopback)
			}
			// CLI flag overrides config.
			if command.IsSet("port") {
				configuration.Gateway.Port = ptrto.Value(int(command.Int("port")))
			}

			// Build provider registry.
			providerRegistry := providers.NewProviderRegistry(configuration.Models)

			// --- Shared resources ---

			// Browser: always create relay for extension connections, optionally
			// add headless CDP backend. Both can be active simultaneously.
			browserRelay := relaybrowser.NewRelay()

			var browserConfiguration *models.BrowserConfiguration
			if configuration.Integrations != nil {
				browserConfiguration = configuration.Integrations.Browser
			}
			headlessBrowser := headlessbrowser.NewHeadless(browserConfiguration)
			if err := headlessBrowser.Connect(ctx); err != nil {
				log.Debugf("headless browser: initial connect: %v", err)
			}
			headlessBrowser.StartReconnectLoop(ctx)
			defer headlessBrowser.Close()

			log.Info("browser: relay accepting extension connections on /api/v1/browser")
			browser := browsers.NewCompositeBrowser(browserRelay, headlessBrowser)
			ctx = browsers.ContextWithBrowser(ctx, browser)

			terminalRelay := terminals.NewRelay()
			ctx = terminals.ContextWithTerminal(ctx, terminalRelay)

			// --- Coordinator + PubSub + SessionTracker ---

			events := pubsub.New()
			sessions := sessiontracker.New()

			summarizer := summarizers.New(ctx, providerRegistry)

			// Set up job scheduler.
			scheduler := jobs.NewScheduler(ctx)
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

			// --- Coordinator + API + Frontend ---

			lifecycleManager := lifecycle.New()
			ctx = lifecycle.ContextWithLifecycle(ctx, lifecycleManager)

			coordinator := coordinators.New(ctx, configuration, providerRegistry, summarizer, events)
			ctx = coordinators.ContextWithCoordinator(ctx, coordinator)

			api := v1api.New(coordinator, events, sessions, browserRelay, terminalRelay)
			frontendComponent := frontend.New()

			// Wire summarizer to coordinator.
			summarizer.IsConversationActive = func(conversationId string) bool {
				return coordinator.GetActiveConversationRunner(conversationId) != nil
			}
			summarizer.Broadcast = func(event string, payload interface{}) {
				events.Broadcast(pubsub.EventType(event), payload)
			}

			// Wire scheduler to coordinator.
			scheduler.Broadcast = func(event string, payload interface{}) {
				events.Broadcast(pubsub.EventType(event), payload)
			}
			scheduler.RunMessage = func(ctx context.Context, userId, agentId, conversationId, message, providerModelName string) (string, <-chan struct{}, func() error) {
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
				handle, sendError := coordinator.Run(runContext, coordinators.RunParameters{
					AgentID:           agentId,
					ConversationID:    conversationId,
					Message:           message,
					ProviderModelName: providerModelName,
				}, nil)
				if sendError != nil {
					doneChannel := make(chan struct{})
					close(doneChannel)
					return "", doneChannel, func() error { return sendError }
				}
				return handle.RunID, handle.Done(), func() error {
					_, waitError := handle.Wait()
					return waitError
				}
			}

			// --- Discord bot ---

			discordToken := ""
			if configuration.Channels.Discord != nil {
				discordToken = configuration.Channels.Discord.GetToken()
			}
			if discordToken == "" {
				discordToken = os.Getenv("DISCORD_BOT_TOKEN")
			}
			if discordToken != "" {
				discordBot := discord.New(discordToken, ctx, coordinator, events, sessions)
				if err := discordBot.Start(); err != nil {
					log.Errorf("discord bot failed to start: %v", err)
				} else {
					defer discordBot.Stop()
				}
			}

			// --- Telegram bot ---

			telegramToken := ""
			if configuration.Channels.Telegram != nil {
				telegramToken = configuration.Channels.Telegram.GetToken()
			}
			if telegramToken == "" {
				telegramToken = os.Getenv("TELEGRAM_BOT_TOKEN")
			}
			if telegramToken != "" {
				telegramBot := telegram.New(ctx, telegramToken, coordinator, events, sessions)
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
				Handler:     handler,
				ReadTimeout: 30 * time.Second,
				IdleTimeout: 120 * time.Second,
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
				defer deferutil.Recover()
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
				case signal := <-signaling:
					log.Warningf("received signal %v", signal)
					if signal == syscall.SIGQUIT {
						log.Warningf("dumping all goroutine stacks:\n%s", debugutil.GetAllStacks())
					}
					if signal == syscall.SIGHUP {
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
				log.Fatalf("graceful shutdown timed out:\n%s", debugutil.GetAllStacks())
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
				defer deferutil.Recover()
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

// listenAddress returns the host:port string derived from the gateway configuration.
func listenAddress(configuration *models.Configuration) string {
	host := "127.0.0.1"
	var bind models.BindMode
	port := 8833
	if configuration != nil && configuration.Gateway != nil {
		bind = configuration.Gateway.GetBind()
		if configuration.Gateway.GetPort() > 0 {
			port = configuration.Gateway.GetPort()
		}
	}
	if bind == models.BindModeLAN {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}
