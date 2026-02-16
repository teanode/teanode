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

// Bot manages a Discord bot that forwards messages to the agents.
type Bot struct {
	config        *configs.DiscordConfig
	agentRegistry *agents.AgentRegistry
	discord       *discordgo.Session
	botUserId     string

	// Active runs per conversation id — prevents concurrent runs on same conversation.
	activeMutex sync.Mutex
	active      map[string]context.CancelFunc

	// Per-channel model overrides (channelId → model name).
	modelMutex     sync.RWMutex
	modelOverrides map[string]string

	Broadcast       func(event string, payload interface{})
	SetActiveRun    func(conversationId, runId string)
	ClearActiveRun  func(conversationId, runId string)
	SetActiveAgent  func(agentId string) error
	NewConversation func(agentId string) string
}

// New creates a new Discord bot that dynamically resolves the active agent and conversation from the registry.
func New(discordConfig *configs.DiscordConfig, agentRegistry *agents.AgentRegistry) *Bot {
	return &Bot{
		config:         discordConfig,
		agentRegistry:  agentRegistry,
		active:         make(map[string]context.CancelFunc),
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
	if name, arguments, ok := slashcommands.Parse(content); ok {
		self.handleCommand(discordSession, event, name, arguments)
		return
	}

	activeAgentId := self.agentRegistry.ActiveAgentID()
	runner := self.agentRegistry.Get(activeAgentId)
	if runner == nil {
		discordSession.ChannelMessageSend(event.ChannelID, "No active agent available.")
		return
	}
	conversationId := self.agentRegistry.ActiveConversationID(activeAgentId)

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

	go self.handleMessage(ctx, cancel, runner, conversationId, event.ChannelID, content)
}

func (self *Bot) handleMessage(ctx context.Context, cancel context.CancelFunc, runner *agents.Runner, conversationId, channelId, message string) {
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

	result, err := runner.Run(ctx, agents.RunParams{
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

	// Reuse the preview message as the final message by editing it.
	if previewMessageId != "" {
		finalText := result.Response
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
	self.sendChunked(channelId, result.Response)
}

func (self *Bot) getModel(channelId string) string {
	self.modelMutex.RLock()
	defer self.modelMutex.RUnlock()
	return self.modelOverrides[channelId]
}

func (self *Bot) handleCommand(discordSession *discordgo.Session, messageEvent *discordgo.MessageCreate, name, arguments string) {
	channelId := messageEvent.ChannelID
	var reply string

	activeAgentId := self.agentRegistry.ActiveAgentID()
	runner := self.agentRegistry.Get(activeAgentId)

	switch name {
	case "new":
		conversationId := self.newConversation(activeAgentId)
		reply = fmt.Sprintf("New conversation started. (`%s`)", conversationId)

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
				reply = fmt.Sprintf("Conversation cleared. New conversation started. (`%s`)", newConversationId)
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
			lines = append(lines, fmt.Sprintf("Active agent: `%s`", activeAgentId))
			lines = append(lines, "Agents:")
			for _, agentId := range self.agentRegistry.AgentIDs() {
				marker := "  "
				if agentId == activeAgentId {
					marker = "* "
				}
				lines = append(lines, marker+"`"+agentId+"`")
			}
			reply = strings.Join(lines, "\n")
		} else {
			if err := self.setActiveAgent(arguments); err != nil {
				reply = fmt.Sprintf("Error: %v", err)
			} else {
				newConversationId := self.agentRegistry.ActiveConversationID(arguments)
				reply = fmt.Sprintf("Switched to agent `%s`. (conversation: `%s`)", arguments, newConversationId)
			}
		}

	case "status":
		conversationId := self.agentRegistry.ActiveConversationID(activeAgentId)
		model := self.getModel(channelId)
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
		reply = fmt.Sprintf("Agent: `%s`\nConversation: `%s`\nModel: `%s`\nProvider: `%s`\nStatus: %s", activeAgentId, conversationId, model, providerName, status)

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
