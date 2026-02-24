package voice

import "github.com/op/go-logging"

var pipelineLog = logging.MustGetLogger("voice.pipeline")

// Start begins session background loops.
func (self *Session) Start() {
	pipelineLog.Infof("voice session start: session=%s conv=%s agent=%s", self.ID, self.ConversationID, self.AgentID)
	streamingEnabled := self.startStreamingTranscriber()
	self.wg.Add(4)
	if streamingEnabled {
		self.wg.Add(1)
		go func() { defer self.wg.Done(); self.streamingTranscribeLoop() }()
	}
	go func() { defer self.wg.Done(); self.audioInputLoop() }()
	go func() { defer self.wg.Done(); self.llmEventForwarder() }()
	go func() { defer self.wg.Done(); self.ttsSynthLoop() }()
	go func() { defer self.wg.Done(); self.audioOutputLoop() }()
}

func (self *Session) startNewTurn(turnId string) {
	self.stateMu.Lock()
	self.currentTurnId = turnId
	self.interimText = ""
	self.interimBestText = ""
	self.streamingFinalTurnID = ""
	self.streamingFinalText = ""
	self.stateMu.Unlock()
	// Advance the generation counter. Set bargeInFired to newGen-1 so that the
	// CAS(gen-1 → gen) in triggerBargeIn succeeds exactly once for this new
	// generation, regardless of whether the previous generation ever fired.
	newGen := self.bargeInGen.Add(1)
	self.bargeInFired.Store(newGen - 1)
}
