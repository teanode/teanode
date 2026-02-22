package voice

import (
	"encoding/binary"
	"testing"
)

func makeFrame(sample int16, samples int) []byte {
	buf := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		binary.LittleEndian.PutUint16(buf[i*2:i*2+2], uint16(sample))
	}
	return buf
}

func TestVADSilenceNoEvents(t *testing.T) {
	v := &VADState{}
	silence := makeFrame(0, 320)
	for i := 0; i < 20; i++ {
		started, ended, _ := v.ProcessFrame(silence)
		if started || ended {
			t.Fatalf("unexpected event at frame %d", i)
		}
	}
}

func TestVADStartsAtFrameTen(t *testing.T) {
	v := &VADState{}
	loud := makeFrame(12000, 320)
	for i := 1; i <= 10; i++ {
		started, _, _ := v.ProcessFrame(loud)
		if i < 10 && started {
			t.Fatalf("started too early at frame %d", i)
		}
		if i == 10 && !started {
			t.Fatalf("expected start at frame 10")
		}
	}
}

func TestVADEndsAfterSixteenRedemptionFrames(t *testing.T) {
	v := &VADState{}
	loud := makeFrame(12000, 320)
	silence := makeFrame(0, 320)
	for i := 0; i < 10; i++ {
		v.ProcessFrame(loud)
	}
	for i := 1; i <= 16; i++ {
		_, ended, _ := v.ProcessFrame(silence)
		if i < 16 && ended {
			t.Fatalf("ended too early at frame %d", i)
		}
		if i == 16 && !ended {
			t.Fatalf("expected ended at frame 16")
		}
	}
}
