package voice

func (self *Session) audioOutputLoop() {
	for {
		select {
		case <-self.doneCh:
			return
		case data := <-self.audioOutCh:
			if self.sendBinaryFn != nil {
				self.sendBinaryFn(data)
			}
		}
	}
}
