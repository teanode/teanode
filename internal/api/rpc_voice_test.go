package api

import (
	"testing"
)

func TestVoiceStartRejectsMP3OutputCodec(t *testing.T) {
	err := validateVoiceAudioFormats(
		voiceAudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
		voiceAudioFormat{Codec: "mp3", SampleRateHz: 24000, Channels: 1},
	)
	if err == nil {
		t.Fatal("expected validation error for mp3 output codec")
	}
}
