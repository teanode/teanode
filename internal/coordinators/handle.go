package coordinators

import (
	"github.com/teanode/teanode/internal/runners"
)

// SendMessageParameters are the parameters for sending a message through the coordinator.
type SendMessageParameters struct {
	AgentID            string
	ConversationID     string // empty = auto-create
	Message            string
	Model              string
	OriginID           string              // opaque client-generated ID echoed in broadcasts so the sender can filter its own messages
	Origin             string              // source of the message (e.g. "webui", "discord", "telegram"); empty for automated sources like the scheduler
	OriginSessionID    string              // source session identifier (used for disconnect-aware notifications)
	Attachments        []map[string]string // file attachments
	SystemPromptSuffix string              // optional; appended to system prompt for this run only
	SystemPromptMode   runners.SystemPromptMode
}

// RunHandle is returned by SendMessage and allows the caller to wait for completion.
type RunHandle struct {
	RunnerID       string
	ConversationID string
	done           chan struct{}
	result         *runners.RunResult
	err            error
}

// NewRunHandle creates a new RunHandle.
func NewRunHandle(runnerId, conversationId string) *RunHandle {
	return &RunHandle{
		RunnerID:       runnerId,
		ConversationID: conversationId,
		done:           make(chan struct{}),
	}
}

// Done returns a channel that is closed when the run completes.
func (self *RunHandle) Done() <-chan struct{} {
	return self.done
}

// Wait blocks until the run completes and returns the result.
func (self *RunHandle) Wait() (*runners.RunResult, error) {
	<-self.done
	return self.result, self.err
}

// Resolve completes the handle with the given result and error.
func (self *RunHandle) Resolve(result *runners.RunResult, err error) {
	self.result = result
	self.err = err
	close(self.done)
}

// CompactHandle is returned by CompactConversation and allows the caller to wait for completion.
type CompactHandle struct {
	done   chan struct{}
	result *runners.CompactResult
	err    error
}

// NewCompactHandle creates a new CompactHandle.
func NewCompactHandle() *CompactHandle {
	return &CompactHandle{
		done: make(chan struct{}),
	}
}

// Done returns a channel that is closed when the compaction completes.
func (self *CompactHandle) Done() <-chan struct{} {
	return self.done
}

// Wait blocks until the compaction completes and returns the result.
func (self *CompactHandle) Wait() (*runners.CompactResult, error) {
	<-self.done
	return self.result, self.err
}

// Resolve completes the handle with the given result and error.
func (self *CompactHandle) Resolve(result *runners.CompactResult, err error) {
	self.result = result
	self.err = err
	close(self.done)
}
