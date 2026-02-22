package voice

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultSileroEndpoint = "http://127.0.0.1:8081/vad/silero"
	sileroPositiveProb    = 0.6
	sileroNegativeProb    = 0.35
)

type SileroVAD struct {
	endpoint        string
	client          *http.Client
	IsSpeaking      bool
	speechFrames    int
	redemptionCount int
}

func NewSileroVAD(endpoint string) (*SileroVAD, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, errors.New("empty silero endpoint")
	}
	if _, err := url.ParseRequestURI(endpoint); err != nil {
		return nil, fmt.Errorf("invalid silero endpoint: %w", err)
	}
	return &SileroVAD{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
	}, nil
}

func sileroEndpoint() string {
	if value := strings.TrimSpace(os.Getenv("TEANODE_SILERO_URL")); value != "" {
		return value
	}
	return defaultSileroEndpoint
}

func (v *SileroVAD) ProcessFrame(pcm []byte) (bool, bool, float64) {
	prob, err := v.scoreFrame(pcm)
	if err != nil {
		return false, false, 0
	}

	started := false
	ended := false
	if !v.IsSpeaking {
		if prob >= sileroPositiveProb {
			v.speechFrames++
			if v.speechFrames >= vadMinSpeechFrames {
				v.IsSpeaking = true
				v.redemptionCount = 0
				started = true
			}
		} else {
			v.speechFrames = 0
		}
		return started, ended, prob
	}
	v.speechFrames = 0

	if prob < sileroNegativeProb {
		v.redemptionCount++
		if v.redemptionCount >= vadRedemptionFrames {
			v.IsSpeaking = false
			v.redemptionCount = 0
			ended = true
		}
	} else {
		v.redemptionCount = 0
	}
	return started, ended, prob
}

func (v *SileroVAD) scoreFrame(pcm []byte) (float64, error) {
	body, err := json.Marshal(map[string]any{
		"pcm_b64": base64.StdEncoding.EncodeToString(pcm),
		"sr":      16000,
	})
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequest(http.MethodPost, v.endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := v.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return 0, fmt.Errorf("silero sidecar status: %s", response.Status)
	}
	var payload struct {
		Prob float64 `json:"prob"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return 0, err
	}
	return payload.Prob, nil
}
