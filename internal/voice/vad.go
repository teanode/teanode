package voice

import (
	"encoding/binary"
	"math"
)

const (
	// Tuned to reduce false positives and self-interruptions from playback leakage.
	vadPositiveThreshold = 0.04
	vadNegativeThreshold = 0.02
	vadMinSpeechFrames   = 10
	vadRedemptionFrames  = 16
)

// VADAnalyzer is the runtime speech boundary detector used by the pipeline.
type VADAnalyzer interface {
	ProcessFrame(pcm []byte) (started bool, ended bool, score float64)
}

// EnergyVAD tracks simple energy-based speech state.
type EnergyVAD struct {
	IsSpeaking      bool
	speechFrames    int
	redemptionCount int
}

// Reset clears all transient VAD counters.
func (self *EnergyVAD) Reset() {
	self.IsSpeaking = false
	self.speechFrames = 0
	self.redemptionCount = 0
}

// ProcessFrame processes one s16le frame and returns (started, ended, rms).
func (self *EnergyVAD) ProcessFrame(pcm []byte) (bool, bool, float64) {
	rms := rmsS16Le(pcm)

	started := false
	ended := false
	if !self.IsSpeaking {
		if rms >= vadPositiveThreshold {
			self.speechFrames++
			if self.speechFrames >= vadMinSpeechFrames {
				self.IsSpeaking = true
				self.redemptionCount = 0
				started = true
			}
		} else {
			self.speechFrames = 0
		}
		return started, ended, rms
	}
	self.speechFrames = 0

	if rms < vadNegativeThreshold {
		self.redemptionCount++
		if self.redemptionCount >= vadRedemptionFrames {
			self.IsSpeaking = false
			self.speechFrames = 0
			self.redemptionCount = 0
			ended = true
		}
	} else {
		self.redemptionCount = 0
	}

	return started, ended, rms
}

func rmsS16Le(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}
	samples := len(pcm) / 2
	var sum float64
	for i := 0; i < samples; i++ {
		raw := int16(binary.LittleEndian.Uint16(pcm[i*2 : i*2+2]))
		n := float64(raw) / 32768.0
		sum += n * n
	}
	return math.Sqrt(sum / float64(samples))
}
