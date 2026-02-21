package voice

import "encoding/binary"

// PCMToWAV wraps mono s16le PCM bytes in a minimal WAV container.
func PCMToWAV(pcm []byte, sampleRate, channels int) []byte {
	if channels <= 0 {
		channels = 1
	}
	if sampleRate <= 0 {
		sampleRate = 16000
	}

	const bitsPerSample = 16
	dataSize := len(pcm)
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
