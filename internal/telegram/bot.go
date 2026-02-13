package telegram

import (
	"context"
	"fmt"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/command"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/logging"
	"github.com/ziyan/teanode/internal/session"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

var log = logging.Get("telegram")

const maxTelegramMessageLen = 4096

// Bot manages a Telegram bot that forwards messages to the agent.
type Bot struct {
	config   *config.TelegramConfig
	runner   *agent.Runner
	sessions *session.Store
	api      *tgbotapi.BotAPI
	stopChannel   chan struct{}

	// Active runs per session key — prevents concurrent runs on same session.
	activeMutex sync.Mutex
	active   map[string]context.CancelFunc

	// Per-chat session key overrides (chatID string → session key).
	sessionMutex   sync.RWMutex
	sessionKeys map[string]string

	// Per-chat model overrides (chatID string → model name).
	modelMutex        sync.RWMutex
	modelOverrides map[string]string

	Broadcast      func(event string, payload interface{})
	SetActiveRun   func(sessionKey, runId string)
	ClearActiveRun func(sessionKey, runId string)
}

// New creates a new Telegram bot.
func New(config *config.TelegramConfig, runner *agent.Runner, sessions *session.Store) *Bot {
	return &Bot{
		config:         config,
		runner:         runner,
		sessions:       sessions,
		active:         make(map[string]context.CancelFunc),
		sessionKeys:    make(map[string]string),
		modelOverrides: make(map[string]string),
		stopChannel:         make(chan struct{}),
	}
}

// Start connects the bot to Telegram and begins polling for updates.
func (self *Bot) Start() error {
	api, err := tgbotapi.NewBotAPI(self.config.Token)
	if err != nil {
		return fmt.Errorf("creating telegram bot: %w", err)
	}
	self.api = api

	log.Infof("telegram bot connected as @%s", api.Self.UserName)

	// Register commands in Telegram's command menu.
	commands := tgbotapi.NewSetMyCommands(
		tgbotapi.BotCommand{Command: "new", Description: "Start a new session"},
		tgbotapi.BotCommand{Command: "reset", Description: "Clear current session history"},
		tgbotapi.BotCommand{Command: "stop", Description: "Cancel the current run"},
		tgbotapi.BotCommand{Command: "model", Description: "Show or set the model"},
		tgbotapi.BotCommand{Command: "status", Description: "Show bot status"},
		tgbotapi.BotCommand{Command: "help", Description: "Show available commands"},
		tgbotapi.BotCommand{Command: "ask", Description: "Ask the AI (required in groups)"},
	)
	if _, err := self.api.Request(commands); err != nil {
		log.Errorf("failed to set telegram commands: %v", err)
	}

	go self.poll()
	return nil
}

// Stop halts the bot.
func (self *Bot) Stop() {
	close(self.stopChannel)
	if self.api != nil {
		self.api.StopReceivingUpdates()
	}
}

func (self *Bot) poll() {
	defer deferutil.Recover()

	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 30

	updates := self.api.GetUpdatesChan(updateConfig)
	for {
		select {
		case <-self.stopChannel:
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if update.Message == nil {
				continue
			}
			self.onMessage(update.Message)
		}
	}
}

func (self *Bot) onMessage(message *tgbotapi.Message) {
	defer deferutil.Recover()

	text := strings.TrimSpace(message.Text)
	if text == "" {
		return
	}

	// Check allowed users.
	if !self.isUserAllowed(message.From.ID) {
		return
	}

	chatIdStr := fmt.Sprintf("%d", message.Chat.ID)

	// Handle /start specially — always respond with greeting.
	if text == "/start" || strings.HasPrefix(text, "/start@") {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Hello! Send me a message and I'll respond using AI.\n\n"+command.HelpText())
		self.api.Send(msg)
		return
	}

	// Handle /ask — strip prefix and pass to agent.
	if strings.HasPrefix(text, "/ask") {
		// Strip /ask or /ask@botname prefix.
		rest := text[4:]
		if index := strings.Index(rest, " "); strings.HasPrefix(rest, "@") && index > 0 {
			rest = rest[index:]
		}
		text = strings.TrimSpace(rest)
		if text == "" {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Usage: /ask <message>")
			msg.ReplyToMessageID = message.MessageID
			self.api.Send(msg)
			return
		}
		// Fall through to agent handling below.
	} else if name, arguments, ok := command.Parse(text); ok {
		self.handleCommand(message, chatIdStr, name, arguments)
		return
	} else {
		// In group chats, only respond to replies to the bot.
		if message.Chat.Type == "group" || message.Chat.Type == "supergroup" {
			isReplyToBot := message.ReplyToMessage != nil && message.ReplyToMessage.From != nil && message.ReplyToMessage.From.ID == self.api.Self.ID
			if !isReplyToBot {
				return
			}
		}
	}

	sessionKey := self.getSessionKey(chatIdStr)

	// Check if there's already an active run for this session.
	self.activeMutex.Lock()
	if _, busy := self.active[sessionKey]; busy {
		self.activeMutex.Unlock()
		msg := tgbotapi.NewMessage(message.Chat.ID, "I'm still working on a previous request. Please wait.")
		msg.ReplyToMessageID = message.MessageID
		self.api.Send(msg)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	self.active[sessionKey] = cancel
	self.activeMutex.Unlock()

	go self.handleMessage(ctx, cancel, sessionKey, message.Chat.ID, message.MessageID, text)
}

func (self *Bot) handleMessage(ctx context.Context, cancel context.CancelFunc, sessionKey string, chatID int64, replyTo int, message string) {
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

	// Send typing action.
	action := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	self.api.Send(action)

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
				// Re-send typing action after tool calls.
				action := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
				self.api.Send(action)
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

	chatIdStr := fmt.Sprintf("%d", chatID)
	result, err := self.runner.Run(ctx, agent.RunParams{
		SessionKey: sessionKey,
		Message:    message,
		Model:      self.getModel(chatIdStr),
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

	// Send response to Telegram.
	if err != nil {
		log.Errorf("telegram agent run error (session %s): %v", sessionKey, err)
		msg := tgbotapi.NewMessage(chatID, "Sorry, an error occurred while processing your request.")
		msg.ReplyToMessageID = replyTo
		self.api.Send(msg)
		return
	}

	self.sendChunked(chatID, replyTo, result.Response)
}

func (self *Bot) getSessionKey(chatIdStr string) string {
	self.sessionMutex.RLock()
	defer self.sessionMutex.RUnlock()
	if key, ok := self.sessionKeys[chatIdStr]; ok {
		return key
	}
	return fmt.Sprintf("telegram-%s", chatIdStr)
}

func (self *Bot) getModel(chatIdStr string) string {
	self.modelMutex.RLock()
	defer self.modelMutex.RUnlock()
	return self.modelOverrides[chatIdStr]
}

func (self *Bot) handleCommand(message *tgbotapi.Message, chatIdStr, name, arguments string) {
	var reply string

	switch name {
	case "new":
		newKey := fmt.Sprintf("telegram-%s-%s", chatIdStr, uuid.New().String()[:8])
		self.sessionMutex.Lock()
		self.sessionKeys[chatIdStr] = newKey
		self.sessionMutex.Unlock()
		reply = fmt.Sprintf("New session started. (%s)", newKey)

	case "reset":
		sessionKey := self.getSessionKey(chatIdStr)
		if err := self.sessions.Delete(sessionKey); err != nil {
			reply = fmt.Sprintf("Error clearing session: %v", err)
		} else {
			reply = "Session history cleared."
		}

	case "stop":
		sessionKey := self.getSessionKey(chatIdStr)
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
			model := self.getModel(chatIdStr)
			if model == "" {
				model = self.runner.Config.Models.Default
			}
			reply = fmt.Sprintf("Current model: %s", model)
		} else {
			self.modelMutex.Lock()
			self.modelOverrides[chatIdStr] = arguments
			self.modelMutex.Unlock()
			reply = fmt.Sprintf("Model set to %s.", arguments)
		}

	case "status":
		sessionKey := self.getSessionKey(chatIdStr)
		model := self.getModel(chatIdStr)
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
		reply = fmt.Sprintf("Session: %s\nModel: %s\nProvider: %s\nStatus: %s", sessionKey, model, self.runner.Config.Models.Provider, status)

	case "help":
		reply = command.HelpText()
	}

	if reply != "" {
		msg := tgbotapi.NewMessage(message.Chat.ID, reply)
		msg.ReplyToMessageID = message.MessageID
		self.api.Send(msg)
	}
}

func (self *Bot) sendChunked(chatID int64, replyTo int, text string) {
	if text == "" {
		return
	}
	first := true
	for len(text) > 0 {
		chunk := text
		if len(chunk) > maxTelegramMessageLen {
			// Try to split at a newline.
			cut := strings.LastIndex(chunk[:maxTelegramMessageLen], "\n")
			if cut < maxTelegramMessageLen/2 {
				cut = maxTelegramMessageLen
			}
			chunk = text[:cut]
		}
		msg := tgbotapi.NewMessage(chatID, chunk)
		msg.ParseMode = "Markdown"
		if first {
			msg.ReplyToMessageID = replyTo
			first = false
		}
		if _, err := self.api.Send(msg); err != nil {
			// Retry without Markdown parse mode in case of formatting errors.
			msg.ParseMode = ""
			if _, err := self.api.Send(msg); err != nil {
				log.Errorf("telegram send error: %v", err)
				return
			}
		}
		text = text[len(chunk):]
	}
}

func (self *Bot) isUserAllowed(userID int64) bool {
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
