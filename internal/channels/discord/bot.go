package discord

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/util/deferutil"
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
	pendingMedia    []*media.MediaContent
	mediaMutex      sync.Mutex
}

// Bot manages a Discord bot that forwards messages to the agents.
type Bot struct {
	config        *configs.DiscordConfig
	agentRegistry *agents.AgentRegistry
	gateway       gw.Gateway
	discord       *discordgo.Session
	botUserId     string

	// Per-channel model overrides (channelId -> model name).
	modelMutex     sync.RWMutex
	modelOverrides map[string]string

	// Runs initiated by the bot — skip these in OnEvent.
	activeConversationsMutex sync.RWMutex
	activeConversations      map[string]struct{} // conversationId -> present

	// Subscriber-driven streaming state.
	subscribedRunsMutex sync.Mutex
	subscribedRuns      map[string]*discordSubscribedRun // runId -> state
}

// New creates a new Discord bot that dynamically resolves the default agent and conversation from the registry.
func New(discordConfig *configs.DiscordConfig, agentRegistry *agents.AgentRegistry, gateway gw.Gateway) *Bot {
	return &Bot{
		config:              discordConfig,
		agentRegistry:       agentRegistry,
		gateway:             gateway,
		modelOverrides:      make(map[string]string),
		activeConversations: make(map[string]struct{}),
		subscribedRuns:      make(map[string]*discordSubscribedRun),
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

	// Use the single persisted channel ID.
	channelId := self.agentRegistry.DiscordChannelID()
	if channelId == "" {
		return
	}

	switch state {
	case "user_message":
		// Only forward events for the currently active agent.
		agentId, _ := payloadMap["agentId"].(string)
		if agentId != self.agentRegistry.DefaultID() {
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
		if originSessionId == "" {
			return
		}

		// WebUI runs are only delivered to Discord if the originating web session disconnects.
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

		// WebUI fallback: only notify when the originating web session is disconnected.
		if subscribedRun.origin == "webui" && !self.gateway.IsSessionConnected(subscribedRun.originSessionId) {
			if subscribedRun.triggerText != "" {
				self.discord.ChannelMessageSend(subscribedRun.channelId, "WebUI response ready:\n> "+strings.ReplaceAll(subscribedRun.triggerText, "\n", "\n> "))
			}
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
			self.discord.ChannelMessageSend(subscribedRun.channelId, "WebUI run failed: "+errorText)
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

	content := strings.TrimSpace(event.Content)
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
		content = strings.TrimSpace(strings.ReplaceAll(content, "<@"+self.botUserId+">", ""))
		content = strings.TrimSpace(strings.ReplaceAll(content, "<@!"+self.botUserId+">", ""))
		if content == "" && !hasAttachments {
			return
		}
	}

	// Check allowed users.
	if !self.isUserAllowed(event.Author.ID) {
		return
	}

	// Check for slash commands.
	if name, arguments, ok := slashcommands.Parse(content); ok {
		self.handleCommand(discordSession, event, name, arguments)
		return
	}

	defaultAgentId := self.agentRegistry.DefaultID()
	runner := self.agentRegistry.Get(defaultAgentId)
	if runner == nil {
		discordSession.ChannelMessageSend(event.ChannelID, "No default agent available.")
		return
	}
	conversationId := self.agentRegistry.DefaultConversationID(defaultAgentId)

	// Check if there's already an active run for this conversation.
	if self.gateway.GetActiveRun(conversationId) != "" {
		discordSession.ChannelMessageSend(event.ChannelID, "I'm still working on a previous request. Please wait.")
		return
	}

	// Extract attachments from the message.
	var attachments []conversations.Attachment
	if hasAttachments {
		attachments = self.extractAttachments(event.Attachments)
	}

	go self.handleMessage(conversationId, defaultAgentId, event.ChannelID, content, attachments)
}

func (self *Bot) handleMessage(conversationId, agentId, channelId, message string, attachments []conversations.Attachment) {
	defer deferutil.Recover()

	// Persist channel ID for subscriber-driven routing (e.g. scheduled jobs).
	self.agentRegistry.SetDiscordChannelID(channelId)

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

	var pendingMedia []*media.MediaContent
	var pendingMediaMutex sync.Mutex

	preview := newDiscordStreamPreview(self.discord, channelId)

	// Caller-specific callbacks: only preview/typing/media logic.
	callerCallbacks := &agents.RunCallbacks{
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

func (self *Bot) handleCommand(discordSession *discordgo.Session, messageEvent *discordgo.MessageCreate, name, arguments string) {
	channelId := messageEvent.ChannelID
	var reply string

	defaultAgentId := self.agentRegistry.DefaultID()
	runner := self.agentRegistry.Get(defaultAgentId)

	switch name {
	case "new":
		conversationId := self.gateway.NewConversation(defaultAgentId, "")
		reply = fmt.Sprintf("New conversation started. (`%s`)", conversationId)

	case "reset", "clear":
		conversationId := self.agentRegistry.DefaultConversationID(defaultAgentId)
		// Abort active run if any.
		if activeRunId := self.gateway.GetActiveRun(conversationId); activeRunId != "" {
			self.gateway.AbortRun(activeRunId)
		}
		if err := self.gateway.DeleteConversation(defaultAgentId, conversationId); err != nil {
			reply = fmt.Sprintf("Error clearing conversation: %v", err)
		} else {
			newConversationId := self.gateway.NewConversation(defaultAgentId, "")
			reply = fmt.Sprintf("Conversation cleared. New conversation started. (`%s`)", newConversationId)
		}

	case "stop":
		conversationId := self.agentRegistry.DefaultConversationID(defaultAgentId)
		if activeRunId := self.gateway.GetActiveRun(conversationId); activeRunId != "" {
			self.gateway.AbortRun(activeRunId)
			reply = "Run cancelled."
		} else {
			reply = "No active run to cancel."
		}

	case "model":
		if arguments == "" {
			model := self.getModel(channelId)
			if model == "" && runner != nil {
				model = runner.Config.Models.Default
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
			for _, agentId := range self.agentRegistry.AgentIDs() {
				marker := "  "
				if agentId == defaultAgentId {
					marker = "* "
				}
				lines = append(lines, marker+"`"+agentId+"`")
			}
			reply = strings.Join(lines, "\n")
		} else {
			if err := self.gateway.SetDefaultAgent(arguments); err != nil {
				reply = fmt.Sprintf("Error: %v", err)
			} else {
				newConversationId := self.agentRegistry.DefaultConversationID(arguments)
				reply = fmt.Sprintf("Switched to agent `%s`. (conversation: `%s`)", arguments, newConversationId)
			}
		}

	case "status":
		conversationId := self.agentRegistry.DefaultConversationID(defaultAgentId)
		model := self.getModel(channelId)
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
		reply = fmt.Sprintf("Agent: `%s`\nConversation: `%s`\nModel: `%s`\nProvider: `%s`\nStatus: %s", defaultAgentId, conversationId, model, providerName, status)

	case "compact":
		conversationId := self.agentRegistry.DefaultConversationID(defaultAgentId)
		if runner != nil {
			result, err := runner.CompactConversation(context.Background(), conversationId)
			if err != nil {
				reply = fmt.Sprintf("Error compacting: %v", err)
			} else {
				reply = fmt.Sprintf("Conversation compacted. Summarized %d messages.", result.SummarizedMessages)
			}
		} else {
			reply = "No default agent available."
		}

	case "restart":
		discordSession.ChannelMessageSend(channelId, "Restarting gateway...")
		self.gateway.RequestLifecycle(gw.LifecycleRestart)
		return

	case "terminate":
		discordSession.ChannelMessageSend(channelId, "Shutting down gateway...")
		self.gateway.RequestLifecycle(gw.LifecycleShutdown)
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

func (self *Bot) isUserAllowed(userId string) bool {
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

// extractAttachments downloads files attached to a Discord message and saves
// them to the media store, returning conversation attachment references.
func (self *Bot) extractAttachments(messageAttachments []*discordgo.MessageAttachment) []conversations.Attachment {
	mediaStore := self.gateway.MediaStore()
	if mediaStore == nil {
		return nil
	}

	var attachments []conversations.Attachment
	for _, att := range messageAttachments {
		data, err := downloadURL(att.URL)
		if err != nil {
			log.Errorf("failed to download discord attachment %s: %v", att.Filename, err)
			continue
		}

		// Determine format from filename extension, fall back to content type.
		format := strings.TrimPrefix(filepath.Ext(att.Filename), ".")
		if format == "" {
			format = media.FormatFromMimeType(att.ContentType)
		}
		if format == "" {
			format = "bin"
		}

		saved, err := mediaStore.Save(data, format, media.SaveOptions{
			SourceType:   "discord",
			OriginalName: att.Filename,
		})
		if err != nil {
			log.Errorf("failed to save discord attachment: %v", err)
			continue
		}
		attachments = append(attachments, conversations.Attachment{
			MediaID:  saved.MediaID,
			Format:   format,
			Filename: att.Filename,
		})
	}
	return attachments
}

// downloadURL fetches data from a URL.
func downloadURL(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
