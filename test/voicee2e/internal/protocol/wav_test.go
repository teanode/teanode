package protocol

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWAVAsPCM16Mono(t *testing.T) {
	t.Parallel()
	samples := []int16{0, 1000, -1000, 2000, -2000}
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}
	wav := buildPCM16WAV(16000, 1, data)
	path := filepath.Join(t.TempDir(), "sample.wav")
	if err := os.WriteFile(path, wav, 0o644); err != nil {
		t.Fatalf("write wav: %v", err)
	}
	pcm, err := LoadWAVAsPCM16Mono(path, 16000)
	if err != nil {
		t.Fatalf("load wav: %v", err)
	}
	if len(pcm) != len(data) {
		t.Fatalf("unexpected pcm bytes: got=%d want=%d", len(pcm), len(data))
	}
}

func TestLoadWAVAsPCM16Mono_FillerChunkFallback(t *testing.T) {
	t.Parallel()
	samples := []int16{100, -200, 300, -400}
	pcmData := make([]byte, len(samples)*2)
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(pcmData[index*2:], uint16(sample))
	}
	wav := buildPCM16WAVWithFillerData(16000, 1, pcmData)
	path := filepath.Join(t.TempDir(), "filler.wav")
	if err := os.WriteFile(path, wav, 0o644); err != nil {
		t.Fatalf("write wav: %v", err)
	}
	pcm, err := LoadWAVAsPCM16Mono(path, 16000)
	if err != nil {
		t.Fatalf("load wav: %v", err)
	}
	if len(pcm) != len(pcmData) {
		t.Fatalf("unexpected pcm bytes: got=%d want=%d", len(pcm), len(pcmData))
	}
}

func buildPCM16WAV(sampleRate int, channels int, pcm []byte) []byte {
	const bitsPerSample = 16
	blockAlign := channels * bitsPerSample / 8
	byteRate := sampleRate * blockAlign
	riffSize := 4 + (8 + 16) + (8 + len(pcm))
	buf := make([]byte, 44+len(pcm))
	copy(buf[0:4], []byte("RIFF"))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(riffSize))
	copy(buf[8:12], []byte("WAVE"))
	copy(buf[12:16], []byte("fmt "))
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1)
	binary.LittleEndian.PutUint16(buf[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], bitsPerSample)
	copy(buf[36:40], []byte("data"))
	binary.LittleEndian.PutUint32(buf[40:44], uint32(len(pcm)))
	copy(buf[44:], pcm)
	return buf
}

func buildPCM16WAVWithFillerData(sampleRate int, channels int, pcm []byte) []byte {
	const bitsPerSample = 16
	blockAlign := channels * bitsPerSample / 8
	byteRate := sampleRate * blockAlign
	// RIFF + fmt + FLLR + data(empty)
	riffSize := 4 + (8 + 16) + (8 + len(pcm)) + (8 + 0)
	buf := make([]byte, 12+(8+16)+(8+len(pcm))+(8+0))
	copy(buf[0:4], []byte("RIFF"))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(riffSize))
	copy(buf[8:12], []byte("WAVE"))

	offset := 12
	copy(buf[offset:offset+4], []byte("fmt "))
	binary.LittleEndian.PutUint32(buf[offset+4:offset+8], 16)
	binary.LittleEndian.PutUint16(buf[offset+8:offset+10], 1)
	binary.LittleEndian.PutUint16(buf[offset+10:offset+12], uint16(channels))
	binary.LittleEndian.PutUint32(buf[offset+12:offset+16], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[offset+16:offset+20], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[offset+20:offset+22], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[offset+22:offset+24], bitsPerSample)
	offset += 24

	copy(buf[offset:offset+4], []byte("FLLR"))
	binary.LittleEndian.PutUint32(buf[offset+4:offset+8], uint32(len(pcm)))
	copy(buf[offset+8:offset+8+len(pcm)], pcm)
	offset += 8 + len(pcm)

	copy(buf[offset:offset+4], []byte("data"))
	binary.LittleEndian.PutUint32(buf[offset+4:offset+8], 0)
	return buf
}
