package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Models used for Gemini batch audio. Transcription uses a standard
// multimodal model; speech synthesis uses a dedicated TTS preview model.
const (
	geminiTranscriptionModel = "gemini-2.5-flash"
	geminiSpeechModel        = "gemini-2.5-flash-preview-tts"
	geminiDefaultVoice       = "Kore"

	// Gemini TTS produces mono 16-bit PCM at 24kHz.
	geminiSpeechSampleRate    = 24000
	geminiSpeechChannels      = 1
	geminiSpeechBitsPerSample = 16
)

// Transcribe performs batch speech-to-text using the Gemini generateContent
// API with inline audio data.
func (self *GeminiClient) Transcribe(ctx context.Context, request TranscribeRequest) (*TranscribeResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultNonStreamingRequestTimeout)
	defer cancel()

	audio, err := io.ReadAll(request.Audio)
	if err != nil {
		return nil, fmt.Errorf("providers: reading audio data: %w", err)
	}

	prompt := "Generate a verbatim transcript of the speech in this audio. Return only the transcript text with no commentary."
	if request.Language != "" {
		prompt = fmt.Sprintf("%s The spoken language is %s.", prompt, request.Language)
	}
	if request.Prompt != "" {
		prompt = fmt.Sprintf("%s Context: %s", prompt, request.Prompt)
	}

	geminiRequest := geminiRequest{
		Contents: []geminiContent{{
			Role: "user",
			Parts: []geminiPart{
				{Text: prompt},
				{InlineData: &geminiInlineData{
					MimeType: geminiAudioMimeType(request.Format),
					Data:     base64.StdEncoding.EncodeToString(audio),
				}},
			},
		}},
	}

	geminiResponse, err := self.generateContent(ctx, geminiTranscriptionModel, geminiRequest)
	if err != nil {
		return nil, err
	}

	var textParts []string
	if len(geminiResponse.Candidates) > 0 {
		for _, part := range geminiResponse.Candidates[0].Content.Parts {
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
		}
	}

	return &TranscribeResponse{Text: strings.TrimSpace(strings.Join(textParts, ""))}, nil
}

// Synthesize performs batch text-to-speech using the Gemini generateContent
// API. The returned audio is raw 16-bit PCM at 24kHz (mono), as produced by
// the Gemini TTS models.
func (self *GeminiClient) Synthesize(ctx context.Context, request SynthesizeRequest) (*SynthesizeResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultNonStreamingRequestTimeout)
	defer cancel()

	if strings.TrimSpace(request.Text) == "" {
		return nil, fmt.Errorf("providers: synthesize text is required")
	}

	geminiRequest := geminiRequest{
		Contents: []geminiContent{{
			Role:  "user",
			Parts: []geminiPart{{Text: request.Text}},
		}},
		GenerationConfig: &geminiGenerationConfig{
			ResponseModalities: []string{"AUDIO"},
			SpeechConfig: &geminiSpeechConfig{
				VoiceConfig: &geminiVoiceConfig{
					PrebuiltVoiceConfig: &geminiPrebuiltVoiceConfig{
						VoiceName: geminiVoiceName(request.Voice),
					},
				},
			},
		},
	}

	geminiResponse, err := self.generateContent(ctx, geminiSpeechModel, geminiRequest)
	if err != nil {
		return nil, err
	}

	var inlineData *geminiInlineData
	if len(geminiResponse.Candidates) > 0 {
		for _, part := range geminiResponse.Candidates[0].Content.Parts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				inlineData = part.InlineData
				break
			}
		}
	}
	if inlineData == nil {
		return nil, fmt.Errorf("providers: gemini synthesis returned no audio")
	}

	pcm, err := base64.StdEncoding.DecodeString(inlineData.Data)
	if err != nil {
		return nil, fmt.Errorf("providers: decoding synthesized audio: %w", err)
	}

	// Gemini returns headerless 16-bit PCM (e.g. "audio/L16;rate=24000"). Wrap
	// it in a WAV container so it is playable by browser <audio> elements, which
	// cannot decode raw PCM.
	sampleRate := geminiPcmSampleRate(inlineData.MimeType)
	audio := wrapPcmAsWav(pcm, sampleRate, geminiSpeechChannels, geminiSpeechBitsPerSample)

	return &SynthesizeResponse{
		Audio:       io.NopCloser(bytes.NewReader(audio)),
		Format:      "wav",
		ContentType: "audio/wav",
	}, nil
}

// generateContent posts a non-streaming generateContent request and decodes the response.
func (self *GeminiClient) generateContent(ctx context.Context, modelName string, request geminiRequest) (*geminiResponse, error) {
	body, _ := json.Marshal(request)

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", self.baseUrl, modelName, self.apiKey)
	log.Debugf("POST %s/v1beta/models/%s:generateContent (audio)", self.baseUrl, modelName)

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("providers: creating request: %w", err)
	}
	self.setHeaders(httpRequest)

	response, err := self.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("providers: sending request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("providers: API error %d: %s", response.StatusCode, string(responseBody))
	}

	var decoded geminiResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("providers: decoding response: %w", err)
	}

	return &decoded, nil
}

// geminiAudioMimeType maps a TranscribeRequest format hint to a Gemini-supported
// audio MIME type, defaulting to audio/mp3.
func geminiAudioMimeType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		return "audio/wav"
	case "mp3", "mpeg", "mpga":
		return "audio/mp3"
	case "aac":
		return "audio/aac"
	case "ogg", "opus":
		return "audio/ogg"
	case "flac":
		return "audio/flac"
	case "aiff":
		return "audio/aiff"
	case "m4a", "mp4":
		return "audio/mp4"
	case "webm":
		return "audio/webm"
	default:
		return "audio/mp3"
	}
}

// geminiPcmSampleRate extracts the sample rate (Hz) from a PCM MIME type such as
// "audio/L16;rate=24000", falling back to the Gemini default when absent.
func geminiPcmSampleRate(mimeType string) int {
	for _, segment := range strings.Split(mimeType, ";") {
		segment = strings.TrimSpace(segment)
		if rate, ok := strings.CutPrefix(strings.ToLower(segment), "rate="); ok {
			if parsed, err := strconv.Atoi(strings.TrimSpace(rate)); err == nil && parsed > 0 {
				return parsed
			}
		}
	}
	return geminiSpeechSampleRate
}

// wrapPcmAsWav prepends a 44-byte canonical RIFF/WAVE header to raw little-endian
// PCM samples, producing a self-contained WAV file.
func wrapPcmAsWav(pcm []byte, sampleRate, channels, bitsPerSample int) []byte {
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	var buffer bytes.Buffer
	buffer.Grow(44 + len(pcm))

	buffer.WriteString("RIFF")
	_ = binary.Write(&buffer, binary.LittleEndian, uint32(36+len(pcm)))
	buffer.WriteString("WAVE")

	buffer.WriteString("fmt ")
	_ = binary.Write(&buffer, binary.LittleEndian, uint32(16)) // PCM fmt chunk size
	_ = binary.Write(&buffer, binary.LittleEndian, uint16(1))  // audio format: PCM
	_ = binary.Write(&buffer, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&buffer, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buffer, binary.LittleEndian, uint32(byteRate))
	_ = binary.Write(&buffer, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&buffer, binary.LittleEndian, uint16(bitsPerSample))

	buffer.WriteString("data")
	_ = binary.Write(&buffer, binary.LittleEndian, uint32(len(pcm)))
	buffer.Write(pcm)

	return buffer.Bytes()
}

// geminiVoiceName resolves a requested voice to a Gemini prebuilt voice name.
// OpenAI-style voice names (e.g. "alloy") and empty values fall back to the
// default Gemini voice.
func geminiVoiceName(voice string) string {
	trimmed := strings.TrimSpace(voice)
	switch strings.ToLower(trimmed) {
	case "", "alloy", "echo", "fable", "onyx", "nova", "shimmer", "default":
		return geminiDefaultVoice
	default:
		return trimmed
	}
}
