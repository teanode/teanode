package voice

import (
	"encoding/base64"
	"encoding/binary"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/util/security"
)

var realtimeLog = pipelineLog

// RealtimeSession manages the voice-specific audio transport layer for a
// Realtime API session. It handles audio format conversion (resampling,
// binary frame encoding) and delegates all LLM/tool interaction to a
// RealtimeRunner from the runners package.
type RealtimeSession struct {
	ID             string
	ConversationID string
	AgentID        string
	AudioIn        AudioFormat
	AudioOut       AudioFormat

	runner             *runners.RealtimeRunner
	sendJsonFunction   func(any)
	sendBinaryFunction func([]byte)

	closeOnce   sync.Once
	doneChannel chan struct{}

	outSequence      uint64
	outSequenceMutex sync.Mutex

	// Current turn tracking for voice event emission.
	currentTurnMutex sync.RWMutex
	currentTurnId    string
	userTranscript   string
	assistantText    string
}

// NewRealtimeSession creates a new Realtime API-backed voice session.
// The runner handles the WebSocket event loop and tool execution; this
// session handles audio format conversion and voice-specific framing.
func NewRealtimeSession(
	id, conversationId, agentId string,
	audioIn, audioOut AudioFormat,
	runner *runners.RealtimeRunner,
	sendJson func(any),
	sendBinary func([]byte),
) *RealtimeSession {
	return &RealtimeSession{
		ID:                 id,
		ConversationID:     conversationId,
		AgentID:            agentId,
		AudioIn:            audioIn,
		AudioOut:           audioOut,
		runner:             runner,
		sendJsonFunction:   sendJson,
		sendBinaryFunction: sendBinary,
		doneChannel:        make(chan struct{}),
	}
}

// SetRunner sets the RealtimeRunner for this session. Must be called before Start().
func (self *RealtimeSession) SetRunner(runner *runners.RealtimeRunner) {
	self.runner = runner
}

// Start configures the realtime session and begins the runner's event loop.
// The voice session wires runner callbacks to voice-specific audio framing.
func (self *RealtimeSession) Start(instructions string, voice string) error {
	realtimeLog.Infof("realtime session starting: session=%s conv=%s agent=%s",
		self.ID, self.ConversationID, self.AgentID)

	if err := self.runner.Start(instructions, voice); err != nil {
		return err
	}

	// Monitor runner lifecycle — when the runner's event loop exits, close this session.
	go func() {
		select {
		case <-self.runner.Done():
			self.sendVoiceEvent("session.ended", map[string]any{
				"reason":         "connectionClosed",
				"conversationId": self.ConversationID,
			})
		case <-self.doneChannel:
		}
	}()

	realtimeLog.Infof("realtime session started: session=%s conv=%s", self.ID, self.ConversationID)
	return nil
}

// SetupCallbacks wires the runner's callbacks to the voice session's audio
// framing and event emission. Call this before Start().
func (self *RealtimeSession) SetupCallbacks() *runners.RealtimeCallbacks {
	return &runners.RealtimeCallbacks{
		OnError: func(code string, message string) {
			self.sendVoiceEvent("voice.error", map[string]any{
				"code":        code,
				"message":     message,
				"recoverable": true,
			})
		},
		OnInputSpeechStarted: func() {
			self.sendVoiceEvent("turn.event", map[string]any{
				"event": "speechStarted",
			})
		},
		OnInputSpeechStopped: func() {
			self.sendVoiceEvent("turn.event", map[string]any{
				"event": "speechEnded",
			})
		},
		OnInputTranscript: func(text string) {
			turnId := self.getCurrentTurnId()
			self.currentTurnMutex.Lock()
			self.userTranscript = text
			self.currentTurnMutex.Unlock()
			self.sendVoiceEvent("transcript.final", map[string]any{
				"turnId": turnId,
				"text":   text,
			})
		},
		OnResponseStarted: func(responseId string) {
			turnId := security.NewULID()
			self.currentTurnMutex.Lock()
			self.currentTurnId = turnId
			self.assistantText = ""
			self.currentTurnMutex.Unlock()
			self.sendVoiceEvent("response.started", map[string]any{
				"responseId": responseId,
				"turnId":     turnId,
			})
		},
		OnAudioDelta: func(pcmData []byte) {
			// OpenAI outputs 24kHz PCM16 — same as our output format, no resampling needed.
			frame := EncodeBinaryAudioFrame(BinaryAudioFrame{
				FrameType:   FrameTypeAudioOut,
				Seq:         self.nextOutSequence(),
				CaptureTSMs: time.Now().UnixMilli(),
				Data:        pcmData,
			})
			if self.sendBinaryFunction != nil {
				self.sendBinaryFunction(frame)
			}
		},
		OnTextDelta: func(text string) {
			self.currentTurnMutex.Lock()
			self.assistantText += text
			self.currentTurnMutex.Unlock()
		},
		OnResponseDone: func(responseId string) {
			self.currentTurnMutex.RLock()
			turnId := self.currentTurnId
			self.currentTurnMutex.RUnlock()
			self.sendVoiceEvent("response.completed", map[string]any{
				"responseId": responseId,
				"turnId":     turnId,
			})
			self.currentTurnMutex.Lock()
			self.currentTurnId = ""
			self.assistantText = ""
			self.currentTurnMutex.Unlock()
		},
		OnSessionEnded: func(reason string) {
			self.sendVoiceEvent("session.ended", map[string]any{
				"reason":         reason,
				"conversationId": self.ConversationID,
			})
		},
	}
}

// HandleInputBinaryFrame receives a binary PCM frame from the client and forwards it
// to the Realtime API as a base64-encoded input_audio_buffer.append event.
func (self *RealtimeSession) HandleInputBinaryFrame(raw []byte) error {
	frame, err := ParseBinaryAudioFrame(raw)
	if err != nil {
		return err
	}
	if frame.FrameType != FrameTypeAudioIn {
		return nil
	}

	// The client sends 16kHz PCM. OpenAI Realtime expects 24kHz PCM16.
	pcm24k := resamplePCM16(frame.Data, self.AudioIn.SampleRateHz, 24000)
	encoded := base64.StdEncoding.EncodeToString(pcm24k)

	return self.runner.SendAudioBase64(encoded)
}

// Close terminates the Realtime session and the underlying runner.
func (self *RealtimeSession) Close() {
	self.closeOnce.Do(func() {
		realtimeLog.Infof("realtime session close: session=%s", self.ID)
		close(self.doneChannel)
		self.runner.Close()
	})
}

// CancelResponse cancels the current in-progress response.
func (self *RealtimeSession) CancelResponse() {
	_ = self.runner.CancelResponse()
	self.sendFlushFrame()
}

// InputCommit commits the current audio buffer (for push-to-talk mode).
func (self *RealtimeSession) InputCommit(_ string) {
	_ = self.runner.CommitInput()
}

func (self *RealtimeSession) nextOutSequence() uint64 {
	self.outSequenceMutex.Lock()
	defer self.outSequenceMutex.Unlock()
	self.outSequence++
	return self.outSequence
}

func (self *RealtimeSession) getCurrentTurnId() string {
	self.currentTurnMutex.RLock()
	defer self.currentTurnMutex.RUnlock()
	return self.currentTurnId
}

func (self *RealtimeSession) sendVoiceEvent(eventType string, payload interface{}) {
	if self.sendJsonFunction == nil {
		return
	}
	self.sendJsonFunction(map[string]interface{}{
		"v":         1,
		"type":      eventType,
		"sessionId": self.ID,
		"seq":       self.nextOutSequence(),
		"tsMs":      time.Now().UnixMilli(),
		"payload":   payload,
	})
}

func (self *RealtimeSession) sendFlushFrame() {
	frame := EncodeBinaryAudioFrame(BinaryAudioFrame{
		FrameType:   FrameTypeFlush,
		Seq:         self.nextOutSequence(),
		CaptureTSMs: time.Now().UnixMilli(),
	})
	if self.sendBinaryFunction != nil {
		self.sendBinaryFunction(frame)
	}
}

// resamplePCM16 resamples raw PCM16 LE audio from srcRate to dstRate using linear interpolation.
func resamplePCM16(data []byte, srcRate, dstRate int) []byte {
	if srcRate == dstRate || srcRate <= 0 || dstRate <= 0 {
		return data
	}
	sampleCount := len(data) / 2
	if sampleCount == 0 {
		return data
	}

	ratio := float64(srcRate) / float64(dstRate)
	outCount := int(float64(sampleCount) / ratio)
	out := make([]byte, outCount*2)

	for index := 0; index < outCount; index++ {
		srcPos := float64(index) * ratio
		left := int(srcPos)
		right := left + 1
		if right >= sampleCount {
			right = sampleCount - 1
		}
		frac := srcPos - float64(left)

		leftSample := int16(binary.LittleEndian.Uint16(data[left*2:]))
		rightSample := int16(binary.LittleEndian.Uint16(data[right*2:]))
		interpolated := float64(leftSample) + (float64(rightSample)-float64(leftSample))*frac

		sample := int16(interpolated)
		binary.LittleEndian.PutUint16(out[index*2:], uint16(sample))
	}
	return out
}
