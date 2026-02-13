// Package pending provides a thread-safe request-response correlation map.
// It assigns incrementing integer IDs to outgoing requests and delivers
// results back through per-request channels.
package pending

import (
	"encoding/json"
	"sync"
)

// Result holds the response for a pending request.
type Result struct {
	Data  json.RawMessage
	Error string
}

// Requests manages a set of pending request-response channels.
type Requests struct {
	mutex    sync.Mutex
	channels map[int]chan Result
	nextId   int
}

// NewRequests creates a new pending request tracker.
func NewRequests() *Requests {
	return &Requests{
		channels: make(map[int]chan Result),
	}
}

// Allocate reserves a new request ID and returns it along with a channel
// that will receive the result.
func (self *Requests) Allocate() (int, <-chan Result) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	id := self.nextId
	self.nextId++
	channel := make(chan Result, 1)
	self.channels[id] = channel
	return id, channel
}

// Resolve delivers a result to the pending request with the given ID.
// Returns false if the ID was not found (already resolved or cancelled).
func (self *Requests) Resolve(id int, result Result) bool {
	self.mutex.Lock()
	channel, ok := self.channels[id]
	if ok {
		delete(self.channels, id)
	}
	self.mutex.Unlock()
	if ok {
		channel <- result
	}
	return ok
}

// Cancel removes a pending request without delivering a result.
func (self *Requests) Cancel(id int) {
	self.mutex.Lock()
	delete(self.channels, id)
	self.mutex.Unlock()
}

// RejectAll sends an error result to all pending requests and clears them.
func (self *Requests) RejectAll(errorMessage string) {
	self.mutex.Lock()
	for id, channel := range self.channels {
		channel <- Result{Error: errorMessage}
		delete(self.channels, id)
	}
	self.mutex.Unlock()
}
