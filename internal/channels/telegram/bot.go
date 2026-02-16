package telegram

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/slashcommands"
	"github.com/teanode/teanode/internal/util/ulid"
)

const maxTelegramMessageLen = 4096

// Bot manages a Telegram bot that forwards messages to the agents.
type Bot struct {
	config        *configs.TelegramConfig
	runner        *agents.Runner
	conversations *conversations.Store
	api           *tgbotapi.BotAPI
	stopChannel   chan struct{}

	// Active runs per conversation id — prevents concurrent runs on same conversation.
	activeMutex sync.Mutex
	active      map[string]context.CancelFunc

	// Per-chat conversation id overrides (chatId string → conversation id).
	conversationMutex sync.RWMutex
	conversationIds   map[string]string

	// Per-chat model overrides (chatId string → model name).
	modelMutex     sync.RWMutex
	modelOverrides map[string]string

	Broadcast      func(event string, payload interface{})
	SetActiveRun   func(conversationId, runId string)
	ClearActiveRun func(conversationId, runId string)
}

// New creates a new Telegram bot. It resolves the runner from the agent registry
// using the config's AgentID (defaults to the configured default agent).
func New(telegramConfig *configs.TelegramConfig, agentRegistry *agents.AgentRegistry) (*Bot, error) {
	agentId := telegramConfig.AgentID
	if agentId == "" {
		agentId = agentRegistry.DefaultID()
	}
	runner := agentRegistry.Get(agentId)
	if runner == nil {
		runner = agentRegistry.Default()
	}
	if runner == nil {
		return nil, fmt.Errorf("no agent runner available for telegram (agent %q)", agentId)
	}
	return &Bot{
		config:          telegramConfig,
		runner:          runner,
		conversations:   runner.Conversations,
		active:          make(map[string]context.CancelFunc),
		conversationIds: make(map[string]string),
		modelOverrides:  make(map[string]string),
		stopChannel:     make(chan struct{}),
	}, nil
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
		tgbotapi.BotCommand{Command: "new", Description: "Start a new conversation"},
		tgbotapi.BotCommand{Command: "reset", Description: "Clear current conversation history"},
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
		msg := tgbotapi.NewMessage(message.Chat.ID, "Hello! Send me a message and I'll respond using AI.\n\n"+slashcommands.HelpText())
		self.api.Send(msg)
		return
	}

	// Handle /ask — strip prefix and pass to agents.
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
	} else if name, arguments, ok := slashcommands.Parse(text); ok {
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

	conversationId := self.getConversationId(chatIdStr)

	// Check if there's already an active run for this conversation.
	self.activeMutex.Lock()
	if _, busy := self.active[conversationId]; busy {
		self.activeMutex.Unlock()
		msg := tgbotapi.NewMessage(message.Chat.ID, "I'm still working on a previous request. Please wait.")
		msg.ReplyToMessageID = message.MessageID
		self.api.Send(msg)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	self.active[conversationId] = cancel
	self.activeMutex.Unlock()

	go self.handleMessage(ctx, cancel, conversationId, message.Chat.ID, message.MessageID, text)
}

func (self *Bot) handleMessage(ctx context.Context, cancel context.CancelFunc, conversationId string, chatId int64, replyTo int, message string) {
	defer deferutil.Recover()
	defer func() {
		self.activeMutex.Lock()
		delete(self.active, conversationId)
		self.activeMutex.Unlock()
		cancel()
	}()

	runId := ulid.GenerateString()

	if self.SetActiveRun != nil {
		self.SetActiveRun(conversationId, runId)
	}
	defer func() {
		if self.ClearActiveRun != nil {
			self.ClearActiveRun(conversationId, runId)
		}
	}()

	// Broadcast user message to WebUI.
	if self.Broadcast != nil {
		self.Broadcast("conversation", map[string]interface{}{
			"state":          "user_message",
			"runId":          runId,
			"conversationId": conversationId,
			"text":           message,
		})
		self.Broadcast("conversations", nil)
	}

	// Send typing action.
	action := tgbotapi.NewChatAction(chatId, tgbotapi.ChatTyping)
	self.api.Send(action)

	var pendingMedia []*media.MediaContent
	var pendingMediaMutex sync.Mutex

	var callbacks *agents.RunCallbacks
	if self.Broadcast != nil {
		broadcast := self.Broadcast
		callbacks = &agents.RunCallbacks{
			OnTextDelta: func(text string) {
				broadcast("chat", map[string]interface{}{
					"state":          "delta",
					"runId":          runId,
					"conversationId": conversationId,
					"text":           text,
				})
			},
			OnToolCall: func(toolName string, arguments string) {
				broadcast("chat", map[string]interface{}{
					"state":          "tool_call",
					"runId":          runId,
					"conversationId": conversationId,
					"toolName":       toolName,
					"arguments":      arguments,
				})
				// Re-send typing action after tool calls.
				action := tgbotapi.NewChatAction(chatId, tgbotapi.ChatTyping)
				self.api.Send(action)
			},
			OnToolResult: func(toolName string, result string) {
				broadcast("chat", map[string]interface{}{
					"state":          "tool_result",
					"runId":          runId,
					"conversationId": conversationId,
					"toolName":       toolName,
					"result":         result,
				})
				// Collect media for sending as photos.
				if detected := media.DetectMedia(result); detected != nil && detected.Base64 != "" && media.IsImageFormat(detected.Format) {
					pendingMediaMutex.Lock()
					pendingMedia = append(pendingMedia, detected)
					pendingMediaMutex.Unlock()
				}
			},
		}
	}

	chatIdStr := fmt.Sprintf("%d", chatId)
	result, err := self.runner.Run(ctx, agents.RunParams{
		ConversationID: conversationId,
		Message:        message,
		Model:          self.getModel(chatIdStr),
	}, callbacks)

	if self.Broadcast != nil {
		if err != nil {
			self.Broadcast("conversation", map[string]interface{}{
				"state":          "error",
				"runId":          runId,
				"conversationId": conversationId,
				"error":          err.Error(),
			})
		} else {
			payload := map[string]interface{}{
				"state":          "final",
				"runId":          runId,
				"conversationId": conversationId,
				"text":           result.Response,
				"model":          result.Model,
				"stopReason":     result.StopReason,
			}
			if result.Usage != nil {
				payload["usage"] = result.Usage
			}
			self.Broadcast("conversation", payload)
		}
	}

	// Send response to Telegram.
	if err != nil {
		log.Errorf("telegram agent run error (conversation %s): %v", conversationId, err)
		msg := tgbotapi.NewMessage(chatId, "Sorry, an error occurred while processing your request.")
		msg.ReplyToMessageID = replyTo
		self.api.Send(msg)
		return
	}

	// Send collected media as photo attachments.
	for _, mediaContent := range pendingMedia {
		rawData, decodeError := base64.StdEncoding.DecodeString(mediaContent.Base64)
		if decodeError != nil {
			continue
		}
		filename := fmt.Sprintf("screenshot.%s", mediaContent.Format)
		photo := tgbotapi.NewPhoto(chatId, tgbotapi.FileBytes{Name: filename, Bytes: rawData})
		photo.ReplyToMessageID = replyTo
		if _, sendError := self.api.Send(photo); sendError != nil {
			log.Errorf("telegram photo send error: %v", sendError)
		}
	}

	self.sendChunked(chatId, replyTo, result.Response)
}

func (self *Bot) getConversationId(chatIdStr string) string {
	self.conversationMutex.RLock()
	defer self.conversationMutex.RUnlock()
	if id, ok := self.conversationIds[chatIdStr]; ok {
		return id
	}
	return ulid.GenerateString()
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
		conversationId := ulid.GenerateString()
		self.conversationMutex.Lock()
		self.conversationIds[chatIdStr] = conversationId
		self.conversationMutex.Unlock()
		reply = fmt.Sprintf("New conversation started. (%s)", conversationId)

	case "reset":
		conversationId := self.getConversationId(chatIdStr)
		if err := self.conversations.Delete(conversationId); err != nil {
			reply = fmt.Sprintf("Error clearing conversation: %v", err)
		} else {
			reply = "Conversation history cleared."
		}

	case "stop":
		conversationId := self.getConversationId(chatIdStr)
		self.activeMutex.Lock()
		cancel, found := self.active[conversationId]
		self.activeMutex.Unlock()
		if found {
			cancel()
			self.runner.CancelConversation(conversationId)
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
		conversationId := self.getConversationId(chatIdStr)
		model := self.getModel(chatIdStr)
		if model == "" {
			model = self.runner.Config.Models.Default
		}
		self.activeMutex.Lock()
		_, running := self.active[conversationId]
		self.activeMutex.Unlock()
		status := "idle"
		if running {
			status = "running"
		}
		reply = fmt.Sprintf("Conversation: %s\nModel: %s\nProvider: %s\nStatus: %s", conversationId, model, self.runner.Config.Models.DefaultProviderName(), status)

	case "help":
		reply = slashcommands.HelpText()
	}

	if reply != "" {
		msg := tgbotapi.NewMessage(message.Chat.ID, reply)
		msg.ReplyToMessageID = message.MessageID
		self.api.Send(msg)
	}
}

func (self *Bot) sendChunked(chatId int64, replyTo int, text string) {
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
		msg := tgbotapi.NewMessage(chatId, chunk)
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

func (self *Bot) isUserAllowed(userId int64) bool {
	if len(self.config.AllowedUsers) == 0 {
		return true
	}
	for _, id := range self.config.AllowedUsers {
		if id == userId {
			return true
		}
	}
	return false
}
