package voice

func (self *Session) audioOutputLoop() {
	for {
		select {
		case <-self.doneChannel:
			return
		case data := <-self.audioOutChannel:
			if self.sendBinaryFunction != nil {
				self.sendBinaryFunction(data)
			}
		}
	}
}
