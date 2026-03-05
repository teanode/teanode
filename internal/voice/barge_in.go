package voice

import "time"

func (self *Session) triggerBargeIn() {
	// Exactly-once per turn using a generation counter instead of sync.Once.
	// bargeInFired holds the last generation for which barge-in fired (0 = none).
	// CAS(gen-1 -> gen) succeeds only for the first caller in each generation,
	// and stale callers from a previous turn will CAS against an already-advanced
	// fired value and safely return without firing.
	gen := self.bargeInGen.Load()
	if !self.bargeInFired.CompareAndSwap(gen-1, gen) {
		return // already fired for this generation
	}
	pipelineLog.Infof("voice barge_in triggered: session=%s run=%s response=%s", self.ID, self.GetCurrentRunId(), self.GetCurrentResponseId())
	self.setLastBargeInAt(time.Now())
	runId := self.GetCurrentRunId()
	self.MarkRunCanceled(runId)
	if prev := self.SwapTTSCancel(nil); prev != nil {
		prev()
	}
	self.drainTTSQueue()
	self.drainAudioOutQueue()
	self.trySendFlushFrame()
	if runId != "" && self.dispatcher != nil {
		self.dispatcher.AbortRun(runId)
	}
	self.ClearCurrentRun()
	self.ClearCurrentResponse()
	self.sendVoiceEvent("turn.event", turnEventPayload{Event: "barge_in_triggered"})
}

func (self *Session) drainTTSQueue() {
	for {
		select {
		case <-self.ttsInCh:
		default:
			return
		}
	}
}

func (self *Session) drainAudioOutQueue() {
	for {
		select {
		case <-self.audioOutCh:
		default:
			return
		}
	}
}

func (self *Session) trySendFlushFrame() {
	pipelineLog.Debugf("voice flush frame queued: session=%s", self.ID)
	payload := EncodeBinaryAudioFrame(BinaryAudioFrame{
		FrameType:   FrameTypeFlush,
		Seq:         self.NextOutSeq(),
		CaptureTSMs: time.Now().UnixMilli(),
		DurationMS:  0,
	})
	self.enqueueAudioOut(payload)
}
