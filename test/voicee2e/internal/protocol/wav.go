package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
)

func LoadWAVAsPCM16Mono(path string, targetSampleRate int) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	samples, sampleRate, channels, err := parsePCM16WAV(raw)
	if err != nil {
		return nil, err
	}
	mono := samples
	if channels > 1 {
		mono = downmixToMono(samples, channels)
	}
	if sampleRate != targetSampleRate {
		mono = resampleLinearInt16(mono, sampleRate, targetSampleRate)
	}
	out := make([]byte, len(mono)*2)
	for i, sample := range mono {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(sample))
	}
	return out, nil
}

func parsePCM16WAV(raw []byte) ([]int16, int, int, error) {
	if len(raw) < 44 {
		return nil, 0, 0, errors.New("wav too short")
	}
	if string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, 0, 0, errors.New("invalid wav header")
	}

	offset := 12
	var sampleRate int
	var channels int
	var bitsPerSample int
	var pcm []byte
	for offset+8 <= len(raw) {
		chunkID := string(raw[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(raw[offset+4 : offset+8]))
		offset += 8
		if offset+chunkSize > len(raw) {
			return nil, 0, 0, errors.New("wav chunk out of bounds")
		}
		data := raw[offset : offset+chunkSize]
		offset += chunkSize
		if chunkSize%2 == 1 && offset < len(raw) {
			offset++
		}
		switch chunkID {
		case "fmt ":
			if len(data) < 16 {
				return nil, 0, 0, errors.New("wav fmt too short")
			}
			audioFormat := binary.LittleEndian.Uint16(data[0:2])
			if audioFormat != 1 {
				return nil, 0, 0, fmt.Errorf("unsupported wav format: %d", audioFormat)
			}
			channels = int(binary.LittleEndian.Uint16(data[2:4]))
			sampleRate = int(binary.LittleEndian.Uint32(data[4:8]))
			bitsPerSample = int(binary.LittleEndian.Uint16(data[14:16]))
		case "data":
			pcm = append([]byte(nil), data...)
		}
	}
	if channels <= 0 || sampleRate <= 0 {
		return nil, 0, 0, errors.New("missing wav fmt chunk")
	}
	if bitsPerSample != 16 {
		return nil, 0, 0, fmt.Errorf("unsupported bits per sample: %d", bitsPerSample)
	}
	if len(pcm) == 0 {
		return nil, 0, 0, errors.New("missing wav data chunk")
	}
	if len(pcm)%2 != 0 {
		return nil, 0, 0, errors.New("invalid pcm data size")
	}

	samples := make([]int16, len(pcm)/2)
	for i := 0; i < len(samples); i++ {
		samples[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
	}
	return samples, sampleRate, channels, nil
}

func downmixToMono(samples []int16, channels int) []int16 {
	if channels <= 1 {
		return samples
	}
	frames := len(samples) / channels
	out := make([]int16, frames)
	for frame := 0; frame < frames; frame++ {
		sum := 0
		for channel := 0; channel < channels; channel++ {
			sum += int(samples[frame*channels+channel])
		}
		out[frame] = int16(sum / channels)
	}
	return out
}

func resampleLinearInt16(in []int16, inRate, outRate int) []int16 {
	if inRate <= 0 || outRate <= 0 || len(in) == 0 {
		return in
	}
	if inRate == outRate {
		return in
	}
	ratio := float64(outRate) / float64(inRate)
	outLen := int(math.Round(float64(len(in)) * ratio))
	if outLen <= 0 {
		return []int16{}
	}
	out := make([]int16, outLen)
	for i := 0; i < outLen; i++ {
		src := float64(i) / ratio
		idx := int(src)
		frac := src - float64(idx)
		if idx >= len(in)-1 {
			out[i] = in[len(in)-1]
			continue
		}
		v0 := float64(in[idx])
		v1 := float64(in[idx+1])
		out[i] = int16(math.Round(v0 + (v1-v0)*frac))
	}
	return out
}
