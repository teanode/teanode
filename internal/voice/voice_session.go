package voice

// VoiceSession is the interface satisfied by both the classic Session and RealtimeSession.
// The WebSocket connection layer uses this to handle voice calls without knowing the pipeline type.
type VoiceSession interface {
	// SessionID returns the unique session identifier.
	SessionID() string
	// HandleInputBinaryFrame processes a binary audio frame from the client.
	HandleInputBinaryFrame(raw []byte) error
	// CancelResponse interrupts the current response.
	CancelResponse()
	// InputCommit flushes buffered input (push-to-talk).
	InputCommit(reason string)
	// Close terminates the session and waits for goroutines to exit.
	Close()
}

// Verify interface compliance at compile time.
var (
	_ VoiceSession = (*SessionAdapter)(nil)
	_ VoiceSession = (*RealtimeSession)(nil)
)

// SessionAdapter wraps the classic Session to satisfy VoiceSession.
type SessionAdapter struct {
	*Session
}

func (self *SessionAdapter) SessionID() string {
	return self.ID
}

// RealtimeSession already has the right method signatures except SessionID.
func (self *RealtimeSession) SessionID() string {
	return self.ID
}
