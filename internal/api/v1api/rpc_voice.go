package v1api

import (
	"encoding/json"

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

	if isVoiceStartConflict(self.getActiveVoiceSession()) {
		log.Warningf("voice.start conflict: active session already exists")
		self.sendError(frame.ID, 409, "voice session already active")
		return
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

	session, err := self.api.gateway.StartVoiceSession(
		parameters.ConversationID,
		parameters.AgentID,
		parameters.PromptSuffix,
		audioIn,
		audioOut,
		features,
		func(payload interface{}) { self.writeJSON(payload) },
		func(data []byte) { self.writeBinary(data) },
	)
	if err != nil {
		log.Errorf("voice.start failed to create session: %v", err)
		self.sendError(frame.ID, 500, "failed to start voice session: "+err.Error())
		return
	}
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
	if isVoiceEndNotFound(session) {
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

func isVoiceStartConflict(active *voice.Session) bool {
	return active != nil
}

func isVoiceEndNotFound(active *voice.Session) bool {
	return active == nil
}
