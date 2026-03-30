package browser

import "sync"

type navigationSnapshot struct {
	generation uint64
	url        string
}

type navigationStore struct {
	sessions map[string]navigationSnapshot
	mutex    sync.Mutex
}

var globalNavigationStore = &navigationStore{
	sessions: make(map[string]navigationSnapshot),
}

func (self *navigationStore) markNavigated(sessionId string, url string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	state := self.sessions[sessionId]
	state.generation++
	state.url = url
	self.sessions[sessionId] = state
}

func (self *navigationStore) clear(sessionId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	delete(self.sessions, sessionId)
}

func (self *navigationStore) snapshot(sessionId string) (uint64, string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	state := self.sessions[sessionId]
	return state.generation, state.url
}
