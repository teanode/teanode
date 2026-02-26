package discord

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/lifecycle"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/mimetypes"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/slashcommands"
)

const maxDiscordMessageLen = 2000

// discordStreamPreview manages a live-updating preview message during LLM streaming.
// It sends an initial message on the first text delta, then edits it at a capped
// rate (every 500ms) as more tokens arrive. Discord renders markdown client-side,
// so partial/unclosed markup is tolerated during streaming.
type discordStreamPreview struct {
	mutex        sync.Mutex
	accumulated  strings.Builder
	lastSentText string
	messageId    string
	stopped      bool
	done         chan struct{}
	channelId    string
	session      *discordgo.Session
}

func newDiscordStreamPreview(session *discordgo.Session, channelId string) *discordStreamPreview {
	preview := &discordStreamPreview{
		channelId: channelId,
		session:   session,
		done:      make(chan struct{}),
	}
	go preview.run()
	return preview
}

func (self *discordStreamPreview) run() {
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

func (self *discordStreamPreview) flush() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	text := self.accumulated.String()
	if text == self.lastSentText || text == "" || self.stopped {
		return
	}
	if len(text) > maxDiscordMessageLen {
		text = text[:maxDiscordMessageLen]
	}
	self.lastSentText = text

	if self.messageId == "" {
		sent, err := self.session.ChannelMessageSend(self.channelId, text)
		if err != nil {
			return
		}
		self.messageId = sent.ID
	} else {
		self.session.ChannelMessageEdit(self.channelId, self.messageId, text)
	}
}

// Update appends a text delta to the accumulated buffer.
func (self *discordStreamPreview) Update(delta string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.stopped {
		return
	}
	self.accumulated.WriteString(delta)
}

// Reset clears the buffer for the next LLM round (after a tool call).
func (self *discordStreamPreview) Reset() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.accumulated.Reset()
	self.lastSentText = ""
}

// Stop shuts down the background goroutine, performs a final flush, and returns
// the preview message ID and accumulated text.
func (self *discordStreamPreview) Stop() (string, string) {
	close(self.done)
	self.flush()
	self.mutex.Lock()
	self.stopped = true
	messageId := self.messageId
	text := self.accumulated.String()
	self.mutex.Unlock()
	return messageId, text
}

// Delete removes the preview message from the channel if one was sent.
func (self *discordStreamPreview) Delete() {
	self.mutex.Lock()
	messageId := self.messageId
	self.messageId = ""
	self.mutex.Unlock()
	if messageId != "" {
		self.session.ChannelMessageDelete(self.channelId, messageId)
	}
}

// discordSubscribedRun tracks streaming state for a run received via Subscriber events.
type discordSubscribedRun struct {
	preview         *discordStreamPreview
	channelId       string
	origin          string
	originSessionId string
	triggerText     string
	pendingMedia    []*mimetypes.MediaContent
	mediaMutex      sync.Mutex
}

// Bot manages a Discord bot that forwards messages to the agents.
type Bot struct {
	token     string
	ctx       context.Context
	gateway   gw.Gateway
	discord   *discordgo.Session
	botUserId string

	// Per-channel model overrides (channelId -> model name).
	modelMutex     sync.RWMutex
	modelOverrides map[string]string

	// Runs initiated by the bot — skip these in OnEvent.
	activeConversationsMutex sync.RWMutex
	activeConversations      map[string]struct{} // conversationId -> present

	// Subscriber-driven streaming state.
	subscribedRunsMutex sync.Mutex
	subscribedRuns      map[string]*discordSubscribedRun // runId -> state
	userChannelsMutex   sync.RWMutex
	userChannels        map[string]string // userId -> channelId
}

// New creates a new Discord bot that dynamically resolves the default agent and conversation from the gateway.
func New(token string, ctx context.Context, gateway gw.Gateway) *Bot {
	return &Bot{
		token:               token,
		ctx:                 ctx,
		gateway:             gateway,
		modelOverrides:      make(map[string]string),
		activeConversations: make(map[string]struct{}),
		subscribedRuns:      make(map[string]*discordSubscribedRun),
		userChannels:        make(map[string]string),
	}
}

// Start connects the bot to Discord.
func (self *Bot) Start() error {
	discordSession, err := discordgo.New("Bot " + self.token)
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
	self.gateway.Subscribe(self)
	log.Infof("discord bot connected as %s (%s)", discordSession.State.User.Username, self.botUserId)
	return nil
}

// Stop disconnects the bot.
func (self *Bot) Stop() {
	self.gateway.Unsubscribe(self)
	if self.discord != nil {
		self.discord.Close()
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
	defaultConversationId := self.gateway.EnsureDefaultConversation(userId, defaultAgentId)
	if defaultConversationId == "" || conversationId == "" {
		return false
	}
	return conversationId == defaultConversationId
}

// OnEvent implements gw.Subscriber. It handles conversation events for runs
// not initiated by this bot (e.g. scheduled jobs), streaming them to the appropriate channel.
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

	userId, _ := payloadMap["userId"].(string)
	channelId := self.channelIdForUser(userId)
	if channelId == "" {
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
			// Scheduled/automated runs: stream live into Discord.
			if triggerText != "" {
				self.discord.ChannelMessageSend(channelId, "> "+strings.ReplaceAll(triggerText, "\n", "\n> "))
			}
			preview := newDiscordStreamPreview(self.discord, channelId)
			self.subscribedRunsMutex.Lock()
			self.subscribedRuns[runId] = &discordSubscribedRun{
				preview:     preview,
				channelId:   channelId,
				origin:      origin,
				triggerText: triggerText,
			}
			self.subscribedRunsMutex.Unlock()
			self.discord.ChannelTyping(channelId)
			return
		}

		if origin != "webui" {
			return
		}

		originSessionId, _ := payloadMap["originSessionId"].(string)
		if !self.shouldForwardDisconnectedSession(userId, agentId, conversationId, originSessionId) {
			return
		}

		// Session runs are only delivered to Discord if the originating web session disconnects.
		self.subscribedRunsMutex.Lock()
		self.subscribedRuns[runId] = &discordSubscribedRun{
			channelId:       channelId,
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
			self.discord.ChannelTyping(subscribedRun.channelId)
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

		// Send collected media as file attachments.
		for index, mediaContent := range subscribedRun.pendingMedia {
			rawData, decodeError := base64.StdEncoding.DecodeString(mediaContent.Base64)
			if decodeError != nil {
				continue
			}
			filename := fmt.Sprintf("image_%d.%s", index+1, mediaContent.Format)
			self.discord.ChannelMessageSendComplex(subscribedRun.channelId, &discordgo.MessageSend{
				Files: []*discordgo.File{
					{Name: filename, Reader: bytes.NewReader(rawData)},
				},
			})
		}

		// Scheduled/automated runs stream via preview and always post final output.
		if subscribedRun.preview != nil {
			previewMessageId, _ := subscribedRun.preview.Stop()
			if previewMessageId != "" {
				firstChunk := finalText
				remaining := ""
				if len(finalText) > maxDiscordMessageLen {
					cut := strings.LastIndex(finalText[:maxDiscordMessageLen], "\n")
					if cut < maxDiscordMessageLen/2 {
						cut = maxDiscordMessageLen
					}
					firstChunk = finalText[:cut]
					remaining = finalText[cut:]
				}
				self.discord.ChannelMessageEdit(subscribedRun.channelId, previewMessageId, firstChunk)
				if remaining != "" {
					self.sendChunked(subscribedRun.channelId, remaining)
				}
			} else {
				self.sendChunked(subscribedRun.channelId, finalText)
			}
			return
		}

		// Session fallback: only notify when the originating web session is disconnected.
		if subscribedRun.origin == "webui" && !self.gateway.IsSessionConnected(subscribedRun.originSessionId) {
			self.sendChunked(subscribedRun.channelId, finalText)
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
			self.discord.ChannelMessageSend(subscribedRun.channelId, "Sorry, an error occurred: "+errorText)
		} else if state == "error" && subscribedRun.origin == "webui" && !self.gateway.IsSessionConnected(subscribedRun.originSessionId) {
			errorText, _ := payloadMap["error"].(string)
			if errorText == "" {
				errorText = "An error occurred while processing the request."
			}
			self.discord.ChannelMessageSend(subscribedRun.channelId, "Session run failed: "+errorText)
		}
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

	content := event.Content
	hasAttachments := len(event.Attachments) > 0

	if content == "" && !hasAttachments {
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
		content = strings.ReplaceAll(content, "<@"+self.botUserId+">", "")
		content = strings.ReplaceAll(content, "<@!"+self.botUserId+">", "")
		if content == "" && !hasAttachments {
			return
		}
	}

	// Check for slash commands.
	user := self.linkedUserForDiscordUser(event.Author.ID)
	if user == nil {
		discordSession.ChannelMessageSend(event.ChannelID, unlinkedDiscordMessage(event.Author.ID))
		return
	}
	userId := user.ID
	self.setChannelForUser(userId, event.ChannelID)

	if name, arguments, ok := slashcommands.Parse(content); ok {
		self.handleCommand(user, discordSession, event, name, arguments)
		return
	}

	defaultAgentId := user.GetDefaultAgentID()
	if defaultAgentId == "" {
		discordSession.ChannelMessageSend(event.ChannelID, "No default agent available.")
		return
	}
	conversationId := self.gateway.EnsureDefaultConversation(userId, defaultAgentId)

	// Check if there's already an active run for this conversation.
	if self.gateway.GetActiveRun(conversationId) != "" {
		discordSession.ChannelMessageSend(event.ChannelID, "I'm still working on a previous request. Please wait.")
		return
	}

	// Extract attachments from the message.
	var attachments []map[string]string
	if hasAttachments {
		attachments = self.extractAttachments(event.Attachments)
	}

	go self.handleMessage(user, conversationId, defaultAgentId, event.ChannelID, content, attachments)
}

func unlinkedDiscordMessage(discordUserId string) string {
	return fmt.Sprintf(
		"Your Discord account is not linked to a TeaNode user yet.\n\n"+
			"Link it by editing `%s` and adding:\n"+
			"channelLinks:\n"+
			"  discord:\n"+
			"    \"%s\": \"<userId>\"\n\n"+
			"`<userId>` must exist under `users:` in the same file.\n"+
			"Example:\n"+
			"users:\n"+
			"  user-1:\n"+
			"    username: alice\n"+
			"channelLinks:\n"+
			"  discord:\n"+
			"    \"%s\": \"user-1\"",
		"security.yaml",
		discordUserId,
		discordUserId,
	)
}

func (self *Bot) handleMessage(user *models.User, conversationId, agentId, channelId, message string, attachments []map[string]string) {
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

	// Send typing indicator.
	self.discord.ChannelTyping(channelId)

	var pendingMedia []*mimetypes.MediaContent
	var pendingMediaMutex sync.Mutex

	preview := newDiscordStreamPreview(self.discord, channelId)

	// Caller-specific callbacks: only preview/typing/media logic.
	callerCallbacks := &runners.RunCallbacks{
		OnTextDelta: func(text string) {
			preview.Update(text)
		},
		OnToolCall: func(toolName string, arguments string) {
			preview.Reset()
			// Re-send typing indicator after tool calls.
			self.discord.ChannelTyping(channelId)
		},
		OnToolResult: func(toolName string, result string) {
			// Collect media for sending as attachments.
			if detected := mimetypes.DetectMedia(result); detected != nil && detected.Base64 != "" && mimetypes.IsImageFormat(detected.Format) {
				pendingMediaMutex.Lock()
				pendingMedia = append(pendingMedia, detected)
				pendingMediaMutex.Unlock()
			}
		},
	}

	runContext := models.ContextWithUserSessionToken(self.ctx, user, nil, nil)
	handle := self.gateway.SendMessage(runContext, gw.SendMessageParameters{
		AgentID:        agentId,
		ConversationID: conversationId,
		Message:        message,
		Model:          self.getModel(channelId),
		Origin:         "discord",
		Attachments:    attachments,
	}, callerCallbacks)

	// Wait for completion.
	<-handle.Done

	previewMessageId, _ := preview.Stop()
	outcome := handle.Outcome()

	// Handle error: delete preview, send error message.
	if outcome.Error != nil {
		log.Errorf("discord agent run error (conversation %s): %v", handle.ConversationID, outcome.Error)
		preview.Delete()
		self.discord.ChannelMessageSend(channelId, "Sorry, an error occurred while processing your request.")
		return
	}

	// Send collected media as file attachments.
	for index, mediaContent := range pendingMedia {
		rawData, decodeError := base64.StdEncoding.DecodeString(mediaContent.Base64)
		if decodeError != nil {
			continue
		}
		filename := fmt.Sprintf("image_%d.%s", index+1, mediaContent.Format)
		self.discord.ChannelMessageSendComplex(channelId, &discordgo.MessageSend{
			Files: []*discordgo.File{
				{Name: filename, Reader: bytes.NewReader(rawData)},
			},
		})
	}

	// Reuse the preview message as the final message by editing it.
	if previewMessageId != "" {
		finalText := outcome.Response
		firstChunk := finalText
		remaining := ""
		if len(finalText) > maxDiscordMessageLen {
			cut := strings.LastIndex(finalText[:maxDiscordMessageLen], "\n")
			if cut < maxDiscordMessageLen/2 {
				cut = maxDiscordMessageLen
			}
			firstChunk = finalText[:cut]
			remaining = finalText[cut:]
		}
		self.discord.ChannelMessageEdit(channelId, previewMessageId, firstChunk)
		if remaining != "" {
			self.sendChunked(channelId, remaining)
		}
		return
	}

	// No preview message was created — send as new message(s).
	self.sendChunked(channelId, outcome.Response)
}

func (self *Bot) getModel(channelId string) string {
	self.modelMutex.RLock()
	defer self.modelMutex.RUnlock()
	return self.modelOverrides[channelId]
}

func (self *Bot) handleCommand(user *models.User, discordSession *discordgo.Session, messageEvent *discordgo.MessageCreate, name, arguments string) {
	channelId := messageEvent.ChannelID
	var reply string

	defaultAgentId := user.GetDefaultAgentID()
	if defaultAgentId == "" {
		discordSession.ChannelMessageSend(channelId, "No default agent available.")
		return
	}
	switch name {
	case "new":
		conversationId := self.gateway.NewDefaultConversation(user.ID, defaultAgentId, "")
		reply = fmt.Sprintf("New conversation started. (`%s`)", conversationId)

	case "reset", "clear":
		conversationId := self.gateway.EnsureDefaultConversation(user.ID, defaultAgentId)
		// Abort active run if any.
		if activeRunId := self.gateway.GetActiveRun(conversationId); activeRunId != "" {
			self.gateway.AbortRun(activeRunId)
		}
		newConversationId := self.gateway.NewDefaultConversation(user.ID, defaultAgentId, "")
		reply = fmt.Sprintf("Conversation cleared. New conversation started. (`%s`)", newConversationId)

	case "stop":
		conversationId := self.gateway.EnsureDefaultConversation(user.ID, defaultAgentId)
		if activeRunId := self.gateway.GetActiveRun(conversationId); activeRunId != "" {
			self.gateway.AbortRun(activeRunId)
			reply = "Run cancelled."
		} else {
			reply = "No active run to cancel."
		}

	case "model":
		if arguments == "" {
			model := self.getModel(channelId)
			if model == "" {
				model = self.resolveDefaultModel()
			}
			reply = fmt.Sprintf("Current model: `%s`", model)
		} else {
			self.modelMutex.Lock()
			self.modelOverrides[channelId] = arguments
			self.modelMutex.Unlock()
			reply = fmt.Sprintf("Model set to `%s`.", arguments)
		}

	case "agent":
		if arguments == "" {
			var lines []string
			lines = append(lines, fmt.Sprintf("Default agent: `%s`", defaultAgentId))
			lines = append(lines, "Agents:")
			for _, agentId := range self.listAgentIDsFromStore() {
				marker := "  "
				if agentId == defaultAgentId {
					marker = "* "
				}
				lines = append(lines, marker+"`"+agentId+"`")
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
				self.gateway.Broadcast(gw.EventTypeDefaultAgent, map[string]interface{}{
					"defaultAgentId": arguments,
					"userId":         user.ID,
				})
				newConversationId := self.gateway.EnsureDefaultConversation(user.ID, arguments)
				reply = fmt.Sprintf("Switched to agent `%s`. (conversation: `%s`)", arguments, newConversationId)
			}
		}

	case "status":
		conversationId := self.gateway.EnsureDefaultConversation(user.ID, defaultAgentId)
		model := self.getModel(channelId)
		if model == "" {
			model = self.resolveDefaultModel()
		}
		running := self.gateway.GetActiveRun(conversationId) != ""
		status := "idle"
		if running {
			status = "running"
		}
		providerName := self.gateway.ProviderRegistry().DefaultProvider()
		reply = fmt.Sprintf("Agent: `%s`\nConversation: `%s`\nModel: `%s`\nProvider: `%s`\nStatus: %s", defaultAgentId, conversationId, model, providerName, status)

	case "compact":
		conversationId := self.gateway.EnsureDefaultConversation(user.ID, defaultAgentId)
		compactContext := models.ContextWithUserSessionToken(self.ctx, user, nil, nil)
		result, err := self.gateway.Coordinator().CompactConversation(compactContext, defaultAgentId, conversationId)
		if err != nil {
			reply = fmt.Sprintf("Error compacting: %v", err)
		} else {
			reply = fmt.Sprintf("Conversation compacted. Summarized %d messages.", result.SummarizedMessages)
		}

	case "restart":
		discordSession.ChannelMessageSend(channelId, "Restarting gateway...")
		lifecycle.LifecycleFromContext(self.ctx).RequestLifecycle(lifecycle.Restart)
		return

	case "terminate":
		discordSession.ChannelMessageSend(channelId, "Shutting down gateway...")
		lifecycle.LifecycleFromContext(self.ctx).RequestLifecycle(lifecycle.Shutdown)
		return

	case "help":
		reply = slashcommands.HelpText()
	}

	if reply != "" {
		discordSession.ChannelMessageSend(channelId, reply)
	}
}

func (self *Bot) sendChunked(channelId, text string) {
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
		if _, err := self.discord.ChannelMessageSend(channelId, chunk); err != nil {
			log.Errorf("discord send error: %v", err)
			return
		}
		text = text[len(chunk):]
	}
}

func (self *Bot) linkedUserForDiscordUser(discordUserId string) *models.User {
	var user *models.User
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		foundUser, err := transaction.GetUserByDiscordUserID(ctx, discordUserId, nil)
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

func (self *Bot) listAgentIDsFromStore() []string {
	agentIDs := make([]string, 0)
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		agents, listError := transaction.ListAgents(ctx, nil)
		if listError != nil {
			return nil
		}
		agentIDs = make([]string, 0, len(agents))
		for _, agent := range agents {
			agentIDs = append(agentIDs, agent.ID)
		}
		sort.Strings(agentIDs)
		return nil
	})
	return agentIDs
}

func (self *Bot) setChannelForUser(userId, channelId string) {
	if userId == "" || channelId == "" {
		return
	}
	self.userChannelsMutex.Lock()
	self.userChannels[userId] = channelId
	self.userChannelsMutex.Unlock()
}

func (self *Bot) channelIdForUser(userId string) string {
	self.userChannelsMutex.RLock()
	defer self.userChannelsMutex.RUnlock()
	return self.userChannels[userId]
}

// extractAttachments downloads files attached to a Discord message and saves
// them through the configured store, returning conversation attachment references.
func (self *Bot) extractAttachments(messageAttachments []*discordgo.MessageAttachment) []map[string]string {
	var attachments []map[string]string
	for _, att := range messageAttachments {
		data, err := downloadUrl(att.URL)
		if err != nil {
			log.Errorf("failed to download discord attachment %s: %v", att.Filename, err)
			continue
		}

		// Determine format from filename extension, fall back to content type.
		format := strings.TrimPrefix(filepath.Ext(att.Filename), ".")
		if format == "" {
			format = mimetypes.FormatFromMIMEType(att.ContentType)
		}
		if format == "" {
			format = "bin"
		}

		contentType := att.ContentType
		if contentType == "" {
			contentType = mimetypes.MIMETypeFromFormat(format)
		}
		var createdMedia *models.Media
		createError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
			var saveError error
			createdMedia, saveError = transaction.CreateMedia(ctx, bytes.NewReader(data), &models.Media{
				Format:       &format,
				ContentType:  &contentType,
				Source:       ptrto.Value("discord"),
				OriginalName: &att.Filename,
			}, nil)
			return saveError
		})
		if createError != nil {
			log.Errorf("failed to save discord attachment: %v", createError)
			continue
		}
		attachments = append(attachments, map[string]string{
			"mediaId":  createdMedia.ID,
			"format":   format,
			"filename": att.Filename,
		})
	}
	return attachments
}

// downloadUrl fetches data from a URL.
func downloadUrl(url string) ([]byte, error) {
	response, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("downloading: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d", response.StatusCode)
	}
	return io.ReadAll(response.Body)
}
