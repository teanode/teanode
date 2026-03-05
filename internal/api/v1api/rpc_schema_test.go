package v1api

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/schemas"
)

type schemaMockProvider struct{}

func (self *schemaMockProvider) ChatCompletion(ctx context.Context, request providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{}, nil
}

func (self *schemaMockProvider) ChatCompletionStream(ctx context.Context, request providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	return make(chan providers.StreamEvent), nil
}

func (self *schemaMockProvider) ListModels(ctx context.Context) ([]providers.ModelInformation, error) {
	return nil, nil
}

type schemaMockTranscriber struct{ schemaMockProvider }

func (self *schemaMockTranscriber) Transcribe(ctx context.Context, request providers.TranscribeRequest) (*providers.TranscribeResponse, error) {
	return &providers.TranscribeResponse{Text: "ok"}, nil
}

type schemaMockSynth struct{ schemaMockProvider }

func (self *schemaMockSynth) Synthesize(ctx context.Context, request providers.SynthesizeRequest) (*providers.SynthesizeResponse, error) {
	return &providers.SynthesizeResponse{
		Audio:       io.NopCloser(strings.NewReader("audio")),
		Format:      "wav",
		ContentType: "audio/wav",
	}, nil
}

func getSchemaStringList(t *testing.T, schema map[string]interface{}, path ...string) []string {
	t.Helper()
	var current interface{} = schema
	for _, key := range path {
		asMap, ok := current.(map[string]interface{})
		if !ok {
			t.Fatalf("path %v does not resolve to map at %q", path, key)
		}
		current = asMap[key]
	}
	items, ok := current.([]interface{})
	if !ok {
		t.Fatalf("path %v does not resolve to []interface{}", path)
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("path %v includes non-string value %T", path, item)
		}
		values = append(values, text)
	}
	return values
}

func TestWithVoiceProviderEnums_AddsVoiceProviderEnums(t *testing.T) {
	registry := providers.NewRegistry("openai")
	registry.Register("deepgram", &schemaMockTranscriber{})
	registry.Register("openai", &schemaMockTranscriber{})
	registry.Register("elevenlabs", &schemaMockSynth{})
	registry.Register("openai_tts", &schemaMockSynth{})

	schema := withVoiceProviderEnums(schemas.ConfigSchema(), registry)

	var parsed map[string]interface{}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	transcriberEnum := getSchemaStringList(t, parsed, "properties", "voice", "properties", "transcriber_provider", "enum")
	if len(transcriberEnum) < 3 {
		t.Fatalf("unexpected transcriber enum values: %#v", transcriberEnum)
	}
	if transcriberEnum[0] != "" {
		t.Fatalf("expected first transcriber enum to be auto empty string, got %q", transcriberEnum[0])
	}
	if transcriberEnum[1] != "openai" {
		t.Fatalf("expected openai to be first explicit transcriber provider, got %q", transcriberEnum[1])
	}

	synthEnum := getSchemaStringList(t, parsed, "properties", "voice", "properties", "synth_provider", "enum")
	if len(synthEnum) < 3 {
		t.Fatalf("unexpected synth enum values: %#v", synthEnum)
	}
	if synthEnum[0] != "" {
		t.Fatalf("expected first synth enum to be auto empty string, got %q", synthEnum[0])
	}
}
