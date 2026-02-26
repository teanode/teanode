package v1api

import (
	"encoding/json"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/voice"
)

func (self *webSocketConnection) handleVoiceStart(frame requestFrame) {
	var parameters voiceStartParams
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		log.Warningf("voice.start invalid parameters: %v", err)
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	applyVoiceDefaults(&parameters)
	if err := validateVoiceAudioFormats(parameters.AudioIn, parameters.AudioOut); err != nil {
		log.Warningf("voice.start format validation failed: %v", err)
		self.sendError(frame.ID, 400, err.Error())
		return
	}
	log.Infof("voice.start requested: agent=%s conv=%s in=%s/%dHz/%dch out=%s/%dHz/%dch features[vad=%v turn=%v denoise=%v barge=%v]",
		parameters.AgentID, parameters.ConversationID,
		parameters.AudioIn.Codec, parameters.AudioIn.SampleRateHz, parameters.AudioIn.Channels,
		parameters.AudioOut.Codec, parameters.AudioOut.SampleRateHz, parameters.AudioOut.Channels,
		parameters.Features.ServerVAD, parameters.Features.ServerTurn, parameters.Features.ServerDenoise, parameters.Features.BargeIn,
	)

	if self.getActiveVoiceSession() != nil {
		log.Warningf("voice.start conflict: active session already exists")
		self.sendError(frame.ID, 409, "voice session already active")
		return
	}

	// Resolve user and agent.
	user := models.UserFromContext(self.ctx)
	if user == nil || user.ID == "" {
		self.sendError(frame.ID, 401, "userId is required")
		return
	}
	agentId := user.GetDefaultAgentID()
	if agentId == "" {
		self.sendError(frame.ID, 500, "no default agent configured")
		return
	}

	// Resolve or create conversation.
	conversationId := parameters.ConversationID
	if conversationId == "" {
		conversationId = self.api.coordinator.NewDefaultConversation(user.ID, agentId, "")
	} else {
		self.api.coordinator.SetDefaultConversationIfUnset(user.ID, agentId, conversationId)
	}

	audioIn := voice.AudioFormat{
		Codec:        parameters.AudioIn.Codec,
		SampleRateHz: parameters.AudioIn.SampleRateHz,
		Channels:     parameters.AudioIn.Channels,
		FrameMS:      parameters.AudioIn.FrameMS,
	}
	audioOut := voice.AudioFormat{
		Codec:        parameters.AudioOut.Codec,
		SampleRateHz: parameters.AudioOut.SampleRateHz,
		Channels:     parameters.AudioOut.Channels,
		FrameMS:      parameters.AudioOut.FrameMS,
	}
	features := voice.Features{
		ServerVAD:     parameters.Features.ServerVAD,
		ServerTurn:    parameters.Features.ServerTurn,
		ServerDenoise: parameters.Features.ServerDenoise,
		BargeIn:       parameters.Features.BargeIn,
	}

	sessionId := security.NewULID()
	session := voice.NewSession(
		sessionId,
		conversationId,
		agentId,
		parameters.PromptSuffix,
		audioIn,
		audioOut,
		features,
		self.api.coordinator,
		self.api.pubsub,
		func(payload interface{}) { self.writeJSON(payload) },
		func(data []byte) { self.writeBinary(data) },
	)
	if !self.setActiveVoiceSession(session) {
		log.Warningf("voice.start race conflict while setting active session")
		self.sendError(frame.ID, 409, "voice session already active")
		return
	}

	session.Start()
	log.Infof("voice.start session ready: session=%s conv=%s", session.ID, session.ConversationID)
	self.sendResponse(frame.ID, voiceSessionReadyPayload{
		SessionID:      session.ID,
		ConversationID: session.ConversationID,
		AudioOut:       parameters.AudioOut,
		Features:       parameters.Features,
	})
}

func (self *webSocketConnection) handleVoiceEnd(frame requestFrame) {
	var parameters voiceEndParams
	if len(frame.Params) > 0 {
		if err := json.Unmarshal(frame.Params, &parameters); err != nil {
			log.Warningf("voice.end invalid parameters: %v", err)
			self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
			return
		}
	}
	session := self.getActiveVoiceSession()
	if session == nil {
		log.Warningf("voice.end without active session")
		self.sendError(frame.ID, 404, "no active voice session")
		return
	}
	if parameters.SessionID != "" && parameters.SessionID != session.ID {
		log.Warningf("voice.end session mismatch: requested=%s active=%s", parameters.SessionID, session.ID)
		self.sendError(frame.ID, 404, "voice session not found")
		return
	}
	log.Infof("voice.end closing session=%s", session.ID)
	session.Close()
	self.clearActiveVoiceSession(session)
	self.sendResponse(frame.ID, map[string]any{"ended": true})
}

func (self *webSocketConnection) handleVoiceResponseCancel(frame requestFrame) {
	var parameters voiceResponseCancelParams
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		log.Warningf("voice.response.cancel invalid parameters: %v", err)
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	session := self.getActiveVoiceSession()
	if session == nil {
		log.Warningf("voice.response.cancel without active session")
		self.sendError(frame.ID, 404, "no active voice session")
		return
	}
	log.Infof("voice.response.cancel session=%s response=%s reason=%s", session.ID, parameters.ResponseID, parameters.Reason)
	session.CancelResponse()
	self.sendResponse(frame.ID, map[string]any{"cancelled": true})
}

func (self *webSocketConnection) handleVoiceInputCommit(frame requestFrame) {
	var parameters voiceInputCommitParams
	if len(frame.Params) > 0 {
		if err := json.Unmarshal(frame.Params, &parameters); err != nil {
			log.Warningf("voice.input.commit invalid parameters: %v", err)
			self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
			return
		}
	}
	session := self.getActiveVoiceSession()
	if session == nil {
		log.Warningf("voice.input.commit without active session")
		self.sendError(frame.ID, 404, "no active voice session")
		return
	}
	log.Infof("voice.input.commit session=%s reason=%s", session.ID, parameters.Reason)
	session.InputCommit()
	self.sendResponse(frame.ID, map[string]any{"committed": true})
}

func applyVoiceDefaults(parameters *voiceStartParams) {
	if parameters.AudioIn.Codec == "" {
		parameters.AudioIn.Codec = "pcm_s16le"
	}
	if parameters.AudioIn.SampleRateHz == 0 {
		parameters.AudioIn.SampleRateHz = 16000
	}
	if parameters.AudioIn.Channels == 0 {
		parameters.AudioIn.Channels = 1
	}
	if parameters.AudioIn.FrameMS == 0 {
		parameters.AudioIn.FrameMS = 20
	}

	if parameters.AudioOut.Codec == "" {
		parameters.AudioOut.Codec = "pcm_s16le"
	}
	if parameters.AudioOut.SampleRateHz == 0 {
		parameters.AudioOut.SampleRateHz = 24000
	}
	if parameters.AudioOut.Channels == 0 {
		parameters.AudioOut.Channels = 1
	}
}

