package voice

import (
	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/util/deferutil"
)

var pipelineLog = logging.MustGetLogger("voice.pipeline")

// Start begins session background loops.
func (self *Session) Start() {
	pipelineLog.Infof("voice session start: session=%s conv=%s agent=%s", self.ID, self.ConversationID, self.AgentID)
	streamingEnabled := self.startStreamingTranscriber()
	self.waitGroup.Add(4)
	if streamingEnabled {
		self.waitGroup.Add(1)
		go func() {
			defer deferutil.Recover()
			defer self.waitGroup.Done()
			self.streamingTranscribeLoop()
		}()
	}
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		self.audioInputLoop()
	}()
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		self.llmEventForwarder()
	}()
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		self.ttsSynthLoop()
	}()
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		self.audioOutputLoop()
	}()
}

func (self *Session) startNewTurn(turnId string) {
	self.stateMutex.Lock()
	self.currentTurnId = turnId
	self.interimText = ""
	self.interimBestText = ""
	self.streamingFinalTurnId = ""
	self.streamingFinalText = ""
	self.stateMutex.Unlock()
	// Advance the generation counter. Set bargeInFired to newGen-1 so that the
	// CAS(gen-1 → gen) in triggerBargeIn succeeds exactly once for this new
	// generation, regardless of whether the previous generation ever fired.
	newGen := self.bargeInGen.Add(1)
	self.bargeInFired.Store(newGen - 1)
}
