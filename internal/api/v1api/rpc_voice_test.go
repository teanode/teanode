package v1api

import (
	"testing"

	"github.com/teanode/teanode/internal/voice"
)

func TestVoiceStartSecondSessionConflict(t *testing.T) {
	s := &voice.Session{ID: "sess_1"}
	if !isVoiceStartConflict(s) {
		t.Fatal("expected second voice.start to be conflict")
	}
}

func TestVoiceStartRejectsMP3OutputCodec(t *testing.T) {
	err := validateVoiceAudioFormats(
		voiceAudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
		voiceAudioFormat{Codec: "mp3", SampleRateHz: 24000, Channels: 1},
	)
	if err == nil {
		t.Fatal("expected validation error for mp3 output codec")
	}
}

func TestVoiceEndWithoutActiveSessionReturnsNotFound(t *testing.T) {
	if !isVoiceEndNotFound(nil) {
		t.Fatal("expected voice.end without active session to be not found")
	}
}
