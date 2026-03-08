package fsstore

import (
	"testing"
)

func TestEncodeDecodeEmbeddingBase64Roundtrip(t *testing.T) {
	original := []float64{1.5, -0.003, 42, 0, 0.1, 0.2, 0.3, -0.5}
	encoded := encodeEmbeddingBase64(original)
	decoded, err := decodeEmbeddingBase64(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != len(original) {
		t.Fatalf("length mismatch: %d vs %d", len(decoded), len(original))
	}
	for index, value := range original {
		if decoded[index] != value {
			t.Errorf("index %d: got %f, want %f", index, decoded[index], value)
		}
	}
}

func TestEncodeEmbeddingBase64Empty(t *testing.T) {
	encoded := encodeEmbeddingBase64(nil)
	if encoded != "" {
		t.Errorf("expected empty string for nil, got %q", encoded)
	}
}

func TestDecodeEmbeddingBase64Empty(t *testing.T) {
	decoded, err := decodeEmbeddingBase64("")
	if err != nil {
		t.Fatalf("decode empty: %v", err)
	}
	if len(decoded) != 0 {
		t.Errorf("expected empty slice, got %v", decoded)
	}
}

func TestDecodeEmbeddingBase64InvalidLength(t *testing.T) {
	// Base64 of 5 bytes (not a multiple of 8).
	_, err := decodeEmbeddingBase64("AQIDBAU=")
	if err == nil {
		t.Error("expected error for invalid length")
	}
}

func TestDecodeEmbeddingBase64InvalidBase64(t *testing.T) {
	_, err := decodeEmbeddingBase64("!!!not-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}
