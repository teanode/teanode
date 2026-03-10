package v1api

import (
	"encoding/json"
	"sort"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/voice"
)

func (self *webSocketConnection) handleVoiceStart(frame requestFrame) (interface{}, error) {
	var parameters voiceStartParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		log.Warningf("voice.start invalid parameters: %v", err)
		return nil, rpcError(400, "invalid parameters: "+err.Error())
	}

	applyVoiceDefaults(&parameters)
	if err := validateVoiceAudioFormats(parameters.AudioIn, parameters.AudioOut); err != nil {
		log.Warningf("voice.start format validation failed: %v", err)
		return nil, rpcError(400, err.Error())
	}
	log.Infof("voice.start requested: agent=%s conv=%s pipeline=%s in=%s/%dHz/%dch out=%s/%dHz/%dch features[vad=%v turn=%v barge=%v strategy=%s]",
		parameters.AgentID, parameters.ConversationID, parameters.Pipeline,
		parameters.AudioIn.Codec, parameters.AudioIn.SampleRateHz, parameters.AudioIn.Channels,
		parameters.AudioOut.Codec, parameters.AudioOut.SampleRateHz, parameters.AudioOut.Channels,
		parameters.Features.ServerVAD, parameters.Features.ServerTurn, parameters.Features.BargeIn, parameters.Features.TurnStrategy,
	)

	if self.getActiveVoiceSession() != nil {
		log.Warningf("voice.start conflict: active session already exists")
		return nil, rpcError(409, "voice session already active")
	}

	// Resolve user and agent.
	user := models.UserFromContext(self.ctx)
	if user == nil || user.ID == "" {
		return nil, rpcError(401, "userId is required")
	}
	agentId := user.GetDefaultAgentID()
	if agentId == "" {
		return nil, rpcError(500, "no default agent configured")
	}

	// Resolve or create conversation. Voice calls should not change the default
	// conversation — they use a dedicated conversation that may be temporary.
	conversationId := parameters.ConversationID
	if conversationId == "" {
		conversationId = self.api.coordinator.NewConversation(user.ID, agentId)
	}

	audioIn := voice.AudioFormat{
		Codec:        parameters.AudioIn.Codec,
		SampleRateHz: parameters.AudioIn.SampleRateHz,
		Channels:     parameters.AudioIn.Channels,
		FrameMS:      parameters.AudioIn.FrameMilliseconds,
	}
	audioOut := voice.AudioFormat{
		Codec:        parameters.AudioOut.Codec,
		SampleRateHz: parameters.AudioOut.SampleRateHz,
		Channels:     parameters.AudioOut.Channels,
		FrameMS:      parameters.AudioOut.FrameMilliseconds,
	}

	sessionId := security.NewULID()
	pipeline := parameters.Pipeline

	// Realtime pipeline: use OpenAI Realtime API.
	if pipeline == "realtime" {
		return self.startRealtimeVoiceSession(sessionId, conversationId, agentId, audioIn, audioOut, parameters)
	}

	// Default: classic pipeline (STT → LLM → TTS).
	if pipeline == "" {
		pipeline = "classic"
	}
	features := voice.Features{
		ServerVAD:    parameters.Features.ServerVAD,
		ServerTurn:   parameters.Features.ServerTurn,
		BargeIn:      parameters.Features.BargeIn,
		TurnStrategy: parameters.Features.TurnStrategy,
	}

	session := voice.NewSession(
		sessionId,
		conversationId,
		agentId,
		audioIn,
		audioOut,
		features,
		self.api.coordinator,
		self.api.pubsub,
		func(payload interface{}) { self.writeJSON(payload) },
		func(data []byte) { self.writeBinary(data) },
	)
	if !self.setActiveVoiceSession(&voice.SessionAdapter{Session: session}) {
		log.Warningf("voice.start race conflict while setting active session")
		return nil, rpcError(409, "voice session already active")
	}

	session.Start()
	log.Infof("voice.start session ready: session=%s conv=%s pipeline=classic", session.ID, session.ConversationID)
	return voiceSessionReadyPayload{
		SessionID:      session.ID,
		ConversationID: session.ConversationID,
		AudioOut:       parameters.AudioOut,
		Pipeline:       "classic",
		Features: voiceFeatures{
			ServerVAD:    features.ServerVAD,
			ServerTurn:   features.ServerTurn,
			BargeIn:      features.BargeIn,
			TurnStrategy: features.TurnStrategy,
		},
	}, nil
}

func (self *webSocketConnection) startRealtimeVoiceSession(
	sessionId, conversationId, agentId string,
	audioIn, audioOut voice.AudioFormat,
	parameters voiceStartParameters,
) (interface{}, error) {
	providerRegistry := self.api.coordinator.ProviderRegistry()
	if providerRegistry == nil {
		return nil, rpcError(500, "provider registry unavailable")
	}
	realtimeProvider, providerName, ok := providerRegistry.FindRealtimeProvider()
	if !ok {
		return nil, rpcError(400, "no realtime provider configured")
	}

	// Dial the Realtime API.
	conn, err := realtimeProvider.DialRealtime(self.ctx, providers.RealtimeSessionConfig{
		Voice:             "alloy",
		Modalities:        []string{"text", "audio"},
		InputAudioFormat:  "pcm16",
		OutputAudioFormat: "pcm16",
	})
	if err != nil {
		log.Warningf("voice.start realtime dial failed: provider=%s err=%v", providerName, err)
		return nil, rpcError(502, "failed to connect to realtime API: "+err.Error())
	}

	// Create the voice session first to get callbacks for the runner.
	session := voice.NewRealtimeSession(
		sessionId,
		conversationId,
		agentId,
		audioIn,
		audioOut,
		nil, // runner set below
		func(payload interface{}) { self.writeJSON(payload) },
		func(data []byte) { self.writeBinary(data) },
	)

	// Create a RealtimeRunner via the coordinator — it builds the tool registry
	// from the agent configuration and handles tool execution directly.
	callbacks := session.SetupCallbacks()
	userId := ""
	if contextUser := models.UserFromContext(self.ctx); contextUser != nil {
		userId = contextUser.ID
	}
	runner, err := self.api.coordinator.CreateRealtimeRunner(userId, agentId, conversationId, conn, callbacks)
	if err != nil {
		conn.Close()
		log.Warningf("voice.start realtime runner creation failed: %v", err)
		return nil, rpcError(500, "failed to create realtime runner: "+err.Error())
	}
	session.SetRunner(runner)

	if !self.setActiveVoiceSession(session) {
		conn.Close()
		log.Warningf("voice.start race conflict while setting active realtime session")
		return nil, rpcError(409, "voice session already active")
	}

	if err := session.Start("You are a helpful voice assistant.", "alloy"); err != nil {
		session.Close()
		self.clearActiveVoiceSession(session)
		return nil, rpcError(502, "failed to start realtime session: "+err.Error())
	}

	log.Infof("voice.start session ready: session=%s conv=%s pipeline=realtime provider=%s", session.ID, session.ConversationID, providerName)
	return voiceSessionReadyPayload{
		SessionID:      session.ID,
		ConversationID: session.ConversationID,
		AudioOut:       parameters.AudioOut,
		Pipeline:       "realtime",
		Features: voiceFeatures{
			ServerVAD:  true,
			ServerTurn: true,
			BargeIn:    true,
		},
	}, nil
}

func (self *webSocketConnection) handleVoiceEnd(frame requestFrame) (interface{}, error) {
	var parameters voiceEndParameters
	if len(frame.Params) > 0 {
		if err := json.Unmarshal(frame.Params, &parameters); err != nil {
			log.Warningf("voice.end invalid parameters: %v", err)
			return nil, rpcError(400, "invalid parameters: "+err.Error())
		}
	}
	session := self.getActiveVoiceSession()
	if session == nil {
		log.Warningf("voice.end without active session")
		return nil, rpcError(404, "no active voice session")
	}
	if parameters.SessionID != "" && parameters.SessionID != session.SessionID() {
		log.Warningf("voice.end session mismatch: requested=%s active=%s", parameters.SessionID, session.SessionID())
		return nil, rpcError(404, "voice session not found")
	}
	log.Infof("voice.end closing session=%s", session.SessionID())
	session.Close()
	self.clearActiveVoiceSession(session)
	return map[string]any{"ended": true}, nil
}

func (self *webSocketConnection) handleVoiceResponseCancel(frame requestFrame) (interface{}, error) {
	var parameters voiceResponseCancelParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		log.Warningf("voice.response.cancel invalid parameters: %v", err)
		return nil, rpcError(400, "invalid parameters: "+err.Error())
	}
	session := self.getActiveVoiceSession()
	if session == nil {
		log.Warningf("voice.response.cancel without active session")
		return nil, rpcError(404, "no active voice session")
	}
	log.Infof("voice.response.cancel session=%s response=%s reason=%s", session.SessionID(), parameters.ResponseID, parameters.Reason)
	session.CancelResponse()
	return map[string]any{"cancelled": true}, nil
}

func (self *webSocketConnection) handleVoiceInputCommit(frame requestFrame) (interface{}, error) {
	var parameters voiceInputCommitParameters
	if len(frame.Params) > 0 {
		if err := json.Unmarshal(frame.Params, &parameters); err != nil {
			log.Warningf("voice.input.commit invalid parameters: %v", err)
			return nil, rpcError(400, "invalid parameters: "+err.Error())
		}
	}
	session := self.getActiveVoiceSession()
	if session == nil {
		log.Warningf("voice.input.commit without active session")
		return nil, rpcError(404, "no active voice session")
	}
	log.Infof("voice.input.commit session=%s reason=%s", session.SessionID(), parameters.Reason)
	session.InputCommit(parameters.Reason)
	return map[string]any{"committed": true}, nil
}

func (self *webSocketConnection) handleVoiceProviders(frame requestFrame) (interface{}, error) {
	providerRegistry := self.api.coordinator.ProviderRegistry()
	var transcribers, streamingTranscribers, synthesizers, streamingSynthesizers, realtimeProviders []string
	if providerRegistry != nil {
		for _, name := range providerRegistry.ProviderNames() {
			client, ok := providerRegistry.ClientByName(name)
			if !ok {
				continue
			}
			if _, ok := client.(providers.TranscribeProvider); ok {
				transcribers = append(transcribers, name)
			}
			if _, ok := client.(providers.StreamingTranscribeProvider); ok {
				streamingTranscribers = append(streamingTranscribers, name)
			}
			if _, ok := client.(providers.SynthesizeProvider); ok {
				synthesizers = append(synthesizers, name)
			}
			if _, ok := client.(providers.StreamingSynthesizeProvider); ok {
				streamingSynthesizers = append(streamingSynthesizers, name)
			}
			if _, ok := client.(providers.RealtimeProvider); ok {
				realtimeProviders = append(realtimeProviders, name)
			}
		}
		sort.Strings(transcribers)
		sort.Strings(streamingTranscribers)
		sort.Strings(synthesizers)
		sort.Strings(streamingSynthesizers)
		sort.Strings(realtimeProviders)
	}
	return map[string]any{
		"transcribers":          orEmptySlice(transcribers),
		"streamingTranscribers": orEmptySlice(streamingTranscribers),
		"synthesizers":          orEmptySlice(synthesizers),
		"streamingSynthesizers": orEmptySlice(streamingSynthesizers),
		"realtimeProviders":     orEmptySlice(realtimeProviders),
	}, nil
}

func orEmptySlice(slice []string) []string {
	if slice == nil {
		return []string{}
	}
	return slice
}

func applyVoiceDefaults(parameters *voiceStartParameters) {
	if parameters.AudioIn.Codec == "" {
		parameters.AudioIn.Codec = "pcm_s16le"
	}
	if parameters.AudioIn.SampleRateHz == 0 {
		parameters.AudioIn.SampleRateHz = 16000
	}
	if parameters.AudioIn.Channels == 0 {
		parameters.AudioIn.Channels = 1
	}
	if parameters.AudioIn.FrameMilliseconds == 0 {
		parameters.AudioIn.FrameMilliseconds = 20
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
