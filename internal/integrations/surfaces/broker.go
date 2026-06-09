package surfaces

import "sync"

// SurfaceBroker is an in-memory registry of active surfaces and interrupts per
// conversation. It mirrors the design of questions.QuestionBroker and
// approvals.ApprovalBroker: state lives only in memory and exists to let the
// RPC layer list (rehydrate) active render payloads and validate incoming
// surface actions.
type SurfaceBroker struct {
	mutex      sync.Mutex
	surfaces   map[string]*Surface   // surfaceId -> surface
	interrupts map[string]*Interrupt // interruptId -> interrupt
}

// NewSurfaceBroker creates an empty broker.
func NewSurfaceBroker() *SurfaceBroker {
	return &SurfaceBroker{
		surfaces:   make(map[string]*Surface),
		interrupts: make(map[string]*Interrupt),
	}
}

// RegisterSurface stores an emitted surface so it can be listed and actioned.
func (self *SurfaceBroker) RegisterSurface(surface *Surface) {
	if surface == nil || surface.SurfaceID == "" {
		return
	}
	self.mutex.Lock()
	self.surfaces[surface.SurfaceID] = surface
	self.mutex.Unlock()
}

// RegisterInterrupt stores an active interrupt.
func (self *SurfaceBroker) RegisterInterrupt(interrupt *Interrupt) {
	if interrupt == nil || interrupt.InterruptID == "" {
		return
	}
	self.mutex.Lock()
	self.interrupts[interrupt.InterruptID] = interrupt
	self.mutex.Unlock()
}

// LookupSurface returns the surface with the given id, or nil.
func (self *SurfaceBroker) LookupSurface(surfaceId string) *Surface {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.surfaces[surfaceId]
}

// RemoveSurface deletes a surface from the registry.
func (self *SurfaceBroker) RemoveSurface(surfaceId string) {
	self.mutex.Lock()
	delete(self.surfaces, surfaceId)
	self.mutex.Unlock()
}

// RemoveInterrupt deletes an interrupt from the registry.
func (self *SurfaceBroker) RemoveInterrupt(interruptId string) {
	self.mutex.Lock()
	delete(self.interrupts, interruptId)
	self.mutex.Unlock()
}

// SurfacesForConversation returns all active surfaces for a conversation.
func (self *SurfaceBroker) SurfacesForConversation(conversationId string) []*Surface {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	result := make([]*Surface, 0)
	for _, surface := range self.surfaces {
		if surface.ConversationID == conversationId {
			result = append(result, surface)
		}
	}
	return result
}

// InterruptsForConversation returns all active interrupts for a conversation.
func (self *SurfaceBroker) InterruptsForConversation(conversationId string) []*Interrupt {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	result := make([]*Interrupt, 0)
	for _, interrupt := range self.interrupts {
		if interrupt.ConversationID == conversationId {
			result = append(result, interrupt)
		}
	}
	return result
}

// InterruptsForSurface returns interrupts whose actions are routed through the
// given surface id.
func (self *SurfaceBroker) InterruptsForSurface(surfaceId string) []*Interrupt {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	result := make([]*Interrupt, 0)
	for _, interrupt := range self.interrupts {
		if interrupt.SurfaceID == surfaceId {
			result = append(result, interrupt)
		}
	}
	return result
}
