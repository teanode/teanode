package telegram

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/slashcommands"
	"github.com/teanode/teanode/internal/util/ulid"
)

const maxTelegramMessageLen = 4096

// telegramStreamPreview manages a live-updating preview message during LLM streaming.
// It sends an initial plain-text message on the first text delta, then edits it
// at a capped rate (every 500ms) as more tokens arrive.
type telegramStreamPreview struct {
	mutex        sync.Mutex
	accumulated  strings.Builder
	lastSentText string
	messageId    int
	stopped      bool
	done         chan struct{}
	chatId       int64
	replyTo      int
	api          *tgbotapi.BotAPI
}

func newTelegramStreamPreview(api *tgbotapi.BotAPI, chatId int64, replyTo int) *telegramStreamPreview {
	preview := &telegramStreamPreview{
		chatId:  chatId,
		replyTo: replyTo,
		api:     api,
		done:    make(chan struct{}),
	}
	go preview.run()
	return preview
}

func (self *telegramStreamPreview) run() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-self.done:
			return
		case <-ticker.C:
			self.flush()
		}
	}
}

func (self *telegramStreamPreview) flush() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	text := self.accumulated.String()
	if text == self.lastSentText || text == "" || self.stopped {
		return
	}
	if len(text) > maxTelegramMessageLen {
		text = text[:maxTelegramMessageLen]
	}
	self.lastSentText = text

	if self.messageId == 0 {
		msg := tgbotapi.NewMessage(self.chatId, text)
		msg.ReplyToMessageID = self.replyTo
		sent, err := self.api.Send(msg)
		if err != nil {
			return
		}
		self.messageId = sent.MessageID
	} else {
		edit := tgbotapi.NewEditMessageText(self.chatId, self.messageId, text)
		self.api.Send(edit)
	}
}

// Update appends a text delta to the accumulated buffer.
func (self *telegramStreamPreview) Update(delta string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.stopped {
		return
	}
	self.accumulated.WriteString(delta)
}

// Reset clears the buffer for the next LLM round (after a tool call).
func (self *telegramStreamPreview) Reset() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.accumulated.Reset()
	self.lastSentText = ""
}

// Stop shuts down the background goroutine, performs a final flush, and returns
// the preview message ID and accumulated text.
func (self *telegramStreamPreview) Stop() (int, string) {
	close(self.done)
	self.flush()
	self.mutex.Lock()
	self.stopped = true
	messageId := self.messageId
	text := self.accumulated.String()
	self.mutex.Unlock()
	return messageId, text
}

// Delete removes the preview message from the chat if one was sent.
func (self *telegramStreamPreview) Delete() {
	self.mutex.Lock()
	messageId := self.messageId
	self.messageId = 0
	self.mutex.Unlock()
	if messageId != 0 {
		deleteMessage := tgbotapi.NewDeleteMessage(self.chatId, messageId)
		self.api.Request(deleteMessage)
	}
}

// Bot manages a Telegram bot that forwards messages to the agents.
type Bot struct {
	config        *configs.TelegramConfig
	agentRegistry *agents.AgentRegistry
	api           *tgbotapi.BotAPI
	stopChannel   chan struct{}

	// Active runs per conversation id — prevents concurrent runs on same conversation.
	activeMutex sync.Mutex
	active      map[string]context.CancelFunc

	// Per-chat model overrides (chatId string → model name).
	modelMutex     sync.RWMutex
	modelOverrides map[string]string

	Broadcast       func(event string, payload interface{})
	SetActiveRun    func(conversationId, runId string)
	ClearActiveRun  func(conversationId, runId string)
	SetActiveAgent  func(agentId string) error
	NewConversation func(agentId string) string
}

// New creates a new Telegram bot that dynamically resolves the active agent and conversation from the registry.
func New(telegramConfig *configs.TelegramConfig, agentRegistry *agents.AgentRegistry) *Bot {
	return &Bot{
		config:         telegramConfig,
		agentRegistry:  agentRegistry,
		active:         make(map[string]context.CancelFunc),
		modelOverrides: make(map[string]string),
		stopChannel:    make(chan struct{}),
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
		tgbotapi.BotCommand{Command: "new", Description: "Start a new conversation"},
		tgbotapi.BotCommand{Command: "reset", Description: "Clear current conversation history"},
		tgbotapi.BotCommand{Command: "clear", Description: "Clear current conversation and start new"},
		tgbotapi.BotCommand{Command: "stop", Description: "Cancel the current run"},
		tgbotapi.BotCommand{Command: "model", Description: "Show or set the model"},
		tgbotapi.BotCommand{Command: "agent", Description: "Show or switch the active agent"},
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

	activeAgentId := self.agentRegistry.ActiveAgentID()
	runner := self.agentRegistry.Get(activeAgentId)
	if runner == nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, "No active agent available.")
		msg.ReplyToMessageID = message.MessageID
		self.api.Send(msg)
		return
	}
	conversationId := self.agentRegistry.ActiveConversationID(activeAgentId)

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

	go self.handleMessage(ctx, cancel, runner, conversationId, message.Chat.ID, message.MessageID, text)
}

func (self *Bot) handleMessage(ctx context.Context, cancel context.CancelFunc, runner *agents.Runner, conversationId string, chatId int64, replyTo int, message string) {
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

	preview := newTelegramStreamPreview(self.api, chatId, replyTo)
	broadcast := self.Broadcast

	callbacks := &agents.RunCallbacks{
		OnTextDelta: func(text string) {
			if broadcast != nil {
				broadcast("chat", map[string]interface{}{
					"state":          "delta",
					"runId":          runId,
					"conversationId": conversationId,
					"text":           text,
				})
			}
			preview.Update(text)
		},
		OnToolCall: func(toolName string, arguments string) {
			if broadcast != nil {
				broadcast("chat", map[string]interface{}{
					"state":          "tool_call",
					"runId":          runId,
					"conversationId": conversationId,
					"toolName":       toolName,
					"arguments":      arguments,
				})
			}
			preview.Reset()
			// Re-send typing action after tool calls.
			action := tgbotapi.NewChatAction(chatId, tgbotapi.ChatTyping)
			self.api.Send(action)
		},
		OnToolResult: func(toolName string, result string) {
			if broadcast != nil {
				broadcast("chat", map[string]interface{}{
					"state":          "tool_result",
					"runId":          runId,
					"conversationId": conversationId,
					"toolName":       toolName,
					"result":         result,
				})
			}
			// Collect media for sending as photos.
			if detected := media.DetectMedia(result); detected != nil && detected.Base64 != "" && media.IsImageFormat(detected.Format) {
				pendingMediaMutex.Lock()
				pendingMedia = append(pendingMedia, detected)
				pendingMediaMutex.Unlock()
			}
		},
	}

	chatIdStr := fmt.Sprintf("%d", chatId)
	result, err := runner.Run(ctx, agents.RunParams{
		ConversationID: conversationId,
		Message:        message,
		Model:          self.getModel(chatIdStr),
	}, callbacks)

	previewMessageId, _ := preview.Stop()

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

	// Handle error: delete preview, send error message.
	if err != nil {
		log.Errorf("telegram agent run error (conversation %s): %v", conversationId, err)
		preview.Delete()
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

	// Reuse the preview message as the final message by editing it.
	if previewMessageId != 0 {
		finalText := result.Response
		firstChunk := finalText
		remaining := ""
		if len(finalText) > maxTelegramMessageLen {
			cut := strings.LastIndex(finalText[:maxTelegramMessageLen], "\n")
			if cut < maxTelegramMessageLen/2 {
				cut = maxTelegramMessageLen
			}
			firstChunk = finalText[:cut]
			remaining = finalText[cut:]
		}
		edit := tgbotapi.NewEditMessageText(chatId, previewMessageId, firstChunk)
		edit.ParseMode = "Markdown"
		if _, editError := self.api.Send(edit); editError != nil {
			edit.ParseMode = ""
			self.api.Send(edit)
		}
		if remaining != "" {
			self.sendChunked(chatId, 0, remaining)
		}
		return
	}

	// No preview message was created — send as new message(s).
	self.sendChunked(chatId, replyTo, result.Response)
}

func (self *Bot) getModel(chatIdStr string) string {
	self.modelMutex.RLock()
	defer self.modelMutex.RUnlock()
	return self.modelOverrides[chatIdStr]
}

func (self *Bot) handleCommand(message *tgbotapi.Message, chatIdStr, name, arguments string) {
	var reply string

	activeAgentId := self.agentRegistry.ActiveAgentID()
	runner := self.agentRegistry.Get(activeAgentId)

	switch name {
	case "new":
		conversationId := self.newConversation(activeAgentId)
		reply = fmt.Sprintf("New conversation started. (%s)", conversationId)

	case "reset", "clear":
		conversationId := self.agentRegistry.ActiveConversationID(activeAgentId)
		// Cancel active run if any.
		self.activeMutex.Lock()
		if cancel, found := self.active[conversationId]; found {
			cancel()
			if runner != nil {
				runner.CancelConversation(conversationId)
			}
		}
		self.activeMutex.Unlock()
		if runner != nil {
			if err := runner.Conversations.Delete(conversationId); err != nil {
				reply = fmt.Sprintf("Error clearing conversation: %v", err)
			} else {
				newConversationId := self.newConversation(activeAgentId)
				reply = fmt.Sprintf("Conversation cleared. New conversation started. (%s)", newConversationId)
			}
		} else {
			reply = "No active agent available."
		}

	case "stop":
		conversationId := self.agentRegistry.ActiveConversationID(activeAgentId)
		self.activeMutex.Lock()
		cancel, found := self.active[conversationId]
		self.activeMutex.Unlock()
		if found {
			cancel()
			if runner != nil {
				runner.CancelConversation(conversationId)
			}
			reply = "Run cancelled."
		} else {
			reply = "No active run to cancel."
		}

	case "model":
		if arguments == "" {
			model := self.getModel(chatIdStr)
			if model == "" && runner != nil {
				model = runner.Config.Models.Default
			}
			reply = fmt.Sprintf("Current model: %s", model)
		} else {
			self.modelMutex.Lock()
			self.modelOverrides[chatIdStr] = arguments
			self.modelMutex.Unlock()
			reply = fmt.Sprintf("Model set to %s.", arguments)
		}

	case "agent":
		if arguments == "" {
			var lines []string
			lines = append(lines, fmt.Sprintf("Active agent: %s", activeAgentId))
			lines = append(lines, "Agents:")
			for _, agentId := range self.agentRegistry.AgentIDs() {
				marker := "  "
				if agentId == activeAgentId {
					marker = "* "
				}
				lines = append(lines, marker+agentId)
			}
			reply = strings.Join(lines, "\n")
		} else {
			if err := self.setActiveAgent(arguments); err != nil {
				reply = fmt.Sprintf("Error: %v", err)
			} else {
				newConversationId := self.agentRegistry.ActiveConversationID(arguments)
				reply = fmt.Sprintf("Switched to agent %s. (conversation: %s)", arguments, newConversationId)
			}
		}

	case "status":
		conversationId := self.agentRegistry.ActiveConversationID(activeAgentId)
		model := self.getModel(chatIdStr)
		if model == "" && runner != nil {
			model = runner.Config.Models.Default
		}
		self.activeMutex.Lock()
		_, running := self.active[conversationId]
		self.activeMutex.Unlock()
		status := "idle"
		if running {
			status = "running"
		}
		providerName := ""
		if runner != nil {
			providerName = runner.Config.Models.DefaultProviderName()
		}
		reply = fmt.Sprintf("Agent: %s\nConversation: %s\nModel: %s\nProvider: %s\nStatus: %s", activeAgentId, conversationId, model, providerName, status)

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

func (self *Bot) setActiveAgent(agentId string) error {
	if self.SetActiveAgent != nil {
		return self.SetActiveAgent(agentId)
	}
	return self.agentRegistry.SetActiveAgent(agentId)
}

func (self *Bot) newConversation(agentId string) string {
	if self.NewConversation != nil {
		return self.NewConversation(agentId)
	}
	return self.agentRegistry.NewConversation(agentId)
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
