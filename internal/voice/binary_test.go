package voice

import "testing"

func TestBinaryFrameRoundTrip(t *testing.T) {
	in := BinaryAudioFrame{
		FrameType:          FrameTypeAudioIn,
		Seq:                0x0000AABBCCDDEE,
		CaptureTimestampMS: 1739990000123,
		DurationMS:         20,
		Data:               []byte{1, 2, 3, 4, 5, 6},
	}
	encoded := EncodeBinaryAudioFrame(in)
	decoded, err := ParseBinaryAudioFrame(encoded)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if decoded.FrameType != in.FrameType || decoded.Seq != in.Seq || decoded.CaptureTimestampMS != in.CaptureTimestampMS || decoded.DurationMS != in.DurationMS {
		t.Fatalf("decoded metadata mismatch: got %+v want %+v", *decoded, in)
	}
	if string(decoded.Data) != string(in.Data) {
		t.Fatalf("decoded payload mismatch: got %v want %v", decoded.Data, in.Data)
	}
}

func TestBinaryFrameRejectInvalid(t *testing.T) {
	if _, err := ParseBinaryAudioFrame([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected short frame error")
	}
	badMagic := make([]byte, BinaryHeaderSize)
	badMagic[0] = 0x00
	badMagic[1] = FrameTypeAudioIn
	if _, err := ParseBinaryAudioFrame(badMagic); err == nil {
		t.Fatal("expected bad magic error")
	}
	badType := make([]byte, BinaryHeaderSize)
	badType[0] = BinaryMagic
	badType[1] = 0x99
	if _, err := ParseBinaryAudioFrame(badType); err == nil {
		t.Fatal("expected bad frame type error")
	}
}
