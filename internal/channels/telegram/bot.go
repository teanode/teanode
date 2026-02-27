package telegram

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/lifecycle"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/mimetypes"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/sessiontracker"
	"github.com/teanode/teanode/internal/util/slashcommands"
)

const maxTelegramMessageLength = 4096

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
	defer deferutil.Recover()
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
	if len(text) > maxTelegramMessageLength {
		text = text[:maxTelegramMessageLength]
	}

	if self.messageId == 0 {
		messageRequest := tgbotapi.NewMessage(self.chatId, text)
		messageRequest.ReplyToMessageID = self.replyTo
		sent, err := self.api.Send(messageRequest)
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
	if len(text) > maxTelegramMessageLength {
		text = text[:maxTelegramMessageLength]
	}

	messageRequest := tgbotapi.NewMessage(self.chatId, text)
	sent, err := self.api.Send(messageRequest)
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
	preview         *telegramStreamPreview
	chatId          int64
	origin          string
	originSessionId string
	triggerText     string
	pendingMedia    []*mimetypes.MediaContent
	mediaMutex      sync.Mutex
}

// Bot manages a Telegram bot that forwards messages to the agents.
type Bot struct {
	ctx            context.Context
	token          string
	coordinator    *coordinators.Coordinator
	pubsub         *pubsub.PubSub
	sessionTracker *sessiontracker.SessionTracker
	api            *tgbotapi.BotAPI
	stopChannel    chan struct{}

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

// New creates a new Telegram bot that dynamically resolves the default agent and conversation from the coordinator.
func New(ctx context.Context, token string, coordinator *coordinators.Coordinator, events *pubsub.PubSub, sessions *sessiontracker.SessionTracker) *Bot {
	return &Bot{
		ctx:                 ctx,
		token:               token,
		coordinator:         coordinator,
		pubsub:              events,
		sessionTracker:      sessions,
		modelOverrides:      make(map[string]string),
		stopChannel:         make(chan struct{}),
		activeConversations: make(map[string]struct{}),
		subscribedRuns:      make(map[string]*telegramSubscribedRun),
	}
}

// Start connects the bot to Telegram and begins polling for updates.
func (self *Bot) Start() error {
	api, err := tgbotapi.NewBotAPI(self.token)
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
		tgbotapi.BotCommand{Command: "agent", Description: "Show or switch the default agent"},
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

	self.pubsub.Subscribe(self)
	go self.poll()
	return nil
}

// Stop halts the bot.
func (self *Bot) Stop() {
	self.pubsub.Unsubscribe(self)
	close(self.stopChannel)
	if self.api != nil {
		self.api.StopReceivingUpdates()
	}
}

func (self *Bot) shouldForwardDisconnectedSession(userId, agentId, conversationId, originSessionId string) bool {
	if originSessionId == "" {
		return false
	}
	if userId == "" {
		return false
	}
	var defaultAgentId string
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		user, err := transaction.GetUser(ctx, userId, nil)
		if err != nil {
			return nil
		}
		defaultAgentId = user.GetDefaultAgentID()
		return nil
	})
	if agentId != defaultAgentId {
		return false
	}
	defaultConversationId := self.coordinator.EnsureDefaultConversation(userId, defaultAgentId)
	if defaultConversationId == "" || conversationId == "" {
		return false
	}
	return conversationId == defaultConversationId
}

// OnEvent implements pubsub.Subscriber. It handles conversation events for runs
// not initiated by this bot (e.g. scheduled jobs), streaming them to the appropriate chat.
func (self *Bot) OnEvent(eventType pubsub.EventType, payload interface{}) {
	if eventType != pubsub.EventTypeConversation {
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

	userId, _ := payloadMap["userId"].(string)
	chatId := self.telegramChatIdForUser(userId)
	if chatId == 0 {
		return
	}

	switch state {
	case "user_message":
		agentId, _ := payloadMap["agentId"].(string)
		// Only forward events for the default agent.
		var defaultAgentId string
		_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
			user, err := transaction.GetUser(ctx, userId, nil)
			if err != nil {
				return nil
			}
			defaultAgentId = user.GetDefaultAgentID()
			return nil
		})
		if agentId != defaultAgentId {
			return
		}

		origin, _ := payloadMap["origin"].(string)
		triggerText, _ := payloadMap["text"].(string)

		if origin == "" {
			// Scheduled/automated runs: stream live into Telegram.
			if triggerText != "" {
				contextMessage := tgbotapi.NewMessage(chatId, "> "+strings.ReplaceAll(triggerText, "\n", "\n> "))
				self.api.Send(contextMessage)
			}
			preview := newTelegramStreamPreview(self.api, chatId, 0)
			self.subscribedRunsMutex.Lock()
			self.subscribedRuns[runId] = &telegramSubscribedRun{
				preview:     preview,
				chatId:      chatId,
				origin:      origin,
				triggerText: triggerText,
			}
			self.subscribedRunsMutex.Unlock()
			action := tgbotapi.NewChatAction(chatId, tgbotapi.ChatTyping)
			self.api.Send(action)
			return
		}

		if origin != "webui" {
			return
		}

		originSessionId, _ := payloadMap["originSessionId"].(string)
		if !self.shouldForwardDisconnectedSession(userId, agentId, conversationId, originSessionId) {
			return
		}

		// Session runs are only delivered to Telegram if the originating web session disconnects.
		self.subscribedRunsMutex.Lock()
		self.subscribedRuns[runId] = &telegramSubscribedRun{
			chatId:          chatId,
			origin:          origin,
			originSessionId: originSessionId,
			triggerText:     triggerText,
		}
		self.subscribedRunsMutex.Unlock()

	case "delta":
		self.subscribedRunsMutex.Lock()
		subscribedRun := self.subscribedRuns[runId]
		self.subscribedRunsMutex.Unlock()
		if subscribedRun != nil && subscribedRun.preview != nil {
			text, _ := payloadMap["text"].(string)
			subscribedRun.preview.Update(text)
		}

	case "tool_call":
		self.subscribedRunsMutex.Lock()
		subscribedRun := self.subscribedRuns[runId]
		self.subscribedRunsMutex.Unlock()
		if subscribedRun != nil && subscribedRun.preview != nil {
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
			if detected := mimetypes.DetectMedia(result); detected != nil && detected.Base64 != "" && mimetypes.IsImageFormat(detected.Format) {
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

		// Scheduled/automated runs stream via preview and always post final output.
		if subscribedRun.preview != nil {
			previewMessageId, _ := subscribedRun.preview.Stop()
			if previewMessageId != 0 {
				firstChunk := finalText
				remaining := ""
				if len(finalText) > maxTelegramMessageLength {
					cut := strings.LastIndex(finalText[:maxTelegramMessageLength], "\n")
					if cut < maxTelegramMessageLength/2 {
						cut = maxTelegramMessageLength
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
			return
		}

		// Session fallback: only notify when the originating web session is disconnected.
		if subscribedRun.origin == "webui" && !self.sessionTracker.IsConnected(subscribedRun.originSessionId) {
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

		if subscribedRun.preview != nil {
			subscribedRun.preview.Stop()
			subscribedRun.preview.Delete()
		}
		if state == "error" && subscribedRun.preview != nil {
			errorText, _ := payloadMap["error"].(string)
			if errorText == "" {
				errorText = "An error occurred while processing the request."
			}
			messageRequest := tgbotapi.NewMessage(subscribedRun.chatId, "Sorry, an error occurred: "+errorText)
			self.api.Send(messageRequest)
		} else if state == "error" && subscribedRun.origin == "webui" && !self.sessionTracker.IsConnected(subscribedRun.originSessionId) {
			errorText, _ := payloadMap["error"].(string)
			if errorText == "" {
				errorText = "An error occurred while processing the request."
			}
			messageRequest := tgbotapi.NewMessage(subscribedRun.chatId, "Session run failed: "+errorText)
			self.api.Send(messageRequest)
		}
	}
}

func (self *Bot) poll() {
	defer deferutil.Recover()

	updateConfiguration := tgbotapi.NewUpdate(0)
	updateConfiguration.Timeout = 30

	updates := self.api.GetUpdatesChan(updateConfiguration)
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

	text := message.Text
	if text == "" {
		text = message.Caption
	}
	hasAttachments := message.Photo != nil || message.Document != nil || message.Audio != nil || message.Video != nil || message.Voice != nil

	if text == "" && !hasAttachments {
		return
	}

	chatIdString := fmt.Sprintf("%d", message.Chat.ID)
	user := self.linkedUserForTelegramChat(chatIdString)
	if user == nil {
		messageRequest := tgbotapi.NewMessage(message.Chat.ID, unlinkedTelegramMessage(chatIdString))
		messageRequest.ReplyToMessageID = message.MessageID
		self.api.Send(messageRequest)
		return
	}
	userId := user.ID

	// Handle /start specially — always respond with greeting.
	if text == "/start" || strings.HasPrefix(text, "/start@") {
		messageRequest := tgbotapi.NewMessage(message.Chat.ID, "Hello! Send me a message and I'll respond using AI.\n\n"+slashcommands.HelpText())
		self.api.Send(messageRequest)
		return
	}

	// Handle /ask — strip prefix and pass to agents.
	if strings.HasPrefix(text, "/ask") {
		// Strip /ask or /ask@botname prefix.
		rest := text[4:]
		if index := strings.Index(rest, " "); strings.HasPrefix(rest, "@") && index > 0 {
			rest = rest[index:]
		}
		text = rest
		if text == "" && !hasAttachments {
			messageRequest := tgbotapi.NewMessage(message.Chat.ID, "Usage: /ask <message>")
			messageRequest.ReplyToMessageID = message.MessageID
			self.api.Send(messageRequest)
			return
		}
		// Fall through to agent handling below.
	} else if name, arguments, ok := slashcommands.Parse(text); ok {
		self.handleCommand(user, message, chatIdString, name, arguments)
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

	defaultAgentId := user.GetDefaultAgentID()
	if defaultAgentId == "" {
		messageRequest := tgbotapi.NewMessage(message.Chat.ID, "No default agent available.")
		messageRequest.ReplyToMessageID = message.MessageID
		self.api.Send(messageRequest)
		return
	}
	conversationId := self.coordinator.EnsureDefaultConversation(userId, defaultAgentId)

	// Check if there's already an active run for this conversation.
	if self.coordinator.GetActiveConversationRunner(conversationId) != nil {
		messageRequest := tgbotapi.NewMessage(message.Chat.ID, "I'm still working on a previous request. Please wait.")
		messageRequest.ReplyToMessageID = message.MessageID
		self.api.Send(messageRequest)
		return
	}

	// Extract attachments from the message.
	var attachments []map[string]string
	if hasAttachments {
		attachments = self.extractAttachments(message)
	}

	go self.handleMessage(user, conversationId, defaultAgentId, message.Chat.ID, message.MessageID, text, chatIdString, attachments)
}

func unlinkedTelegramMessage(chatId string) string {
	return fmt.Sprintf(
		"Your Telegram chat is not linked to a TeaNode user yet.\n\n"+
			"Link it by editing `%s` and adding:\n"+
			"channelLinks:\n"+
			"  telegram:\n"+
			"    \"%s\": \"<userId>\"\n\n"+
			"`<userId>` must exist under `users:` in the same file.\n"+
			"Example:\n"+
			"users:\n"+
			"  user-1:\n"+
			"    username: alice\n"+
			"channelLinks:\n"+
			"  telegram:\n"+
			"    \"%s\": \"user-1\"",
		"security.yaml",
		chatId,
		chatId,
	)
}

func (self *Bot) handleMessage(user *models.User, conversationId, agentId string, chatId int64, replyTo int, message, chatIdString string, attachments []map[string]string) {
	defer deferutil.Recover()

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

	var pendingMedia []*mimetypes.MediaContent
	var pendingMediaMutex sync.Mutex

	preview := newTelegramStreamPreview(self.api, chatId, replyTo)

	// Caller-specific callbacks: only preview/typing/media logic.
	callerCallbacks := &runners.RunCallbacks{
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
			if detected := mimetypes.DetectMedia(result); detected != nil && detected.Base64 != "" && mimetypes.IsImageFormat(detected.Format) {
				pendingMediaMutex.Lock()
				pendingMedia = append(pendingMedia, detected)
				pendingMediaMutex.Unlock()
			}
		},
	}

	runContext := models.ContextWithUserSessionToken(self.ctx, user, nil, nil)
	handle, sendError := self.coordinator.Run(runContext, coordinators.RunParameters{
		AgentID:        agentId,
		ConversationID: conversationId,
		Message:        message,
		Model:          self.getModel(chatIdString),
		Origin:         "telegram",
		Attachments:    attachments,
	}, callerCallbacks)

	if sendError != nil {
		log.Errorf("telegram agent send error (conversation %s): %v", conversationId, sendError)
		preview.Stop()
		preview.Delete()
		messageRequest := tgbotapi.NewMessage(chatId, "Sorry, an error occurred while processing your request.")
		messageRequest.ReplyToMessageID = replyTo
		self.api.Send(messageRequest)
		return
	}

	// Wait for completion.
	result, runError := handle.Wait()

	previewMessageId, _ := preview.Stop()

	// Handle error: delete preview, send error message.
	if runError != nil {
		log.Errorf("telegram agent run error (conversation %s): %v", handle.ConversationID, runError)
		preview.Delete()
		messageRequest := tgbotapi.NewMessage(chatId, "Sorry, an error occurred while processing your request.")
		messageRequest.ReplyToMessageID = replyTo
		self.api.Send(messageRequest)
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
		if len(finalText) > maxTelegramMessageLength {
			cut := strings.LastIndex(finalText[:maxTelegramMessageLength], "\n")
			if cut < maxTelegramMessageLength/2 {
				cut = maxTelegramMessageLength
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

func (self *Bot) getModel(chatIdString string) string {
	self.modelMutex.RLock()
	defer self.modelMutex.RUnlock()
	return self.modelOverrides[chatIdString]
}

func (self *Bot) handleCommand(user *models.User, message *tgbotapi.Message, chatIdString, name, arguments string) {
	var reply string

	defaultAgentId := user.GetDefaultAgentID()
	if defaultAgentId == "" {
		replyMessage := tgbotapi.NewMessage(message.Chat.ID, "No default agent available.")
		replyMessage.ReplyToMessageID = message.MessageID
		self.api.Send(replyMessage)
		return
	}
	switch name {
	case "new":
		conversationId := self.coordinator.NewDefaultConversation(user.ID, defaultAgentId)
		reply = fmt.Sprintf("New conversation started. (%s)", conversationId)

	case "reset", "clear":
		conversationId := self.coordinator.EnsureDefaultConversation(user.ID, defaultAgentId)
		// Abort active run if any.
		self.coordinator.AbortConversationRun(conversationId)
		newConversationId := self.coordinator.NewDefaultConversation(user.ID, defaultAgentId)
		reply = fmt.Sprintf("Conversation cleared. New conversation started. (%s)", newConversationId)

	case "stop":
		conversationId := self.coordinator.EnsureDefaultConversation(user.ID, defaultAgentId)
		if self.coordinator.GetActiveConversationRunner(conversationId) != nil {
			self.coordinator.AbortConversationRun(conversationId)
			reply = "Run cancelled."
		} else {
			reply = "No active run to cancel."
		}

	case "model":
		if arguments == "" {
			model := self.getModel(chatIdString)
			if model == "" {
				model = self.resolveDefaultModel()
			}
			reply = fmt.Sprintf("Current model: %s", model)
		} else {
			self.modelMutex.Lock()
			self.modelOverrides[chatIdString] = arguments
			self.modelMutex.Unlock()
			reply = fmt.Sprintf("Model set to %s.", arguments)
		}

	case "agent":
		if arguments == "" {
			var lines []string
			lines = append(lines, fmt.Sprintf("Default agent: %s", defaultAgentId))
			lines = append(lines, "Agents:")
			for _, agentId := range self.listAgentIdsFromStore() {
				marker := "  "
				if agentId == defaultAgentId {
					marker = "* "
				}
				lines = append(lines, marker+agentId)
			}
			reply = strings.Join(lines, "\n")
		} else {
			if !self.agentExistsInStore(arguments) {
				reply = fmt.Sprintf("Error: agent not found: %s", arguments)
			} else if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
				_, err := transaction.ModifyUser(ctx, user.ID, func(user *models.User) error {
					user.DefaultAgentID = ptrto.Value(arguments)
					return nil
				}, nil)
				return err
			}); err != nil {
				reply = fmt.Sprintf("Error: %v", err)
			} else {
				self.pubsub.Broadcast(pubsub.EventTypeDefaultAgent, map[string]interface{}{
					"defaultAgentId": arguments,
					"userId":         user.ID,
				})
				newConversationId := self.coordinator.EnsureDefaultConversation(user.ID, arguments)
				reply = fmt.Sprintf("Switched to agent %s. (conversation: %s)", arguments, newConversationId)
			}
		}

	case "status":
		conversationId := self.coordinator.EnsureDefaultConversation(user.ID, defaultAgentId)
		model := self.getModel(chatIdString)
		if model == "" {
			model = self.resolveDefaultModel()
		}
		running := self.coordinator.GetActiveConversationRunner(conversationId) != nil
		status := "idle"
		if running {
			status = "running"
		}
		providerName := self.coordinator.ProviderRegistry().DefaultProvider()
		reply = fmt.Sprintf("Agent: %s\nConversation: %s\nModel: %s\nProvider: %s\nStatus: %s", defaultAgentId, conversationId, model, providerName, status)

	case "compact":
		conversationId := self.coordinator.EnsureDefaultConversation(user.ID, defaultAgentId)
		compactContext := models.ContextWithUserSessionToken(self.ctx, user, nil, nil)
		compactResult, compactError := self.coordinator.CompactConversation(compactContext, defaultAgentId, conversationId)
		if compactError != nil {
			reply = fmt.Sprintf("Error compacting: %v", compactError)
		} else {
			reply = fmt.Sprintf("Conversation compacted. Summarized %d messages.", compactResult.SummarizedMessages)
		}

	case "restart":
		messageRequest := tgbotapi.NewMessage(message.Chat.ID, "Restarting gateway...")
		messageRequest.ReplyToMessageID = message.MessageID
		self.api.Send(messageRequest)
		lifecycle.LifecycleFromContext(self.ctx).RequestLifecycle(lifecycle.Restart)
		return

	case "terminate":
		messageRequest := tgbotapi.NewMessage(message.Chat.ID, "Shutting down gateway...")
		messageRequest.ReplyToMessageID = message.MessageID
		self.api.Send(messageRequest)
		lifecycle.LifecycleFromContext(self.ctx).RequestLifecycle(lifecycle.Shutdown)
		return

	case "help":
		reply = slashcommands.HelpText()
	}

	if reply != "" {
		messageRequest := tgbotapi.NewMessage(message.Chat.ID, reply)
		messageRequest.ReplyToMessageID = message.MessageID
		self.api.Send(messageRequest)
	}
}

func (self *Bot) linkedUserForTelegramChat(chatId string) *models.User {
	var chatIdNumeric int64
	if _, scanError := fmt.Sscanf(chatId, "%d", &chatIdNumeric); scanError != nil {
		return nil
	}
	var user *models.User
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		foundUser, err := transaction.GetUserByTelegramChatID(ctx, chatIdNumeric, nil)
		if err == nil {
			user = foundUser
		}
		return nil
	})
	return user
}

func (self *Bot) agentExistsInStore(agentId string) bool {
	exists := false
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		if _, getError := transaction.GetAgent(ctx, agentId, nil); getError == nil {
			exists = true
		}
		return nil
	})
	return exists
}

func (self *Bot) resolveDefaultModel() string {
	var defaultModel string
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, getError := transaction.GetConfiguration(ctx, nil)
		if getError != nil {
			return getError
		}
		if configuration.Models != nil {
			defaultModel = configuration.Models.GetDefault()
		}
		return nil
	})
	return defaultModel
}

func (self *Bot) listAgentIdsFromStore() []string {
	agentIds := make([]string, 0)
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		agents, listError := transaction.ListAgents(ctx, nil)
		if listError != nil {
			return nil
		}
		agentIds = make([]string, 0, len(agents))
		for _, agent := range agents {
			agentIds = append(agentIds, agent.ID)
		}
		sort.Strings(agentIds)
		return nil
	})
	return agentIds
}

func (self *Bot) telegramChatIdForUser(userId string) int64 {
	if userId == "" {
		return 0
	}
	chatId := int64(0)
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		user, err := transaction.GetUser(ctx, userId, nil)
		if err != nil || user.TelegramChatID == nil {
			return nil
		}
		chatId = *user.TelegramChatID
		return nil
	})
	return chatId
}

func (self *Bot) sendChunked(chatId int64, replyTo int, text string) {
	if text == "" {
		return
	}
	first := true
	for len(text) > 0 {
		chunk := text
		if len(chunk) > maxTelegramMessageLength {
			// Try to split at a newline.
			cut := strings.LastIndex(chunk[:maxTelegramMessageLength], "\n")
			if cut < maxTelegramMessageLength/2 {
				cut = maxTelegramMessageLength
			}
			chunk = text[:cut]
		}
		messageRequest := tgbotapi.NewMessage(chatId, chunk)
		messageRequest.ParseMode = "Markdown"
		if first {
			messageRequest.ReplyToMessageID = replyTo
			first = false
		}
		if _, err := self.api.Send(messageRequest); err != nil {
			// Retry without Markdown parse mode in case of formatting errors.
			messageRequest.ParseMode = ""
			if _, err := self.api.Send(messageRequest); err != nil {
				log.Errorf("telegram send error: %v", err)
				return
			}
		}
		text = text[len(chunk):]
	}
}

// extractAttachments downloads files attached to a Telegram message and saves
// them through the configured store, returning conversation attachment references.
func (self *Bot) extractAttachments(message *tgbotapi.Message) []map[string]string {
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

	var attachments []map[string]string
	for _, file := range files {
		data, err := self.downloadTelegramFile(file.fileId)
		if err != nil {
			log.Errorf("failed to download telegram file %s: %v", file.fileId, err)
			continue
		}

		// Determine format from filename extension, fall back to MIME type.
		format := strings.TrimPrefix(filepath.Ext(file.filename), ".")
		if format == "" {
			format = mimetypes.FormatFromMIMEType(file.mimeType)
		}
		if format == "" {
			format = "bin"
		}

		contentType := file.mimeType
		if contentType == "" {
			contentType = mimetypes.MIMETypeFromFormat(format)
		}
		var createdMedia *models.Media
		createError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
			var saveError error
			createdMedia, saveError = transaction.CreateMedia(ctx, bytes.NewReader(data), &models.Media{
				Format:       &format,
				ContentType:  &contentType,
				Source:       ptrto.Value(models.MediaSourceTelegram),
				OriginalName: &file.filename,
			}, nil)
			return saveError
		})
		if createError != nil {
			log.Errorf("failed to save telegram attachment: %v", createError)
			continue
		}
		attachments = append(attachments, map[string]string{
			"mediaId":  createdMedia.ID,
			"format":   format,
			"filename": file.filename,
		})
	}
	return attachments
}

// downloadTelegramFile downloads a file from Telegram by its file ID.
func (self *Bot) downloadTelegramFile(fileId string) ([]byte, error) {
	fileUrl, err := self.api.GetFileDirectURL(fileId)
	if err != nil {
		return nil, fmt.Errorf("getting file URL: %w", err)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Get(fileUrl)
	if err != nil {
		return nil, fmt.Errorf("downloading file: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d", response.StatusCode)
	}
	const maxFileSize = 100 * 1024 * 1024 // 100 MB
	return io.ReadAll(io.LimitReader(response.Body, maxFileSize))
}
