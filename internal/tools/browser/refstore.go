// Package browser — refstore manages the mapping between stable snapshot refs
// and DOM backend node IDs, enabling ref-based browser interactions.
package browser

import (
	"fmt"
	"sync"
)

// refEntry holds the mapping from a snapshot ref to its backend DOM node.
type refEntry struct {
	BackendDOMNodeID int    // CDP backendDOMNodeId for DOM.resolveNode
	Role             string // accessibility role (e.g. "button", "link")
	Name             string // accessibility name
}

// refStore is a per-session store of ref→DOM node mappings. Each snapshot
// overwrites the previous mapping for that session.
type refStore struct {
	// sessions maps sessionId → (ref → refEntry).
	sessions map[string]map[int]refEntry
	mutex    sync.Mutex
}

var globalRefStore = &refStore{
	sessions: make(map[string]map[int]refEntry),
}

// store replaces the ref mapping for the given session.
func (self *refStore) store(sessionId string, refs map[int]refEntry) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.sessions[sessionId] = refs
}

// lookup returns the refEntry for a given session and ref number.
func (self *refStore) lookup(sessionId string, ref int) (refEntry, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	refs, ok := self.sessions[sessionId]
	if !ok {
		return refEntry{}, fmt.Errorf("no snapshot refs for this session — run a snapshot first")
	}
	entry, ok := refs[ref]
	if !ok {
		return refEntry{}, fmt.Errorf("ref %d not found in last snapshot — run a new snapshot to refresh refs", ref)
	}
	return entry, nil
}

// clear removes the ref mapping for a session (e.g. on navigation).
func (self *refStore) clear(sessionId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	delete(self.sessions, sessionId)
}

// interactiveRoles is the set of accessibility roles that receive a ref
// in the snapshot output. These are elements the LLM is likely to interact with.
var interactiveRoles = map[string]bool{
	"button":           true,
	"link":             true,
	"textbox":          true,
	"searchbox":        true,
	"combobox":         true,
	"listbox":          true,
	"option":           true,
	"checkbox":         true,
	"radio":            true,
	"switch":           true,
	"slider":           true,
	"spinbutton":       true,
	"tab":              true,
	"menuitem":         true,
	"menuitemcheckbox": true,
	"menuitemradio":    true,
	"treeitem":         true,
	"row":              true,
	"gridcell":         true,
	"cell":             true,
	"columnheader":     true,
	"rowheader":        true,
}

// isInteractiveRole returns true if the role should receive a stable ref.
func isInteractiveRole(role string) bool {
	return interactiveRoles[role]
}
