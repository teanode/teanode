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
	"github.com/teanode/teanode/internal/channels"
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

const maxDiscordMessageLength = 2000

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
	defer deferutil.Recover()
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
	if len(text) > maxDiscordMessageLength {
		text = text[:maxDiscordMessageLength]
	}
	self.lastSentText = text

	if self.messageId == "" {
		sent, err := self.session.ChannelMessageSend(self.channelId, text)
		if err != nil {
			return
		}
		self.messageId = sent.ID
	} else {
		_, _ = self.session.ChannelMessageEdit(self.channelId, self.messageId, text)
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
		_ = self.session.ChannelMessageDelete(self.channelId, messageId)
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
	token          string
	ctx            context.Context
	coordinator    *coordinators.Coordinator
	pubsub         *pubsub.PubSub
	sessionTracker *sessiontracker.SessionTracker
	discord        *discordgo.Session
	botUserId      string

	// Runs initiated by the bot — skip these in OnEvent.
	activeConversationsMutex sync.RWMutex
	activeConversations      map[string]struct{} // conversationId -> present

	// Subscriber-driven streaming state.
	subscribedRunsMutex sync.Mutex
	subscribedRuns      map[string]*discordSubscribedRun // runId -> state
	userChannelsMutex   sync.RWMutex
	userChannels        map[string]string // userId -> channelId
}

// New creates a new Discord bot that dynamically resolves the default agent and conversation from the coordinator.
func New(token string, ctx context.Context, coordinator *coordinators.Coordinator, events *pubsub.PubSub, sessions *sessiontracker.SessionTracker) *Bot {
	return &Bot{
		token:               token,
		ctx:                 ctx,
		coordinator:         coordinator,
		pubsub:              events,
		sessionTracker:      sessions,
		activeConversations: make(map[string]struct{}),
		subscribedRuns:      make(map[string]*discordSubscribedRun),
		userChannels:        make(map[string]string),
	}
}

// Start connects the bot to Discord.
func (self *Bot) Start() error {
	discordSession, err := discordgo.New("Bot " + self.token)
	if err != nil {
		return fmt.Errorf("discord: creating discord session: %w", err)
	}

	discordSession.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent
	discordSession.AddHandler(self.onMessageCreate)

	if err := discordSession.Open(); err != nil {
		return fmt.Errorf("discord: opening discord connection: %w", err)
	}

	self.discord = discordSession
	self.botUserId = discordSession.State.User.ID
	self.pubsub.Subscribe(self)
	log.Infof("discord bot connected as %s (%s)", discordSession.State.User.Username, self.botUserId)
	return nil
}

// Stop disconnects the bot.
func (self *Bot) Stop() {
	self.pubsub.Unsubscribe(self)
	if self.discord != nil {
		_ = self.discord.Close()
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
// not initiated by this bot (e.g. scheduled jobs), streaming them to the appropriate channel.
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
				_, _ = self.discord.ChannelMessageSend(channelId, "> "+strings.ReplaceAll(triggerText, "\n", "\n> "))
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
			_ = self.discord.ChannelTyping(channelId)
			return
		}

		if origin != "web" {
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
			_ = self.discord.ChannelTyping(subscribedRun.channelId)
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

		rawText, _ := payloadMap["text"].(string)
		finalText := channels.StripSuggestedReplies(rawText)

		// Send collected media as file attachments.
		for index, mediaContent := range subscribedRun.pendingMedia {
			rawData, decodeError := base64.StdEncoding.DecodeString(mediaContent.Base64)
			if decodeError != nil {
				continue
			}
			filename := fmt.Sprintf("image_%d.%s", index+1, mediaContent.Format)
			_, _ = self.discord.ChannelMessageSendComplex(subscribedRun.channelId, &discordgo.MessageSend{
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
				if len(finalText) > maxDiscordMessageLength {
					cut := strings.LastIndex(finalText[:maxDiscordMessageLength], "\n")
					if cut < maxDiscordMessageLength/2 {
						cut = maxDiscordMessageLength
					}
					firstChunk = finalText[:cut]
					remaining = finalText[cut:]
				}
				_, _ = self.discord.ChannelMessageEdit(subscribedRun.channelId, previewMessageId, firstChunk)
				if remaining != "" {
					self.sendChunked(subscribedRun.channelId, remaining)
				}
			} else {
				self.sendChunked(subscribedRun.channelId, finalText)
			}
			return
		}

		// Session fallback: only notify when the originating web session is disconnected.
		if subscribedRun.origin == "web" && !self.sessionTracker.IsConnected(subscribedRun.originSessionId) {
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
			_, _ = self.discord.ChannelMessageSend(subscribedRun.channelId, "Sorry, an error occurred: "+errorText)
		} else if state == "error" && subscribedRun.origin == "web" && !self.sessionTracker.IsConnected(subscribedRun.originSessionId) {
			errorText, _ := payloadMap["error"].(string)
			if errorText == "" {
				errorText = "An error occurred while processing the request."
			}
			_, _ = self.discord.ChannelMessageSend(subscribedRun.channelId, "Session run failed: "+errorText)
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
		_, _ = discordSession.ChannelMessageSend(event.ChannelID, unlinkedDiscordMessage(event.Author.ID))
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
		_, _ = discordSession.ChannelMessageSend(event.ChannelID, "No default agent available.")
		return
	}
	conversationId := self.coordinator.EnsureDefaultConversation(userId, defaultAgentId)

	// Check if there's already an active run for this conversation.
	if self.coordinator.GetActiveConversationRunner(conversationId) != nil {
		_, _ = discordSession.ChannelMessageSend(event.ChannelID, "I'm still working on a previous request. Please wait.")
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
	_ = self.discord.ChannelTyping(channelId)

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
			_ = self.discord.ChannelTyping(channelId)
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
	handle, sendError := self.coordinator.Run(runContext, coordinators.RunParameters{
		AgentID:        agentId,
		ConversationID: conversationId,
		Message:        message,
		Origin:         runners.OriginChannel,
		Attachments:    attachments,
	}, callerCallbacks)
	if sendError != nil {
		log.Errorf("discord send message error (conversation %s): %v", conversationId, sendError)
		preview.Stop()
		preview.Delete()
		_, _ = self.discord.ChannelMessageSend(channelId, "Sorry, an error occurred while processing your request.")
		return
	}

	// Wait for completion.
	result, runError := handle.Wait()

	previewMessageId, _ := preview.Stop()

	// Handle error: delete preview, send error message.
	if runError != nil {
		log.Errorf("discord agent run error (conversation %s): %v", handle.ConversationID, runError)
		preview.Delete()
		_, _ = self.discord.ChannelMessageSend(channelId, "Sorry, an error occurred while processing your request.")
		return
	}

	// Send collected media as file attachments.
	for index, mediaContent := range pendingMedia {
		rawData, decodeError := base64.StdEncoding.DecodeString(mediaContent.Base64)
		if decodeError != nil {
			continue
		}
		filename := fmt.Sprintf("image_%d.%s", index+1, mediaContent.Format)
		_, _ = self.discord.ChannelMessageSendComplex(channelId, &discordgo.MessageSend{
			Files: []*discordgo.File{
				{Name: filename, Reader: bytes.NewReader(rawData)},
			},
		})
	}

	// Reuse the preview message as the final message by editing it.
	if previewMessageId != "" {
		finalText := channels.StripSuggestedReplies(result.Response)
		firstChunk := finalText
		remaining := ""
		if len(finalText) > maxDiscordMessageLength {
			cut := strings.LastIndex(finalText[:maxDiscordMessageLength], "\n")
			if cut < maxDiscordMessageLength/2 {
				cut = maxDiscordMessageLength
			}
			firstChunk = finalText[:cut]
			remaining = finalText[cut:]
		}
		_, _ = self.discord.ChannelMessageEdit(channelId, previewMessageId, firstChunk)
		if remaining != "" {
			self.sendChunked(channelId, remaining)
		}
		return
	}

	// No preview message was created -- send as new message(s).
	self.sendChunked(channelId, channels.StripSuggestedReplies(result.Response))
}

func (self *Bot) handleCommand(user *models.User, discordSession *discordgo.Session, messageEvent *discordgo.MessageCreate, name, arguments string) {
	channelId := messageEvent.ChannelID
	var reply string

	defaultAgentId := user.GetDefaultAgentID()
	if defaultAgentId == "" {
		_, _ = discordSession.ChannelMessageSend(channelId, "No default agent available.")
		return
	}
	switch name {
	case "new":
		conversationId := self.coordinator.NewDefaultConversation(user.ID, defaultAgentId)
		reply = fmt.Sprintf("New conversation started. (`%s`)", conversationId)

	case "reset", "clear":
		conversationId := self.coordinator.EnsureDefaultConversation(user.ID, defaultAgentId)
		// Abort active run if any.
		self.coordinator.AbortConversationRun(conversationId)
		newConversationId := self.coordinator.NewDefaultConversation(user.ID, defaultAgentId)
		reply = fmt.Sprintf("Conversation cleared. New conversation started. (`%s`)", newConversationId)

	case "stop":
		conversationId := self.coordinator.EnsureDefaultConversation(user.ID, defaultAgentId)
		if self.coordinator.GetActiveConversationRunner(conversationId) != nil {
			self.coordinator.AbortConversationRun(conversationId)
			reply = "Run cancelled."
		} else {
			reply = "No active run to cancel."
		}

	case "agent":
		if arguments == "" {
			var lines []string
			lines = append(lines, fmt.Sprintf("Default agent: `%s`", defaultAgentId))
			lines = append(lines, "Agents:")
			for _, agentId := range self.listAgentIdsFromStore() {
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
				self.pubsub.Broadcast(pubsub.EventTypeDefaultAgent, map[string]interface{}{
					"defaultAgentId": arguments,
					"userId":         user.ID,
				})
				newConversationId := self.coordinator.EnsureDefaultConversation(user.ID, arguments)
				reply = fmt.Sprintf("Switched to agent `%s`. (conversation: `%s`)", arguments, newConversationId)
			}
		}

	case "status":
		conversationId := self.coordinator.EnsureDefaultConversation(user.ID, defaultAgentId)
		providerModelName := self.resolveDefaultProviderModelName()
		running := self.coordinator.GetActiveConversationRunner(conversationId) != nil
		status := "idle"
		if running {
			status = "running"
		}
		providerName := self.coordinator.ProviderRegistry().DefaultProvider()
		reply = fmt.Sprintf("Agent: `%s`\nConversation: `%s`\nModel: `%s`\nProvider: `%s`\nStatus: %s", defaultAgentId, conversationId, providerModelName, providerName, status)

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
		_, _ = discordSession.ChannelMessageSend(channelId, "Restarting node...")
		lifecycle.LifecycleFromContext(self.ctx).RequestLifecycle(lifecycle.Restart)
		return

	case "terminate":
		_, _ = discordSession.ChannelMessageSend(channelId, "Shutting down node...")
		lifecycle.LifecycleFromContext(self.ctx).RequestLifecycle(lifecycle.Shutdown)
		return

	case "help":
		reply = slashcommands.HelpText()
	}

	if reply != "" {
		_, _ = discordSession.ChannelMessageSend(channelId, reply)
	}
}

func (self *Bot) sendChunked(channelId, text string) {
	if text == "" {
		return
	}
	for len(text) > 0 {
		chunk := text
		if len(chunk) > maxDiscordMessageLength {
			// Try to split at a newline.
			cut := strings.LastIndex(chunk[:maxDiscordMessageLength], "\n")
			if cut < maxDiscordMessageLength/2 {
				cut = maxDiscordMessageLength
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

func (self *Bot) resolveDefaultProviderModelName() string {
	var defaultProviderModelName string
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, getError := transaction.GetConfiguration(ctx, nil)
		if getError != nil {
			return getError
		}
		if configuration.Models != nil {
			defaultProviderModelName = configuration.Models.GetDefault()
		}
		return nil
	})
	return defaultProviderModelName
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
	for _, attachment := range messageAttachments {
		data, err := downloadUrl(attachment.URL)
		if err != nil {
			log.Errorf("failed to download discord attachment %s: %v", attachment.Filename, err)
			continue
		}

		// Determine format from filename extension, fall back to content type.
		format := strings.TrimPrefix(filepath.Ext(attachment.Filename), ".")
		if format == "" {
			format = mimetypes.FormatFromMIMEType(attachment.ContentType)
		}
		if format == "" {
			format = "bin"
		}

		contentType := attachment.ContentType
		if contentType == "" {
			contentType = mimetypes.MIMETypeFromFormat(format)
		}
		var createdMedia *models.Media
		createError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
			var saveError error
			createdMedia, saveError = transaction.CreateMedia(ctx, bytes.NewReader(data), &models.Media{
				Format:       &format,
				ContentType:  &contentType,
				Source:       ptrto.Value(models.MediaSourceDiscord),
				OriginalName: &attachment.Filename,
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
			"filename": attachment.Filename,
		})
	}
	return attachments
}

// downloadUrl fetches data from a URL.
func downloadUrl(targetUrl string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Get(targetUrl)
	if err != nil {
		return nil, fmt.Errorf("discord: downloading: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discord: download returned status %d", response.StatusCode)
	}
	const maxFileSize = 100 * 1024 * 1024 // 100 MB
	return io.ReadAll(io.LimitReader(response.Body, maxFileSize))
}
