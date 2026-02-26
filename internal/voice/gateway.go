package voice

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
)

// voiceDispatcher is the minimal coordinator interface needed by voice sessions.
// *coordinators.Coordinator satisfies this interface.
type voiceDispatcher interface {
	SendMessage(ctx context.Context, parameters coordinators.SendMessageParameters, callbacks *runners.RunCallbacks) (*coordinators.RunHandle, error)
	AbortRunner(runnerId string) bool
	ProviderRegistry() *providers.ProviderRegistry
}

// conversationEventSubscriber filters conversation events by conversationId.
type conversationEventSubscriber struct {
	conversationId string
	eventCh        chan map[string]interface{}
}

func (self *conversationEventSubscriber) OnEvent(eventType pubsub.EventType, payload interface{}) {
	if eventType != pubsub.EventTypeConversation {
		return
	}
	eventMap, ok := payload.(map[string]interface{})
	if !ok {
		return
	}
	conversationId, _ := eventMap["conversationId"].(string)
	if conversationId != self.conversationId {
		return
	}
	state, _ := eventMap["state"].(string)
	critical := state == "final" || state == "error" || state == "aborted" || state == "queued"
	if !critical {
		select {
		case self.eventCh <- eventMap:
		default:
		}
		return
	}

	select {
	case self.eventCh <- eventMap:
	default:
		// Preserve terminal lifecycle events by making room if queue is saturated by deltas.
		select {
		case <-self.eventCh:
		default:
		}
		select {
		case self.eventCh <- eventMap:
		default:
		}
	}
}

// wavToPCM16LE extracts raw PCM16LE samples from a WAV file.
func wavToPCM16LE(wavData []byte) ([]byte, error) {
	if len(wavData) < 44 {
		return nil, fmt.Errorf("wav payload too short")
	}
	if string(wavData[0:4]) != "RIFF" || string(wavData[8:12]) != "WAVE" {
		return nil, fmt.Errorf("invalid wav header")
	}
	var (
		audioFormat   uint16
		channels      uint16
		bitsPerSample uint16
	)
	for index := 12; index+8 <= len(wavData); {
		chunkId := string(wavData[index : index+4])
		chunkSize := int(binary.LittleEndian.Uint32(wavData[index+4 : index+8]))
		chunkStart := index + 8
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(wavData) {
			break
		}
		if chunkId == "fmt " && chunkSize >= 16 {
			audioFormat = binary.LittleEndian.Uint16(wavData[chunkStart : chunkStart+2])
			channels = binary.LittleEndian.Uint16(wavData[chunkStart+2 : chunkStart+4])
			bitsPerSample = binary.LittleEndian.Uint16(wavData[chunkStart+14 : chunkStart+16])
		}
		if chunkId == "data" {
			if audioFormat != 1 {
				return nil, fmt.Errorf("unsupported wav format: %d", audioFormat)
			}
			if channels != 1 {
				return nil, fmt.Errorf("unsupported wav channels: %d", channels)
			}
			if bitsPerSample != 16 {
				return nil, fmt.Errorf("unsupported wav bits per sample: %d", bitsPerSample)
			}
			return append([]byte(nil), wavData[chunkStart:chunkEnd]...), nil
		}
		index = chunkEnd
		if index%2 == 1 {
			index++
		}
	}

	// Fallback parser: some providers return non-standard RIFF chunk sizes.
	dataOffset := 12
	for dataOffset+8 <= len(wavData) {
		idx := bytes.Index(wavData[dataOffset:], []byte("data"))
		if idx < 0 {
			break
		}
		header := dataOffset + idx
		if header+8 > len(wavData) {
			break
		}
		chunkSize := int(binary.LittleEndian.Uint32(wavData[header+4 : header+8]))
		chunkStart := header + 8
		if chunkStart >= len(wavData) {
			break
		}
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(wavData) {
			chunkEnd = len(wavData)
		}
		pcm := append([]byte(nil), wavData[chunkStart:chunkEnd]...)
		if len(pcm)%2 == 1 {
			pcm = pcm[:len(pcm)-1]
		}
		if len(pcm) > 0 {
			return pcm, nil
		}
		dataOffset = chunkStart
	}

	return nil, fmt.Errorf("wav data chunk not found")
}

// synthesizePCM calls a synthesizer and converts WAV output to raw PCM.
func synthesizePCM(ctx context.Context, synthesizer providers.AudioSynthesizer, text, voiceName string, sampleRateHz int) ([]byte, error) {
	result, err := synthesizer.Synthesize(ctx, providers.SynthesizeRequest{
		Text:   text,
		Voice:  voiceName,
		Format: "wav",
		Speed:  1.0,
	})
	if err != nil {
		return nil, err
	}
	defer result.Audio.Close()

	wavData, err := io.ReadAll(result.Audio)
	if err != nil {
		return nil, err
	}
	return wavToPCM16LE(wavData)
}
