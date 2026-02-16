package discord

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/slashcommands"
	"github.com/teanode/teanode/internal/util/ulid"
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
	text := self.accumulated.String()
	if text == self.lastSentText || text == "" || self.stopped {
		self.mutex.Unlock()
		return
	}
	if len(text) > maxDiscordMessageLen {
		text = text[:maxDiscordMessageLen]
	}
	messageId := self.messageId
	self.lastSentText = text
	self.mutex.Unlock()

	if messageId == "" {
		sent, err := self.session.ChannelMessageSend(self.channelId, text)
		if err != nil {
			return
		}
		self.mutex.Lock()
		self.messageId = sent.ID
		self.mutex.Unlock()
	} else {
		self.session.ChannelMessageEdit(self.channelId, messageId, text)
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

// Bot manages a Discord bot that forwards messages to the agents.
type Bot struct {
	config        *configs.DiscordConfig
	runner        *agents.Runner
	conversations *conversations.Store
	discord       *discordgo.Session
	botUserId     string

	// Active runs per conversation id — prevents concurrent runs on same conversation.
	activeMutex sync.Mutex
	active      map[string]context.CancelFunc

	// Per-channel conversation id overrides (channelId → conversation id).
	conversationMutex sync.RWMutex
	conversationIds   map[string]string

	// Per-channel model overrides (channelId → model name).
	modelMutex     sync.RWMutex
	modelOverrides map[string]string

	Broadcast      func(event string, payload interface{})
	SetActiveRun   func(conversationId, runId string)
	ClearActiveRun func(conversationId, runId string)
}

// New creates a new Discord bot. It resolves the runner from the agent registry
// using the config's AgentID (defaults to the configured default agent).
func New(discordConfig *configs.DiscordConfig, agentRegistry *agents.AgentRegistry) (*Bot, error) {
	agentId := discordConfig.AgentID
	if agentId == "" {
		agentId = agentRegistry.DefaultID()
	}
	runner := agentRegistry.Get(agentId)
	if runner == nil {
		runner = agentRegistry.Default()
	}
	if runner == nil {
		return nil, fmt.Errorf("no agent runner available for discord (agent %q)", agentId)
	}
	return &Bot{
		config:          discordConfig,
		runner:          runner,
		conversations:   runner.Conversations,
		active:          make(map[string]context.CancelFunc),
		conversationIds: make(map[string]string),
		modelOverrides:  make(map[string]string),
	}, nil
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
	if name, arguments, ok := slashcommands.Parse(content); ok {
		self.handleCommand(discordSession, event, name, arguments)
		return
	}

	conversationId := self.getConversationId(event.ChannelID)

	// Check if there's already an active run for this conversation.
	self.activeMutex.Lock()
	if _, busy := self.active[conversationId]; busy {
		self.activeMutex.Unlock()
		discordSession.ChannelMessageSend(event.ChannelID, "I'm still working on a previous request. Please wait.")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	self.active[conversationId] = cancel
	self.activeMutex.Unlock()

	go self.handleMessage(ctx, cancel, conversationId, event.ChannelID, content)
}

func (self *Bot) handleMessage(ctx context.Context, cancel context.CancelFunc, conversationId, channelId, message string) {
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

	// Send typing indicator.
	self.discord.ChannelTyping(channelId)

	var pendingMedia []*media.MediaContent
	var pendingMediaMutex sync.Mutex

	preview := newDiscordStreamPreview(self.discord, channelId)
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
			// Re-send typing indicator after tool calls.
			self.discord.ChannelTyping(channelId)
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
			// Collect media for sending as attachments.
			if detected := media.DetectMedia(result); detected != nil && detected.Base64 != "" && media.IsImageFormat(detected.Format) {
				pendingMediaMutex.Lock()
				pendingMedia = append(pendingMedia, detected)
				pendingMediaMutex.Unlock()
			}
		},
	}

	result, err := self.runner.Run(ctx, agents.RunParams{
		ConversationID: conversationId,
		Message:        message,
		Model:          self.getModel(channelId),
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
		log.Errorf("discord agent run error (conversation %s): %v", conversationId, err)
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

	// Try final edit if preview exists and response fits in one message.
	if previewMessageId != "" && len(result.Response) <= maxDiscordMessageLen {
		if _, editError := self.discord.ChannelMessageEdit(channelId, previewMessageId, result.Response); editError == nil {
			return
		}
		// Edit failed — delete and fall through to sendChunked.
		preview.Delete()
	} else if previewMessageId != "" {
		preview.Delete()
	}

	self.sendChunked(channelId, result.Response)
}

func (self *Bot) getConversationId(channelId string) string {
	self.conversationMutex.RLock()
	defer self.conversationMutex.RUnlock()
	if id, ok := self.conversationIds[channelId]; ok {
		return id
	}
	return ulid.GenerateString()
}

func (self *Bot) getModel(channelId string) string {
	self.modelMutex.RLock()
	defer self.modelMutex.RUnlock()
	return self.modelOverrides[channelId]
}

func (self *Bot) handleCommand(discordSession *discordgo.Session, messageEvent *discordgo.MessageCreate, name, arguments string) {
	channelId := messageEvent.ChannelID
	var reply string

	switch name {
	case "new":
		conversationId := ulid.GenerateString()
		self.conversationMutex.Lock()
		self.conversationIds[channelId] = conversationId
		self.conversationMutex.Unlock()
		reply = fmt.Sprintf("New conversation started. (`%s`)", conversationId)

	case "reset":
		conversationId := self.getConversationId(channelId)
		if err := self.conversations.Delete(conversationId); err != nil {
			reply = fmt.Sprintf("Error clearing conversation: %v", err)
		} else {
			reply = "Conversation history cleared."
		}

	case "stop":
		conversationId := self.getConversationId(channelId)
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
			model := self.getModel(channelId)
			if model == "" {
				model = self.runner.Config.Models.Default
			}
			reply = fmt.Sprintf("Current model: `%s`", model)
		} else {
			self.modelMutex.Lock()
			self.modelOverrides[channelId] = arguments
			self.modelMutex.Unlock()
			reply = fmt.Sprintf("Model set to `%s`.", arguments)
		}

	case "status":
		conversationId := self.getConversationId(channelId)
		model := self.getModel(channelId)
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
		reply = fmt.Sprintf("Conversation: `%s`\nModel: `%s`\nProvider: `%s`\nStatus: %s", conversationId, model, self.runner.Config.Models.DefaultProviderName(), status)

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
