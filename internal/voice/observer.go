package voice

// TurnObserver receives lifecycle callbacks for a single voice turn.
type TurnObserver interface {
	OnSpeechStarted(turnId string, tsMs int64)
	OnSpeechEnded(turnId string, tsMs int64)
	OnTranscriptFinal(turnId string, tsMs int64)
	OnTurnCommitted(turnId string, tsMs int64)
	OnTTSRequested(turnId string, tsMs int64)
	OnResponseStarted(turnId string, responseId string, tsMs int64)
	OnResponseCompleted(turnId string, responseId string, tsMs int64)
	OnTurnDropped(turnId string, reason string, tsMs int64)
}
