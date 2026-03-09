package coordinators

import (
	"sync"

	"github.com/teanode/teanode/internal/runners"
)

// RunParameters are the parameters for sending a message through the coordinator.
type RunParameters struct {
	AgentID           string
	ConversationID    string // empty = auto-create
	Message           string
	ProviderModelName string
	OriginID          string              // opaque client-generated ID echoed in broadcasts so the sender can filter its own messages
	Origin            runners.Origin      // source of the message; empty for automated sources like the scheduler
	OriginSessionID   string              // source session identifier (used for disconnect-aware notifications)
	Attachments       []map[string]string // file attachments
	VoiceMode         runners.VoiceMode   // voice interaction type; empty = normal text
	SystemPromptMode  runners.SystemPromptMode
}

// RunHandle is returned by Run and allows the caller to wait for completion.
type RunHandle struct {
	RunID          string
	ConversationID string
	done           chan struct{}
	once           sync.Once
	result         *runners.RunResult
	err            error
}

// NewRunHandle creates a new RunHandle.
func NewRunHandle(runId, conversationId string) *RunHandle {
	return &RunHandle{
		RunID:          runId,
		ConversationID: conversationId,
		done:           make(chan struct{}),
	}
}

// Done returns a channel that is closed when the run completes.
func (self *RunHandle) Done() <-chan struct{} {
	return self.done
}

// Wait blocks until the handle completes and returns both result types and the error.
func (self *RunHandle) Wait() (*runners.RunResult, error) {
	<-self.done
	return self.result, self.err
}

// Resolve completes the handle with the given results and error.
// It is safe to call Resolve multiple times; only the first call takes effect.
func (self *RunHandle) Resolve(runResult *runners.RunResult, err error) {
	self.once.Do(func() {
		self.result = runResult
		self.err = err
		close(self.done)
	})
}
