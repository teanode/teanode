package voice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnergyVAD_Implements_VADAnalyzer(t *testing.T) {
	var _ VADAnalyzer = &EnergyVAD{}
}

func TestSileroVAD_SpeechStart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]float64{"prob": 0.95})
	}))
	defer server.Close()

	vad, err := NewSileroVAD(server.URL)
	if err != nil {
		t.Fatalf("NewSileroVAD error: %v", err)
	}
	frame := makeFrame(12000, 320)
	startedAt := -1
	for i := 0; i < 20; i++ {
		started, _, _ := vad.ProcessFrame(frame)
		if started {
			startedAt = i
			break
		}
	}
	if startedAt < 0 {
		t.Fatal("expected speech start")
	}
}

func TestSileroVAD_SpeechEnd(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		count++
		prob := 0.95
		if count > 12 {
			prob = 0.01
		}
		_ = json.NewEncoder(writer).Encode(map[string]float64{"prob": prob})
	}))
	defer server.Close()

	vad, err := NewSileroVAD(server.URL)
	if err != nil {
		t.Fatalf("NewSileroVAD error: %v", err)
	}
	frame := makeFrame(12000, 320)
	ended := false
	for i := 0; i < 40; i++ {
		_, end, _ := vad.ProcessFrame(frame)
		if end {
			ended = true
			break
		}
	}
	if !ended {
		t.Fatal("expected speech end")
	}
}
