package coordinators

import (
	"sync"

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

// RunHandle is returned by SendMessage and CompactConversation and allows the caller to wait for completion.
type RunHandle struct {
	RunnerID       string
	ConversationID string
	done           chan struct{}
	once           sync.Once
	result         *runners.RunResult
	compactResult  *runners.CompactResult
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

// Wait blocks until the handle completes and returns both result types and the error.
func (self *RunHandle) Wait() (*runners.RunResult, *runners.CompactResult, error) {
	<-self.done
	return self.result, self.compactResult, self.err
}

// Resolve completes the handle with the given results and error.
// It is safe to call Resolve multiple times; only the first call takes effect.
func (self *RunHandle) Resolve(runResult *runners.RunResult, compactResult *runners.CompactResult, err error) {
	self.once.Do(func() {
		self.result = runResult
		self.compactResult = compactResult
		self.err = err
		close(self.done)
	})
}
