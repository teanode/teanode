package v1api

import (
	"encoding/json"

	"github.com/teanode/teanode/internal/voice"
)

func (self *webSocketConnection) handleVoiceStart(frame requestFrame) {
	var params voiceStartParams
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		log.Warningf("voice.start invalid params: %v", err)
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	applyVoiceDefaults(&params)
	if err := validateVoiceAudioFormats(params.AudioIn, params.AudioOut); err != nil {
		log.Warningf("voice.start format validation failed: %v", err)
		self.sendError(frame.ID, 400, err.Error())
		return
	}
	log.Infof("voice.start requested: agent=%s conv=%s in=%s/%dHz/%dch out=%s/%dHz/%dch features[vad=%v turn=%v denoise=%v barge=%v]",
		params.AgentID, params.ConversationID,
		params.AudioIn.Codec, params.AudioIn.SampleRateHz, params.AudioIn.Channels,
		params.AudioOut.Codec, params.AudioOut.SampleRateHz, params.AudioOut.Channels,
		params.Features.ServerVAD, params.Features.ServerTurn, params.Features.ServerDenoise, params.Features.BargeIn,
	)

	if isVoiceStartConflict(self.getActiveVoiceSession()) {
		log.Warningf("voice.start conflict: active session already exists")
		self.sendError(frame.ID, 409, "voice session already active")
		return
	}

	audioIn := voice.AudioFormat{
		Codec:        params.AudioIn.Codec,
		SampleRateHz: params.AudioIn.SampleRateHz,
		Channels:     params.AudioIn.Channels,
		FrameMS:      params.AudioIn.FrameMS,
	}
	audioOut := voice.AudioFormat{
		Codec:        params.AudioOut.Codec,
		SampleRateHz: params.AudioOut.SampleRateHz,
		Channels:     params.AudioOut.Channels,
		FrameMS:      params.AudioOut.FrameMS,
	}
	features := voice.Features{
		ServerVAD:     params.Features.ServerVAD,
		ServerTurn:    params.Features.ServerTurn,
		ServerDenoise: params.Features.ServerDenoise,
		BargeIn:       params.Features.BargeIn,
	}

	session, err := self.api.gateway.StartVoiceSession(
		params.ConversationID,
		params.AgentID,
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
		AudioOut:       params.AudioOut,
		Features:       params.Features,
	})
}

func (self *webSocketConnection) handleVoiceEnd(frame requestFrame) {
	var params voiceEndParams
	if len(frame.Params) > 0 {
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			log.Warningf("voice.end invalid params: %v", err)
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
	if params.SessionID != "" && params.SessionID != session.ID {
		log.Warningf("voice.end session mismatch: requested=%s active=%s", params.SessionID, session.ID)
		self.sendError(frame.ID, 404, "voice session not found")
		return
	}
	log.Infof("voice.end closing session=%s", session.ID)
	session.Close()
	self.clearActiveVoiceSession(session)
	self.sendResponse(frame.ID, map[string]any{"ended": true})
}

func (self *webSocketConnection) handleVoiceResponseCancel(frame requestFrame) {
	var params voiceResponseCancelParams
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		log.Warningf("voice.response.cancel invalid params: %v", err)
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	session := self.getActiveVoiceSession()
	if session == nil {
		log.Warningf("voice.response.cancel without active session")
		self.sendError(frame.ID, 404, "no active voice session")
		return
	}
	log.Infof("voice.response.cancel session=%s response=%s reason=%s", session.ID, params.ResponseID, params.Reason)
	session.CancelResponse()
	self.sendResponse(frame.ID, map[string]any{"cancelled": true})
}

func (self *webSocketConnection) handleVoiceInputCommit(frame requestFrame) {
	var params voiceInputCommitParams
	if len(frame.Params) > 0 {
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			log.Warningf("voice.input.commit invalid params: %v", err)
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
	log.Infof("voice.input.commit session=%s reason=%s", session.ID, params.Reason)
	session.InputCommit()
	self.sendResponse(frame.ID, map[string]any{"committed": true})
}

func applyVoiceDefaults(params *voiceStartParams) {
	if params.AudioIn.Codec == "" {
		params.AudioIn.Codec = "pcm_s16le"
	}
	if params.AudioIn.SampleRateHz == 0 {
		params.AudioIn.SampleRateHz = 16000
	}
	if params.AudioIn.Channels == 0 {
		params.AudioIn.Channels = 1
	}
	if params.AudioIn.FrameMS == 0 {
		params.AudioIn.FrameMS = 20
	}

	if params.AudioOut.Codec == "" {
		params.AudioOut.Codec = "pcm_s16le"
	}
	if params.AudioOut.SampleRateHz == 0 {
		params.AudioOut.SampleRateHz = 24000
	}
	if params.AudioOut.Channels == 0 {
		params.AudioOut.Channels = 1
	}
}

func isVoiceStartConflict(active *voice.Session) bool {
	return active != nil
}

func isVoiceEndNotFound(active *voice.Session) bool {
	return active == nil
}
