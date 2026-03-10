package runners

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/skills"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/security"
)

var realtimeLog = logging.MustGetLogger("realtime-runner")

// RealtimeCallbacks receives events during a Realtime API session.
type RealtimeCallbacks struct {
	// OnSessionReady fires when the Realtime API confirms the session configuration.
	OnSessionReady func()
	// OnError fires on Realtime API errors.
	OnError func(code string, message string)
	// OnInputSpeechStarted fires when the server detects speech input.
	OnInputSpeechStarted func()
	// OnInputSpeechStopped fires when the server detects silence after speech.
	OnInputSpeechStopped func()
	// OnInputTranscript fires when a user speech transcript is available.
	OnInputTranscript func(text string)
	// OnResponseStarted fires when the Realtime API begins generating a response.
	OnResponseStarted func(responseId string)
	// OnAudioDelta fires with raw PCM16 24kHz audio data from the model.
	OnAudioDelta func(pcmData []byte)
	// OnTextDelta fires with incremental text transcript of the audio response.
	OnTextDelta func(text string)
	// OnTextDone fires when the full audio transcript is available.
	OnTextDone func(text string)
	// OnToolCall fires before a tool is executed.
	OnToolCall func(name string, arguments string)
	// OnToolResult fires after a tool is executed with its result.
	OnToolResult func(name string, result string)
	// OnResponseDone fires when the response is complete.
	OnResponseDone func(responseId string)
	// OnSessionEnded fires when the connection is lost or the session ends.
	OnSessionEnded func(reason string)
}

// RealtimeRunner manages a session with the OpenAI Realtime API. It handles
// the event loop, tool execution via ToolRegistry, and streams events to
// callers via RealtimeCallbacks. Unlike the standard Runner which processes
// single request-response cycles, RealtimeRunner manages a long-lived
// bidirectional WebSocket session.
type RealtimeRunner struct {
	ID             string
	AgentID        string
	ConversationID string
	UserID         string

	storeContext context.Context // long-lived context with store for persistence
	toolRegistry *tools.ToolRegistry
	conn         providers.RealtimeConn
	callbacks    *RealtimeCallbacks
	events       *pubsub.PubSub

	closeOnce   sync.Once
	waitGroup   sync.WaitGroup
	doneChannel chan struct{}

	// Tool call accumulation: callId -> accumulated arguments JSON.
	toolCallsMutex sync.Mutex
	toolCalls      map[string]*realtimePendingToolCall

	// Current response tracking.
	currentResponseMutex sync.RWMutex
	currentResponseId    string
	currentRunId         string
	accumulatedText      string
}

type realtimePendingToolCall struct {
	CallID    string
	Name      string
	Arguments string
}

// NewRealtimeRunner creates a runner for an OpenAI Realtime API session.
// The conn must already be dialed. The runner takes ownership of the connection
// and will close it when Close() is called.
func NewRealtimeRunner(
	ctx context.Context,
	agentId string,
	conversationId string,
	userId string,
	conn providers.RealtimeConn,
	agent models.Agent,
	events *pubsub.PubSub,
	callbacks *RealtimeCallbacks,
) *RealtimeRunner {
	toolRegistry := tools.NewToolRegistry()
	skills.RegisterSkills(ctx, toolRegistry, agent.GetSkills())
	toolRegistry.ApplyFilter(agent.GetTools())

	if callbacks == nil {
		callbacks = &RealtimeCallbacks{}
	}

	return &RealtimeRunner{
		ID:             security.NewULID(),
		AgentID:        agentId,
		ConversationID: conversationId,
		UserID:         userId,
		storeContext:   ctx,
		toolRegistry:   toolRegistry,
		conn:           conn,
		callbacks:      callbacks,
		events:         events,
		doneChannel:    make(chan struct{}),
		toolCalls:      make(map[string]*realtimePendingToolCall),
	}
}

// ToolDefinitions returns the tool definitions registered for this runner.
func (self *RealtimeRunner) ToolDefinitions() []providers.ToolDefinition {
	return self.toolRegistry.Definitions()
}

// Start configures the Realtime API session and begins the background event loop.
func (self *RealtimeRunner) Start(instructions string, voice string) error {
	if voice == "" {
		voice = "alloy"
	}

	definitions := self.toolRegistry.Definitions()
	realtimeTools := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		realtimeTools = append(realtimeTools, map[string]any{
			"type":        "function",
			"name":        definition.Function.Name,
			"description": definition.Function.Description,
			"parameters":  definition.Function.Parameters,
		})
	}

	sessionUpdate := map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"modalities":          []string{"text", "audio"},
			"instructions":        instructions,
			"voice":               voice,
			"input_audio_format":  "pcm16",
			"output_audio_format": "pcm16",
			"tools":               realtimeTools,
			"turn_detection": map[string]any{
				"type":                "server_vad",
				"threshold":           0.5,
				"prefix_padding_ms":   300,
				"silence_duration_ms": 500,
				"create_response":     true,
			},
			"input_audio_transcription": map[string]any{
				"model": "whisper-1",
			},
		},
	}
	if err := self.conn.SendJSON(sessionUpdate); err != nil {
		return err
	}

	self.waitGroup.Add(1)
	go func() {
		defer self.waitGroup.Done()
		self.readLoop()
	}()

	realtimeLog.Infof("realtime runner started: runner=%s agent=%s conv=%s tools=%d",
		self.ID, self.AgentID, self.ConversationID, len(definitions))
	return nil
}

// SendAudioBase64 sends base64-encoded PCM16 audio to the Realtime API.
func (self *RealtimeRunner) SendAudioBase64(audioBase64 string) error {
	return self.conn.SendJSON(map[string]any{
		"type":  "input_audio_buffer.append",
		"audio": audioBase64,
	})
}

// CommitInput commits the current audio buffer (for push-to-talk mode)
// and triggers a response.
func (self *RealtimeRunner) CommitInput() error {
	if err := self.conn.SendJSON(map[string]any{
		"type": "input_audio_buffer.commit",
	}); err != nil {
		return err
	}
	return self.conn.SendJSON(map[string]any{
		"type": "response.create",
	})
}

// CancelResponse cancels the current in-progress response.
func (self *RealtimeRunner) CancelResponse() error {
	return self.conn.SendJSON(map[string]any{
		"type": "response.cancel",
	})
}

// SendJSON sends a raw JSON event to the Realtime API.
func (self *RealtimeRunner) SendJSON(event map[string]any) error {
	return self.conn.SendJSON(event)
}

// Close terminates the session and waits for goroutines to exit.
func (self *RealtimeRunner) Close() {
	self.closeOnce.Do(func() {
		realtimeLog.Infof("realtime runner close: runner=%s", self.ID)
		close(self.doneChannel)
		self.conn.Close()
		self.waitGroup.Wait()
	})
}

// Done returns a channel that is closed when the runner exits.
func (self *RealtimeRunner) Done() <-chan struct{} {
	return self.doneChannel
}

// readLoop reads events from the OpenAI Realtime API and dispatches them.
func (self *RealtimeRunner) readLoop() {
	for {
		select {
		case <-self.doneChannel:
			return
		default:
		}

		var event providers.RealtimeEvent
		if err := self.conn.ReadJSON(&event); err != nil {
			select {
			case <-self.doneChannel:
				return
			default:
				realtimeLog.Warningf("realtime read error: runner=%s err=%v", self.ID, err)
				if self.callbacks.OnSessionEnded != nil {
					self.callbacks.OnSessionEnded("connectionError")
				}
				return
			}
		}

		self.handleEvent(event)
	}
}

func (self *RealtimeRunner) handleEvent(event providers.RealtimeEvent) {
	switch event.Type {
	case "session.created", "session.updated":
		realtimeLog.Infof("realtime %s: runner=%s", event.Type, self.ID)
		if self.callbacks.OnSessionReady != nil {
			self.callbacks.OnSessionReady()
		}

	case "error":
		message := "unknown error"
		code := ""
		if event.Error != nil {
			message = event.Error.Message
			code = event.Error.Code
		}
		realtimeLog.Warningf("realtime error: runner=%s code=%s message=%s", self.ID, code, message)
		if self.callbacks.OnError != nil {
			self.callbacks.OnError(code, message)
		}

	case "input_audio_buffer.speech_started":
		realtimeLog.Debugf("realtime speech_started: runner=%s", self.ID)
		if self.callbacks.OnInputSpeechStarted != nil {
			self.callbacks.OnInputSpeechStarted()
		}

	case "input_audio_buffer.speech_stopped":
		realtimeLog.Debugf("realtime speech_stopped: runner=%s", self.ID)
		if self.callbacks.OnInputSpeechStopped != nil {
			self.callbacks.OnInputSpeechStopped()
		}

	case "input_audio_buffer.committed":
		realtimeLog.Debugf("realtime input committed: runner=%s", self.ID)

	case "conversation.item.input_audio_transcription.completed":
		transcript := event.Transcript
		if transcript != "" {
			realtimeLog.Infof("realtime user transcript: runner=%s text=%q", self.ID, transcript)
			self.broadcastConversationEvent(map[string]interface{}{
				"state":  "user_message",
				"text":   transcript,
				"origin": "voice",
			})
			self.persistMessage("user", transcript)
			if self.callbacks.OnInputTranscript != nil {
				self.callbacks.OnInputTranscript(transcript)
			}
		}

	case "response.created":
		var response struct {
			ID string `json:"id"`
		}
		if event.Response != nil {
			_ = json.Unmarshal(event.Response, &response)
		}
		runId := security.NewULID()
		self.currentResponseMutex.Lock()
		self.currentResponseId = response.ID
		self.currentRunId = runId
		self.accumulatedText = ""
		self.currentResponseMutex.Unlock()
		realtimeLog.Infof("realtime response.created: runner=%s response=%s run=%s", self.ID, response.ID, runId)
		if self.callbacks.OnResponseStarted != nil {
			self.callbacks.OnResponseStarted(response.ID)
		}

	case "response.output_item.added":
		// Informational — output item tracking.

	case "response.audio.delta":
		self.handleAudioDelta(event)

	case "response.audio_transcript.delta":
		if event.Delta != "" {
			self.currentResponseMutex.Lock()
			self.accumulatedText += event.Delta
			runId := self.currentRunId
			self.currentResponseMutex.Unlock()
			self.broadcastConversationEvent(map[string]interface{}{
				"state": "delta",
				"runId": runId,
				"text":  event.Delta,
			})
			if self.callbacks.OnTextDelta != nil {
				self.callbacks.OnTextDelta(event.Delta)
			}
		}

	case "response.audio_transcript.done":
		self.currentResponseMutex.RLock()
		text := self.accumulatedText
		self.currentResponseMutex.RUnlock()
		realtimeLog.Debugf("realtime audio transcript done: runner=%s len=%d", self.ID, len(text))
		if self.callbacks.OnTextDone != nil {
			self.callbacks.OnTextDone(text)
		}

	case "response.audio.done":
		realtimeLog.Debugf("realtime audio done: runner=%s", self.ID)

	case "response.function_call_arguments.delta":
		self.accumulateToolCallArgs(event)

	case "response.function_call_arguments.done":
		self.handleToolCallDone(event)

	case "response.done":
		self.handleResponseDone()

	case "rate_limits.updated":
		// Informational.

	default:
		realtimeLog.Debugf("realtime unhandled event: runner=%s type=%s", self.ID, event.Type)
	}
}

func (self *RealtimeRunner) handleAudioDelta(event providers.RealtimeEvent) {
	if event.Delta == "" || self.callbacks.OnAudioDelta == nil {
		return
	}
	pcmData, err := base64.StdEncoding.DecodeString(event.Delta)
	if err != nil {
		realtimeLog.Warningf("realtime audio decode error: runner=%s err=%v", self.ID, err)
		return
	}
	self.callbacks.OnAudioDelta(pcmData)
}

func (self *RealtimeRunner) accumulateToolCallArgs(event providers.RealtimeEvent) {
	if event.CallID == "" {
		return
	}
	self.toolCallsMutex.Lock()
	defer self.toolCallsMutex.Unlock()
	pending, ok := self.toolCalls[event.CallID]
	if !ok {
		pending = &realtimePendingToolCall{CallID: event.CallID, Name: event.Name}
		self.toolCalls[event.CallID] = pending
	}
	if event.Name != "" {
		pending.Name = event.Name
	}
	pending.Arguments += event.Delta
}

func (self *RealtimeRunner) handleToolCallDone(event providers.RealtimeEvent) {
	callId := event.CallID
	if callId == "" {
		return
	}

	self.toolCallsMutex.Lock()
	pending, ok := self.toolCalls[callId]
	if ok {
		if event.Arguments != "" {
			pending.Arguments = event.Arguments
		}
		delete(self.toolCalls, callId)
	} else {
		pending = &realtimePendingToolCall{
			CallID:    callId,
			Name:      event.Name,
			Arguments: event.Arguments,
		}
	}
	self.toolCallsMutex.Unlock()

	realtimeLog.Infof("realtime tool call: runner=%s call=%s name=%s args_len=%d",
		self.ID, pending.CallID, pending.Name, len(pending.Arguments))

	go self.executeToolCall(pending)
}

func (self *RealtimeRunner) executeToolCall(toolCall *realtimePendingToolCall) {
	defer deferutil.Recover()

	self.currentResponseMutex.RLock()
	runId := self.currentRunId
	self.currentResponseMutex.RUnlock()

	self.broadcastConversationEvent(map[string]interface{}{
		"state":     "tool_call",
		"runId":     runId,
		"toolName":  toolCall.Name,
		"arguments": toolCall.Arguments,
	})

	if self.callbacks.OnToolCall != nil {
		self.callbacks.OnToolCall(toolCall.Name, toolCall.Arguments)
	}

	// Persist the assistant tool call as a message with tool_calls metadata.
	self.persistToolCallMessage(toolCall)

	tool := self.toolRegistry.Get(toolCall.Name)
	if tool == nil {
		realtimeLog.Warningf("realtime tool not found: runner=%s name=%s", self.ID, toolCall.Name)
		result := `{"error": "tool not found: ` + toolCall.Name + `"}`
		self.persistToolResultMessage(toolCall.CallID, toolCall.Name, result)
		self.sendToolCallOutput(toolCall.CallID, result)
		return
	}

	// Build execution context with runner metadata.
	ctx := context.Background()
	ctx = ContextWithOrigin(ctx, OriginWeb)
	ctx = ContextWithVoiceMode(ctx, VoiceModeCall)

	result, err := tool.Execute(ctx, toolCall.Arguments)
	if err != nil {
		realtimeLog.Warningf("realtime tool execution error: runner=%s call=%s name=%s err=%v",
			self.ID, toolCall.CallID, toolCall.Name, err)
		result = `{"error": "` + err.Error() + `"}`
	}

	realtimeLog.Infof("realtime tool result: runner=%s call=%s name=%s result_len=%d",
		self.ID, toolCall.CallID, toolCall.Name, len(result))

	self.persistToolResultMessage(toolCall.CallID, toolCall.Name, result)

	self.broadcastConversationEvent(map[string]interface{}{
		"state":    "tool_result",
		"runId":    runId,
		"toolName": toolCall.Name,
		"result":   result,
	})

	if self.callbacks.OnToolResult != nil {
		self.callbacks.OnToolResult(toolCall.Name, result)
	}

	self.sendToolCallOutput(toolCall.CallID, result)
}

func (self *RealtimeRunner) sendToolCallOutput(callId, output string) {
	_ = self.conn.SendJSON(map[string]any{
		"type": "conversation.item.create",
		"item": map[string]any{
			"type":    "function_call_output",
			"call_id": callId,
			"output":  output,
		},
	})
	// Trigger a new response to incorporate the tool result.
	_ = self.conn.SendJSON(map[string]any{
		"type": "response.create",
	})
}

func (self *RealtimeRunner) handleResponseDone() {
	self.currentResponseMutex.RLock()
	responseId := self.currentResponseId
	runId := self.currentRunId
	accumulatedText := self.accumulatedText
	self.currentResponseMutex.RUnlock()

	realtimeLog.Infof("realtime response.done: runner=%s response=%s run=%s text_len=%d",
		self.ID, responseId, runId, len(accumulatedText))

	self.broadcastConversationEvent(map[string]interface{}{
		"state": "final",
		"runId": runId,
		"text":  accumulatedText,
	})

	if accumulatedText != "" {
		self.persistMessage("assistant", accumulatedText)
	}

	if self.callbacks.OnResponseDone != nil {
		self.callbacks.OnResponseDone(responseId)
	}

	self.currentResponseMutex.Lock()
	self.currentResponseId = ""
	self.currentRunId = ""
	self.accumulatedText = ""
	self.currentResponseMutex.Unlock()
}

// persistToolCallMessage saves an assistant message with a tool call invocation.
func (self *RealtimeRunner) persistToolCallMessage(toolCall *realtimePendingToolCall) {
	if self.storeContext == nil {
		return
	}
	message := newToolCallMessage(toolCall.CallID, toolCall.Name, toolCall.Arguments)
	if err := saveConversationMessage(self.storeContext, self.ConversationID, message); err != nil {
		realtimeLog.Warningf("realtime persist tool call failed: runner=%s err=%v", self.ID, err)
	}
}

// persistToolResultMessage saves a tool result message.
func (self *RealtimeRunner) persistToolResultMessage(callId, toolName, result string) {
	if self.storeContext == nil {
		return
	}
	message := newToolMessage(callId, toolName, result)
	if err := saveConversationMessage(self.storeContext, self.ConversationID, message); err != nil {
		realtimeLog.Warningf("realtime persist tool result failed: runner=%s err=%v", self.ID, err)
	}
}

// persistMessage saves a message to the conversation store.
func (self *RealtimeRunner) persistMessage(role, text string) {
	if self.storeContext == nil {
		return
	}
	message := newTextMessage(role, text)
	if err := saveConversationMessage(self.storeContext, self.ConversationID, message); err != nil {
		realtimeLog.Warningf("realtime persist %s message failed: runner=%s err=%v", role, self.ID, err)
	}
}

// broadcastConversationEvent publishes a conversation event via pubsub.
// Common fields (conversationId, agentId, userId) are automatically added.
func (self *RealtimeRunner) broadcastConversationEvent(payload map[string]interface{}) {
	if self.events == nil {
		return
	}
	payload["conversationId"] = self.ConversationID
	payload["agentId"] = self.AgentID
	payload["userId"] = self.UserID
	self.events.Broadcast(pubsub.EventTypeConversation, payload)
}
