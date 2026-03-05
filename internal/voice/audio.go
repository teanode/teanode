package voice

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

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

// PCMToWAV wraps mono s16le PCM bytes in a minimal WAV container.
func PCMToWAV(pcm []byte, sampleRate, channels int) []byte {
	if channels <= 0 {
		channels = 1
	}
	if sampleRate <= 0 {
		sampleRate = 16000
	}

	const bitsPerSample = 16
	// Keep data chunk aligned to whole int16 samples.
	dataSize := len(pcm) &^ 1
	pcm = pcm[:dataSize]
	blockAlign := channels * bitsPerSample / 8
	byteRate := sampleRate * blockAlign

	out := make([]byte, 44+dataSize)
	copy(out[0:4], []byte("RIFF"))
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+dataSize))
	copy(out[8:12], []byte("WAVE"))

	copy(out[12:16], []byte("fmt "))
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1)
	binary.LittleEndian.PutUint16(out[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(out[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(out[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(out[34:36], bitsPerSample)

	copy(out[36:40], []byte("data"))
	binary.LittleEndian.PutUint32(out[40:44], uint32(dataSize))
	copy(out[44:], pcm)
	return out
}
