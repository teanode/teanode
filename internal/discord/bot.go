package discord

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/command"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/logging"
	"github.com/ziyan/teanode/internal/session"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

var log = logging.Get("discord")

const maxDiscordMessageLen = 2000

// Bot manages a Discord bot that forwards messages to the agent.
type Bot struct {
	config   *config.DiscordConfig
	runner   *agent.Runner
	sessions *session.Store
	discord  *discordgo.Session
	botUserId string

	// Active runs per session key — prevents concurrent runs on same session.
	activeMutex sync.Mutex
	active   map[string]context.CancelFunc

	// Per-channel session key overrides (channelID → session key).
	sessionMutex   sync.RWMutex
	sessionKeys map[string]string

	// Per-channel model overrides (channelID → model name).
	modelMutex        sync.RWMutex
	modelOverrides map[string]string

	Broadcast      func(event string, payload interface{})
	SetActiveRun   func(sessionKey, runId string)
	ClearActiveRun func(sessionKey, runId string)
}

// New creates a new Discord bot.
func New(config *config.DiscordConfig, runner *agent.Runner, sessions *session.Store) *Bot {
	return &Bot{
		config:         config,
		runner:         runner,
		sessions:       sessions,
		active:         make(map[string]context.CancelFunc),
		sessionKeys:    make(map[string]string),
		modelOverrides: make(map[string]string),
	}
}

// Start connects the bot to Discord.
func (self *Bot) Start() error {
	discordSession, err := discordgo.New("Bot " + self.config.Token)
	if err != nil {
		return fmt.Errorf("creating discord session: %w", err)
	}

	discordSession.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent
	discordSession.AddHandler(self.onMessageCreate)

	if err := discordSession.Open(); err != nil {
		return fmt.Errorf("opening discord connection: %w", err)
	}

	self.discord = discordSession
	self.botUserId = discordSession.State.User.ID
	log.Infof("discord bot connected as %s (%s)", discordSession.State.User.Username, self.botUserId)
	return nil
}

// Stop disconnects the bot.
func (self *Bot) Stop() {
	if self.discord != nil {
		self.discord.Close()
	}
}

func (self *Bot) onMessageCreate(discordSession *discordgo.Session, event *discordgo.MessageCreate) {
	defer deferutil.Recover()

	// Ignore own messages.
	if event.Author.ID == self.botUserId {
		return
	}

	// Ignore bot messages.
	if event.Author.Bot {
		return
	}

	content := strings.TrimSpace(event.Content)
	if content == "" {
		return
	}

	// In guild channels, require a mention of the bot.
	if event.GuildID != "" {
		mentioned := false
		for _, user := range event.Mentions {
			if user.ID == self.botUserId {
				mentioned = true
				break
			}
		}
		if !mentioned {
			return
		}
		// Strip the mention from the message.
		content = strings.TrimSpace(strings.ReplaceAll(content, "<@"+self.botUserId+">", ""))
		content = strings.TrimSpace(strings.ReplaceAll(content, "<@!"+self.botUserId+">", ""))
		if content == "" {
			return
		}
	}

	// Check allowed users.
	if !self.isUserAllowed(event.Author.ID) {
		return
	}

	// Check for slash commands.
	if name, arguments, ok := command.Parse(content); ok {
		self.handleCommand(discordSession, event, name, arguments)
		return
	}

	sessionKey := self.getSessionKey(event.ChannelID)

	// Check if there's already an active run for this session.
	self.activeMutex.Lock()
	if _, busy := self.active[sessionKey]; busy {
		self.activeMutex.Unlock()
		discordSession.ChannelMessageSend(event.ChannelID, "I'm still working on a previous request. Please wait.")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	self.active[sessionKey] = cancel
	self.activeMutex.Unlock()

	go self.handleMessage(ctx, cancel, sessionKey, event.ChannelID, content)
}

func (self *Bot) handleMessage(ctx context.Context, cancel context.CancelFunc, sessionKey, channelID, message string) {
	defer deferutil.Recover()
	defer func() {
		self.activeMutex.Lock()
		delete(self.active, sessionKey)
		self.activeMutex.Unlock()
		cancel()
	}()

	runId := uuid.New().String()

	if self.SetActiveRun != nil {
		self.SetActiveRun(sessionKey, runId)
	}
	defer func() {
		if self.ClearActiveRun != nil {
			self.ClearActiveRun(sessionKey, runId)
		}
	}()

	// Broadcast user message to WebUI.
	if self.Broadcast != nil {
		self.Broadcast("chat", map[string]interface{}{
			"state":      "user_message",
			"runId":      runId,
			"sessionKey": sessionKey,
			"text":       message,
		})
		self.Broadcast("sessions", nil)
	}

	// Send typing indicator.
	self.discord.ChannelTyping(channelID)

	var callbacks *agent.RunCallbacks
	if self.Broadcast != nil {
		broadcast := self.Broadcast
		callbacks = &agent.RunCallbacks{
			OnTextDelta: func(text string) {
				broadcast("chat", map[string]interface{}{
					"state":      "delta",
					"runId":      runId,
					"sessionKey": sessionKey,
					"text":       text,
				})
			},
			OnToolCall: func(toolName string, arguments string) {
				broadcast("chat", map[string]interface{}{
					"state":      "tool_call",
					"runId":      runId,
					"sessionKey": sessionKey,
					"toolName":   toolName,
					"arguments":  arguments,
				})
				// Re-send typing indicator after tool calls.
				self.discord.ChannelTyping(channelID)
			},
			OnToolResult: func(toolName string, result string) {
				broadcast("chat", map[string]interface{}{
					"state":      "tool_result",
					"runId":      runId,
					"sessionKey": sessionKey,
					"toolName":   toolName,
					"result":     result,
				})
			},
			OnTitleUpdate: func(title string) {
				broadcast("chat", map[string]interface{}{
					"state":      "title",
					"sessionKey": sessionKey,
					"title":      title,
				})
				broadcast("sessions", nil)
			},
		}
	}

	result, err := self.runner.Run(ctx, agent.RunParams{
		SessionKey: sessionKey,
		Message:    message,
		Model:      self.getModel(channelID),
	}, callbacks)

	if self.Broadcast != nil {
		if err != nil {
			self.Broadcast("chat", map[string]interface{}{
				"state":      "error",
				"runId":      runId,
				"sessionKey": sessionKey,
				"error":      err.Error(),
			})
		} else {
			payload := map[string]interface{}{
				"state":      "final",
				"runId":      runId,
				"sessionKey": sessionKey,
				"text":       result.Response,
				"model":      result.Model,
				"stopReason": result.StopReason,
			}
			if result.Usage != nil {
				payload["usage"] = result.Usage
			}
			self.Broadcast("chat", payload)
		}
	}

	// Send response to Discord.
	if err != nil {
		log.Errorf("discord agent run error (session %s): %v", sessionKey, err)
		self.discord.ChannelMessageSend(channelID, "Sorry, an error occurred while processing your request.")
		return
	}

	self.sendChunked(channelID, result.Response)
}

func (self *Bot) getSessionKey(channelID string) string {
	self.sessionMutex.RLock()
	defer self.sessionMutex.RUnlock()
	if key, ok := self.sessionKeys[channelID]; ok {
		return key
	}
	return fmt.Sprintf("discord-%s", channelID)
}

func (self *Bot) getModel(channelID string) string {
	self.modelMutex.RLock()
	defer self.modelMutex.RUnlock()
	return self.modelOverrides[channelID]
}

func (self *Bot) handleCommand(discordSession *discordgo.Session, messageEvent *discordgo.MessageCreate, name, arguments string) {
	channelID := messageEvent.ChannelID
	var reply string

	switch name {
	case "new":
		newKey := fmt.Sprintf("discord-%s-%s", channelID, uuid.New().String()[:8])
		self.sessionMutex.Lock()
		self.sessionKeys[channelID] = newKey
		self.sessionMutex.Unlock()
		reply = fmt.Sprintf("New session started. (`%s`)", newKey)

	case "reset":
		sessionKey := self.getSessionKey(channelID)
		if err := self.sessions.Delete(sessionKey); err != nil {
			reply = fmt.Sprintf("Error clearing session: %v", err)
		} else {
			reply = "Session history cleared."
		}

	case "stop":
		sessionKey := self.getSessionKey(channelID)
		self.activeMutex.Lock()
		cancel, found := self.active[sessionKey]
		self.activeMutex.Unlock()
		if found {
			cancel()
			reply = "Run cancelled."
		} else {
			reply = "No active run to cancel."
		}

	case "model":
		if arguments == "" {
			model := self.getModel(channelID)
			if model == "" {
				model = self.runner.Config.Models.Default
			}
			reply = fmt.Sprintf("Current model: `%s`", model)
		} else {
			self.modelMutex.Lock()
			self.modelOverrides[channelID] = arguments
			self.modelMutex.Unlock()
			reply = fmt.Sprintf("Model set to `%s`.", arguments)
		}

	case "status":
		sessionKey := self.getSessionKey(channelID)
		model := self.getModel(channelID)
		if model == "" {
			model = self.runner.Config.Models.Default
		}
		self.activeMutex.Lock()
		_, running := self.active[sessionKey]
		self.activeMutex.Unlock()
		status := "idle"
		if running {
			status = "running"
		}
		reply = fmt.Sprintf("Session: `%s`\nModel: `%s`\nProvider: `%s`\nStatus: %s", sessionKey, model, self.runner.Config.Models.Provider, status)

	case "help":
		reply = command.HelpText()
	}

	if reply != "" {
		discordSession.ChannelMessageSend(channelID, reply)
	}
}

func (self *Bot) sendChunked(channelID, text string) {
	if text == "" {
		return
	}
	for len(text) > 0 {
		chunk := text
		if len(chunk) > maxDiscordMessageLen {
			// Try to split at a newline.
			cut := strings.LastIndex(chunk[:maxDiscordMessageLen], "\n")
			if cut < maxDiscordMessageLen/2 {
				cut = maxDiscordMessageLen
			}
			chunk = text[:cut]
		}
		if _, err := self.discord.ChannelMessageSend(channelID, chunk); err != nil {
			log.Errorf("discord send error: %v", err)
			return
		}
		text = text[len(chunk):]
	}
}

func (self *Bot) isUserAllowed(userID string) bool {
	if len(self.config.AllowedUsers) == 0 {
		return true
	}
	for _, id := range self.config.AllowedUsers {
		if id == userID {
			return true
		}
	}
	return false
}
