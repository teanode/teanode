package telegram

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/slashcommands"
)

const maxTelegramMessageLen = 4096

// telegramStreamPreview manages a live-updating preview message during LLM streaming.
// It sends an initial plain-text message on the first text delta, then edits it
// with adaptive backoff: starts at 500ms intervals, slowing down over time to
// avoid Telegram's rate limits. If a rate limit (429) is hit, the preview waits
// the prescribed duration, then deletes the old message and sends a new one to recover.
type telegramStreamPreview struct {
	mutex        sync.Mutex
	accumulated  strings.Builder
	lastSentText string
	messageId    int
	editCount    int
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
	interval := 500 * time.Millisecond
	for {
		select {
		case <-self.done:
			return
		case <-time.After(interval):
			retryAfter := self.flush()
			if retryAfter > 0 {
				// Rate limited — wait the prescribed time, then recover.
				log.Warningf("telegram rate limited, retrying after %ds", retryAfter)
				time.Sleep(time.Duration(retryAfter) * time.Second)
				self.recoverMessage()
				// Be conservative after hitting a rate limit.
				interval = 3 * time.Second
			} else {
				// Adaptive backoff: start fast (500ms), slow down by 250ms per edit, cap at 3s.
				self.mutex.Lock()
				editCount := self.editCount
				self.mutex.Unlock()
				milliseconds := 500 + editCount*250
				if milliseconds > 3000 {
					milliseconds = 3000
				}
				interval = time.Duration(milliseconds) * time.Millisecond
			}
		}
	}
}

// flush sends or edits the preview message. Returns retry_after seconds if rate limited, 0 otherwise.
func (self *telegramStreamPreview) flush() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	text := self.accumulated.String()
	if text == self.lastSentText || text == "" || self.stopped {
		return 0
	}
	if len(text) > maxTelegramMessageLen {
		text = text[:maxTelegramMessageLen]
	}

	if self.messageId == 0 {
		msg := tgbotapi.NewMessage(self.chatId, text)
		msg.ReplyToMessageID = self.replyTo
		sent, err := self.api.Send(msg)
		if err != nil {
			if retryAfter := telegramRetryAfter(err); retryAfter > 0 {
				return retryAfter
			}
			return 0
		}
		self.messageId = sent.MessageID
		self.lastSentText = text
		self.editCount++
	} else {
		edit := tgbotapi.NewEditMessageText(self.chatId, self.messageId, text)
		_, err := self.api.Send(edit)
		if err != nil {
			if retryAfter := telegramRetryAfter(err); retryAfter > 0 {
				return retryAfter
			}
			return 0
		}
		self.lastSentText = text
		self.editCount++
	}
	return 0
}

// recoverMessage deletes the current preview message and sends a fresh one
// with the accumulated text. This is used after a rate limit is hit to ensure
// the user sees the full streamed content instead of a truncated message.
func (self *telegramStreamPreview) recoverMessage() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.stopped {
		return
	}

	// Delete the stale message.
	if self.messageId != 0 {
		deleteMessage := tgbotapi.NewDeleteMessage(self.chatId, self.messageId)
		self.api.Request(deleteMessage)
		self.messageId = 0
	}

	// Send a new message with the current accumulated text.
	text := self.accumulated.String()
	if text == "" {
		return
	}
	if len(text) > maxTelegramMessageLen {
		text = text[:maxTelegramMessageLen]
	}

	msg := tgbotapi.NewMessage(self.chatId, text)
	sent, err := self.api.Send(msg)
	if err != nil {
		return
	}
	self.messageId = sent.MessageID
	self.lastSentText = text
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

// telegramRetryAfter extracts the retry_after duration from a Telegram API error.
// Returns the number of seconds to wait, or 0 if the error is not a rate limit.
func telegramRetryAfter(err error) int {
	var telegramError *tgbotapi.Error
	if errors.As(err, &telegramError) {
		return telegramError.RetryAfter
	}
	return 0
}

// telegramSubscribedRun tracks streaming state for a run received via Subscriber events.
type telegramSubscribedRun struct {
	preview      *telegramStreamPreview
	chatId       int64
	pendingMedia []*media.MediaContent
	mediaMutex   sync.Mutex
}

// Bot manages a Telegram bot that forwards messages to the agents.
type Bot struct {
	config        *configs.TelegramConfig
	agentRegistry *agents.AgentRegistry
	gateway       gw.Gateway
	api           *tgbotapi.BotAPI
	stopChannel   chan struct{}

	// Per-chat model overrides (chatId string -> model name).
	modelMutex     sync.RWMutex
	modelOverrides map[string]string

	// Runs initiated by the bot — skip these in OnEvent.
	activeConversationsMutex sync.RWMutex
	activeConversations      map[string]struct{} // conversationId -> present

	// Subscriber-driven streaming state.
	subscribedRunsMutex sync.Mutex
	subscribedRuns      map[string]*telegramSubscribedRun // runId -> state
}

// New creates a new Telegram bot that dynamically resolves the active agent and conversation from the registry.
func New(telegramConfig *configs.TelegramConfig, agentRegistry *agents.AgentRegistry, gateway gw.Gateway) *Bot {
	return &Bot{
		config:              telegramConfig,
		agentRegistry:       agentRegistry,
		gateway:             gateway,
		modelOverrides:      make(map[string]string),
		stopChannel:         make(chan struct{}),
		activeConversations: make(map[string]struct{}),
		subscribedRuns:      make(map[string]*telegramSubscribedRun),
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
		tgbotapi.BotCommand{Command: "compact", Description: "Compact current conversation history"},
		tgbotapi.BotCommand{Command: "restart", Description: "Restart the gateway"},
		tgbotapi.BotCommand{Command: "terminate", Description: "Shut down the gateway"},
		tgbotapi.BotCommand{Command: "help", Description: "Show available commands"},
		tgbotapi.BotCommand{Command: "ask", Description: "Ask the AI (required in groups)"},
	)
	if _, err := self.api.Request(commands); err != nil {
		log.Errorf("failed to set telegram commands: %v", err)
	}

	self.gateway.Subscribe(self)
	go self.poll()
	return nil
}

// Stop halts the bot.
func (self *Bot) Stop() {
	self.gateway.Unsubscribe(self)
	close(self.stopChannel)
	if self.api != nil {
		self.api.StopReceivingUpdates()
	}
}

// OnEvent implements gw.Subscriber. It handles conversation events for runs
// not initiated by this bot (e.g. scheduled jobs), streaming them to the appropriate chat.
func (self *Bot) OnEvent(eventType gw.EventType, payload interface{}) {
	if eventType != gw.EventTypeConversation {
		return
	}
	payloadMap, ok := payload.(map[string]interface{})
	if !ok {
		return
	}

	runId, _ := payloadMap["runId"].(string)
	state, _ := payloadMap["state"].(string)
	conversationId, _ := payloadMap["conversationId"].(string)

	if runId == "" || state == "" {
		return
	}

	// Skip conversations we're actively handling via callerCallbacks.
	self.activeConversationsMutex.RLock()
	_, isActive := self.activeConversations[conversationId]
	self.activeConversationsMutex.RUnlock()
	if isActive {
		return
	}

	// Use the single persisted chat ID.
	chatId := self.agentRegistry.TelegramChatID()
	if chatId == 0 {
		return
	}

	switch state {
	case "user_message":
		// Only handle runs from automated sources (e.g. scheduler). Interactive
		// sources (webui, discord, telegram) already have their own display.
		origin, _ := payloadMap["origin"].(string)
		if origin != "" {
			return
		}

		// Only forward events for the currently active agent.
		agentId, _ := payloadMap["agentId"].(string)
		if agentId != self.agentRegistry.ActiveAgentID() {
			return
		}

		// Show the triggering message so the user has context.
		triggerText, _ := payloadMap["text"].(string)
		if triggerText != "" {
			contextMessage := tgbotapi.NewMessage(chatId, "> "+strings.ReplaceAll(triggerText, "\n", "\n> "))
			self.api.Send(contextMessage)
		}

		preview := newTelegramStreamPreview(self.api, chatId, 0)
		self.subscribedRunsMutex.Lock()
		self.subscribedRuns[runId] = &telegramSubscribedRun{
			preview: preview,
			chatId:  chatId,
		}
		self.subscribedRunsMutex.Unlock()
		action := tgbotapi.NewChatAction(chatId, tgbotapi.ChatTyping)
		self.api.Send(action)

	case "delta":
		self.subscribedRunsMutex.Lock()
		subscribedRun := self.subscribedRuns[runId]
		self.subscribedRunsMutex.Unlock()
		if subscribedRun != nil {
			text, _ := payloadMap["text"].(string)
			subscribedRun.preview.Update(text)
		}

	case "tool_call":
		self.subscribedRunsMutex.Lock()
		subscribedRun := self.subscribedRuns[runId]
		self.subscribedRunsMutex.Unlock()
		if subscribedRun != nil {
			subscribedRun.preview.Reset()
			action := tgbotapi.NewChatAction(subscribedRun.chatId, tgbotapi.ChatTyping)
			self.api.Send(action)
		}

	case "tool_result":
		self.subscribedRunsMutex.Lock()
		subscribedRun := self.subscribedRuns[runId]
		self.subscribedRunsMutex.Unlock()
		if subscribedRun != nil {
			result, _ := payloadMap["result"].(string)
			if detected := media.DetectMedia(result); detected != nil && detected.Base64 != "" && media.IsImageFormat(detected.Format) {
				subscribedRun.mediaMutex.Lock()
				subscribedRun.pendingMedia = append(subscribedRun.pendingMedia, detected)
				subscribedRun.mediaMutex.Unlock()
			}
		}

	case "final":
		self.subscribedRunsMutex.Lock()
		subscribedRun := self.subscribedRuns[runId]
		delete(self.subscribedRuns, runId)
		self.subscribedRunsMutex.Unlock()
		if subscribedRun == nil {
			return
		}

		previewMessageId, _ := subscribedRun.preview.Stop()
		finalText, _ := payloadMap["text"].(string)

		// Send collected media as photo attachments.
		for _, mediaContent := range subscribedRun.pendingMedia {
			rawData, decodeError := base64.StdEncoding.DecodeString(mediaContent.Base64)
			if decodeError != nil {
				continue
			}
			filename := fmt.Sprintf("screenshot.%s", mediaContent.Format)
			photo := tgbotapi.NewPhoto(subscribedRun.chatId, tgbotapi.FileBytes{Name: filename, Bytes: rawData})
			if _, sendError := self.api.Send(photo); sendError != nil {
				log.Errorf("telegram photo send error: %v", sendError)
			}
		}

		// Reuse preview message or send new.
		if previewMessageId != 0 {
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
			edit := tgbotapi.NewEditMessageText(subscribedRun.chatId, previewMessageId, firstChunk)
			edit.ParseMode = "Markdown"
			if _, editError := self.api.Send(edit); editError != nil {
				edit.ParseMode = ""
				self.api.Send(edit)
			}
			if remaining != "" {
				self.sendChunked(subscribedRun.chatId, 0, remaining)
			}
		} else {
			self.sendChunked(subscribedRun.chatId, 0, finalText)
		}

	case "error", "aborted":
		self.subscribedRunsMutex.Lock()
		subscribedRun := self.subscribedRuns[runId]
		delete(self.subscribedRuns, runId)
		self.subscribedRunsMutex.Unlock()
		if subscribedRun == nil {
			return
		}

		subscribedRun.preview.Stop()
		subscribedRun.preview.Delete()
		if state == "error" {
			errorText, _ := payloadMap["error"].(string)
			if errorText == "" {
				errorText = "An error occurred while processing the request."
			}
			msg := tgbotapi.NewMessage(subscribedRun.chatId, "Sorry, an error occurred: "+errorText)
			self.api.Send(msg)
		}
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
		text = strings.TrimSpace(message.Caption)
	}
	hasAttachments := message.Photo != nil || message.Document != nil || message.Audio != nil || message.Video != nil || message.Voice != nil

	if text == "" && !hasAttachments {
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
		if text == "" && !hasAttachments {
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
	if self.gateway.GetActiveRun(conversationId) != "" {
		msg := tgbotapi.NewMessage(message.Chat.ID, "I'm still working on a previous request. Please wait.")
		msg.ReplyToMessageID = message.MessageID
		self.api.Send(msg)
		return
	}

	// Extract attachments from the message.
	var attachments []conversations.Attachment
	if hasAttachments {
		attachments = self.extractAttachments(message)
	}

	go self.handleMessage(conversationId, activeAgentId, message.Chat.ID, message.MessageID, text, chatIdStr, attachments)
}

func (self *Bot) handleMessage(conversationId, agentId string, chatId int64, replyTo int, message, chatIdStr string, attachments []conversations.Attachment) {
	defer deferutil.Recover()

	// Persist chat ID for subscriber-driven routing (e.g. scheduled jobs).
	self.agentRegistry.SetTelegramChatID(chatId)

	// Mark this conversation as actively handled by us.
	self.activeConversationsMutex.Lock()
	self.activeConversations[conversationId] = struct{}{}
	self.activeConversationsMutex.Unlock()

	defer func() {
		self.activeConversationsMutex.Lock()
		delete(self.activeConversations, conversationId)
		self.activeConversationsMutex.Unlock()
	}()

	// Send typing action.
	action := tgbotapi.NewChatAction(chatId, tgbotapi.ChatTyping)
	self.api.Send(action)

	var pendingMedia []*media.MediaContent
	var pendingMediaMutex sync.Mutex

	preview := newTelegramStreamPreview(self.api, chatId, replyTo)

	// Caller-specific callbacks: only preview/typing/media logic.
	callerCallbacks := &agents.RunCallbacks{
		OnTextDelta: func(text string) {
			preview.Update(text)
		},
		OnToolCall: func(toolName string, arguments string) {
			preview.Reset()
			// Re-send typing action after tool calls.
			action := tgbotapi.NewChatAction(chatId, tgbotapi.ChatTyping)
			self.api.Send(action)
		},
		OnToolResult: func(toolName string, result string) {
			// Collect media for sending as photos.
			if detected := media.DetectMedia(result); detected != nil && detected.Base64 != "" && media.IsImageFormat(detected.Format) {
				pendingMediaMutex.Lock()
				pendingMedia = append(pendingMedia, detected)
				pendingMediaMutex.Unlock()
			}
		},
	}

	handle := self.gateway.SendMessage(context.Background(), gw.SendMessageParameters{
		AgentID:        agentId,
		ConversationID: conversationId,
		Message:        message,
		Model:          self.getModel(chatIdStr),
		Origin:         "telegram",
		Attachments:    attachments,
	}, callerCallbacks)

	// Wait for completion.
	<-handle.Done

	previewMessageId, _ := preview.Stop()
	outcome := handle.Outcome()

	// Handle error: delete preview, send error message.
	if outcome.Error != nil {
		log.Errorf("telegram agent run error (conversation %s): %v", handle.ConversationID, outcome.Error)
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
		finalText := outcome.Response
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
	self.sendChunked(chatId, replyTo, outcome.Response)
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
		conversationId := self.gateway.NewConversation(activeAgentId, "")
		reply = fmt.Sprintf("New conversation started. (%s)", conversationId)

	case "reset", "clear":
		conversationId := self.agentRegistry.ActiveConversationID(activeAgentId)
		// Abort active run if any.
		if activeRunId := self.gateway.GetActiveRun(conversationId); activeRunId != "" {
			self.gateway.AbortRun(activeRunId)
		}
		if err := self.gateway.DeleteConversation(activeAgentId, conversationId); err != nil {
			reply = fmt.Sprintf("Error clearing conversation: %v", err)
		} else {
			newConversationId := self.gateway.NewConversation(activeAgentId, "")
			reply = fmt.Sprintf("Conversation cleared. New conversation started. (%s)", newConversationId)
		}

	case "stop":
		conversationId := self.agentRegistry.ActiveConversationID(activeAgentId)
		if activeRunId := self.gateway.GetActiveRun(conversationId); activeRunId != "" {
			self.gateway.AbortRun(activeRunId)
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
			if err := self.gateway.SetActiveAgent(arguments); err != nil {
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
		running := self.gateway.GetActiveRun(conversationId) != ""
		status := "idle"
		if running {
			status = "running"
		}
		providerName := ""
		if runner != nil {
			providerName = runner.Config.Models.DefaultProviderName()
		}
		reply = fmt.Sprintf("Agent: %s\nConversation: %s\nModel: %s\nProvider: %s\nStatus: %s", activeAgentId, conversationId, model, providerName, status)

	case "compact":
		conversationId := self.agentRegistry.ActiveConversationID(activeAgentId)
		if runner != nil {
			configuration, runnerProviders, _, _, _ := runner.Snapshot()
			result, err := agents.CompactConversation(context.Background(), runner.Conversations, runnerProviders, configuration, conversationId)
			if err != nil {
				reply = fmt.Sprintf("Error compacting: %v", err)
			} else {
				reply = fmt.Sprintf("Conversation compacted. Summarized %d messages.", result.SummarizedMessages)
			}
		} else {
			reply = "No active agent available."
		}

	case "restart":
		msg := tgbotapi.NewMessage(message.Chat.ID, "Restarting gateway...")
		msg.ReplyToMessageID = message.MessageID
		self.api.Send(msg)
		self.gateway.RequestLifecycle(gw.LifecycleRestart)
		return

	case "terminate":
		msg := tgbotapi.NewMessage(message.Chat.ID, "Shutting down gateway...")
		msg.ReplyToMessageID = message.MessageID
		self.api.Send(msg)
		self.gateway.RequestLifecycle(gw.LifecycleShutdown)
		return

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

// extractAttachments downloads files attached to a Telegram message and saves
// them to the media store, returning conversation attachment references.
func (self *Bot) extractAttachments(message *tgbotapi.Message) []conversations.Attachment {
	mediaStore := self.gateway.MediaStore()
	if mediaStore == nil {
		return nil
	}

	type telegramFile struct {
		fileId   string
		filename string
		mimeType string
	}

	var files []telegramFile

	// Photos: pick the largest size.
	if message.Photo != nil && len(message.Photo) > 0 {
		best := message.Photo[len(message.Photo)-1]
		files = append(files, telegramFile{fileId: best.FileID, filename: "photo.jpg", mimeType: "image/jpeg"})
	}
	if message.Document != nil {
		files = append(files, telegramFile{fileId: message.Document.FileID, filename: message.Document.FileName, mimeType: message.Document.MimeType})
	}
	if message.Audio != nil {
		name := "audio"
		if message.Audio.Title != "" {
			name = message.Audio.Title
		}
		files = append(files, telegramFile{fileId: message.Audio.FileID, filename: name, mimeType: message.Audio.MimeType})
	}
	if message.Video != nil {
		files = append(files, telegramFile{fileId: message.Video.FileID, filename: "video.mp4", mimeType: "video/mp4"})
	}
	if message.Voice != nil {
		files = append(files, telegramFile{fileId: message.Voice.FileID, filename: "voice.ogg", mimeType: message.Voice.MimeType})
	}

	var attachments []conversations.Attachment
	for _, file := range files {
		data, err := self.downloadTelegramFile(file.fileId)
		if err != nil {
			log.Errorf("failed to download telegram file %s: %v", file.fileId, err)
			continue
		}

		// Determine format from filename extension, fall back to MIME type.
		format := strings.TrimPrefix(filepath.Ext(file.filename), ".")
		if format == "" {
			format = media.FormatFromMimeType(file.mimeType)
		}
		if format == "" {
			format = "bin"
		}

		saved, err := mediaStore.Save(data, format, media.SaveOptions{
			SourceType:   "telegram",
			OriginalName: file.filename,
		})
		if err != nil {
			log.Errorf("failed to save telegram attachment: %v", err)
			continue
		}
		attachments = append(attachments, conversations.Attachment{
			MediaID:  saved.MediaID,
			Format:   format,
			Filename: file.filename,
		})
	}
	return attachments
}

// downloadTelegramFile downloads a file from Telegram by its file ID.
func (self *Bot) downloadTelegramFile(fileId string) ([]byte, error) {
	url, err := self.api.GetFileDirectURL(fileId)
	if err != nil {
		return nil, fmt.Errorf("getting file URL: %w", err)
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("downloading file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
