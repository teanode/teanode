package voice

func (self *Session) streamingTranscribeLoop() {
	stream := self.getStreamingTranscribeStream()
	if stream == nil {
		return
	}
	for {
		select {
		case <-self.doneChannel:
			return
		case event, ok := <-stream.Events():
			if !ok {
				self.setStreamingTranscribeStream(nil)
				return
			}
			if event.Err != nil {
				pipelineLog.Warningf("voice streaming stt failed, falling back to batch: %v", event.Err)
				_ = stream.Close()
				self.setStreamingTranscribeStream(nil)
				return
			}
			switch event.Type {
			case "interim":
				self.setInterimText(event.Text)
			case "final":
				turnId := self.GetCurrentTurnID()
				if turnId == "" {
					continue
				}
				self.setInterimText(event.Text)
				// Buffer final streaming text and commit on turn-end path to avoid
				// locking in an early partial final when additional words arrive.
				self.setStreamingFinalText(turnId, event.Text)
			}
		}
	}
}
