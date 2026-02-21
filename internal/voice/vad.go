package voice

import (
	"encoding/binary"
	"math"
)

const (
	vadPositiveThreshold = 0.015
	vadNegativeThreshold = 0.008
	vadMinSpeechFrames   = 5
	vadRedemptionFrames  = 12
)

// VADState tracks simple energy-based speech state.
type VADState struct {
	IsSpeaking      bool
	speechFrames    int
	redemptionCount int
}

// ProcessFrame processes one s16le frame and returns (started, ended, rms).
func (v *VADState) ProcessFrame(pcm []byte) (bool, bool, float64) {
	rms := rmsS16LE(pcm)

	started := false
	ended := false
	if !v.IsSpeaking {
		if rms >= vadPositiveThreshold {
			v.speechFrames++
			if v.speechFrames >= vadMinSpeechFrames {
				v.IsSpeaking = true
				v.redemptionCount = 0
				started = true
			}
		} else {
			v.speechFrames = 0
		}
		return started, ended, rms
	}

	if rms < vadNegativeThreshold {
		v.redemptionCount++
		if v.redemptionCount >= vadRedemptionFrames {
			v.IsSpeaking = false
			v.speechFrames = 0
			v.redemptionCount = 0
			ended = true
		}
	} else {
		v.redemptionCount = 0
	}

	return started, ended, rms
}

func rmsS16LE(pcm []byte) float64 {
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
