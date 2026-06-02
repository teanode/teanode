package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiTranscribe(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.URL.Path, ":generateContent") {
			t.Errorf("unexpected path: %s", request.URL.Path)
		}
		capturedBody, _ = io.ReadAll(request.Body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"  hello world  "}]}}]}`))
	}))
	defer server.Close()

	client := NewGeminiClient(server.URL, "test-key")
	response, err := client.Transcribe(context.Background(), TranscribeRequest{
		Audio:    strings.NewReader("fake-audio-bytes"),
		Format:   "mp3",
		Language: "en",
	})
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if response.Text != "hello world" {
		t.Errorf("Text = %q, want %q", response.Text, "hello world")
	}

	// Verify the request carried inline audio data with the expected MIME type.
	var sent geminiRequest
	if err := json.Unmarshal(capturedBody, &sent); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if len(sent.Contents) != 1 || len(sent.Contents[0].Parts) != 2 {
		t.Fatalf("unexpected request contents: %+v", sent.Contents)
	}
	inline := sent.Contents[0].Parts[1].InlineData
	if inline == nil {
		t.Fatalf("expected inline audio data in request")
	}
	if inline.MimeType != "audio/mp3" {
		t.Errorf("MimeType = %q, want %q", inline.MimeType, "audio/mp3")
	}
	decoded, err := base64.StdEncoding.DecodeString(inline.Data)
	if err != nil {
		t.Fatalf("decode inline data: %v", err)
	}
	if string(decoded) != "fake-audio-bytes" {
		t.Errorf("decoded audio = %q, want %q", string(decoded), "fake-audio-bytes")
	}
}

func TestGeminiSynthesize(t *testing.T) {
	pcm := []byte{0x01, 0x02, 0x03, 0x04}
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		capturedBody, _ = io.ReadAll(request.Body)
		writer.Header().Set("Content-Type", "application/json")
		payload := geminiResponse{
			Candidates: []geminiCandidate{{
				Content: geminiContent{
					Parts: []geminiPart{{
						InlineData: &geminiInlineData{
							MimeType: "audio/L16;rate=24000",
							Data:     base64.StdEncoding.EncodeToString(pcm),
						},
					}},
				},
			}},
		}
		_ = json.NewEncoder(writer).Encode(payload)
	}))
	defer server.Close()

	client := NewGeminiClient(server.URL, "test-key")
	response, err := client.Synthesize(context.Background(), SynthesizeRequest{
		Text:  "hello",
		Voice: "alloy",
	})
	if err != nil {
		t.Fatalf("Synthesize returned error: %v", err)
	}
	defer func() { _ = response.Audio.Close() }()

	// Raw PCM is wrapped in a WAV container so browser <audio> can play it.
	if response.Format != "wav" {
		t.Errorf("Format = %q, want %q", response.Format, "wav")
	}
	if response.ContentType != "audio/wav" {
		t.Errorf("ContentType = %q, want %q", response.ContentType, "audio/wav")
	}
	audio, err := io.ReadAll(response.Audio)
	if err != nil {
		t.Fatalf("read audio: %v", err)
	}
	if len(audio) != 44+len(pcm) {
		t.Fatalf("audio length = %d, want %d (44-byte WAV header + %d PCM bytes)", len(audio), 44+len(pcm), len(pcm))
	}
	if string(audio[0:4]) != "RIFF" || string(audio[8:12]) != "WAVE" {
		t.Errorf("audio is not a WAV container: %q", string(audio[0:12]))
	}
	if !bytes.Equal(audio[44:], pcm) {
		t.Errorf("WAV payload = %v, want %v", audio[44:], pcm)
	}

	// "alloy" is an OpenAI voice name; it should fall back to the default Gemini voice.
	var sent geminiRequest
	if err := json.Unmarshal(capturedBody, &sent); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if sent.GenerationConfig == nil || sent.GenerationConfig.SpeechConfig == nil {
		t.Fatalf("expected speech config in request: %+v", sent.GenerationConfig)
	}
	voiceName := sent.GenerationConfig.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName
	if voiceName != geminiDefaultVoice {
		t.Errorf("VoiceName = %q, want %q", voiceName, geminiDefaultVoice)
	}
	if len(sent.GenerationConfig.ResponseModalities) != 1 || sent.GenerationConfig.ResponseModalities[0] != "AUDIO" {
		t.Errorf("ResponseModalities = %v, want [AUDIO]", sent.GenerationConfig.ResponseModalities)
	}
}

func TestGeminiSynthesizeNoAudio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"no audio here"}]}}]}`))
	}))
	defer server.Close()

	client := NewGeminiClient(server.URL, "test-key")
	_, err := client.Synthesize(context.Background(), SynthesizeRequest{Text: "hello"})
	if err == nil {
		t.Fatalf("expected error when response has no audio")
	}
}

func TestGeminiSynthesizeEmptyText(t *testing.T) {
	client := NewGeminiClient("http://example.invalid", "test-key")
	_, err := client.Synthesize(context.Background(), SynthesizeRequest{Text: "  "})
	if err == nil {
		t.Fatalf("expected error for empty text")
	}
}

func TestGeminiTranscribeAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":"bad audio"}`))
	}))
	defer server.Close()

	client := NewGeminiClient(server.URL, "test-key")
	_, err := client.Transcribe(context.Background(), TranscribeRequest{
		Audio:  strings.NewReader("x"),
		Format: "wav",
	})
	if err == nil {
		t.Fatalf("expected error on API failure")
	}
}

func TestGeminiAudioMimeType(t *testing.T) {
	cases := map[string]string{
		"mp3":     "audio/mp3",
		"wav":     "audio/wav",
		"m4a":     "audio/mp4",
		"webm":    "audio/webm",
		"flac":    "audio/flac",
		"":        "audio/mp3",
		"unknown": "audio/mp3",
	}
	for format, want := range cases {
		if got := geminiAudioMimeType(format); got != want {
			t.Errorf("geminiAudioMimeType(%q) = %q, want %q", format, got, want)
		}
	}
}

func TestGeminiVoiceName(t *testing.T) {
	if got := geminiVoiceName(""); got != geminiDefaultVoice {
		t.Errorf("geminiVoiceName(empty) = %q, want %q", got, geminiDefaultVoice)
	}
	if got := geminiVoiceName("nova"); got != geminiDefaultVoice {
		t.Errorf("geminiVoiceName(nova) = %q, want %q", got, geminiDefaultVoice)
	}
	if got := geminiVoiceName("Puck"); got != "Puck" {
		t.Errorf("geminiVoiceName(Puck) = %q, want %q", got, "Puck")
	}
}

// Ensure GeminiClient satisfies the audio capability interfaces.
var (
	_ TranscribeProvider = (*GeminiClient)(nil)
	_ SynthesizeProvider = (*GeminiClient)(nil)
)
