package voice

import (
	"encoding/binary"
	"testing"
)

func makeFrame(sample int16, samples int) []byte {
	buffer := make([]byte, samples*2)
	for index := 0; index < samples; index++ {
		binary.LittleEndian.PutUint16(buffer[index*2:index*2+2], uint16(sample))
	}
	return buffer
}

func TestVADSilenceNoEvents(t *testing.T) {
	v := &VADState{}
	silence := makeFrame(0, 320)
	for index := 0; index < 20; index++ {
		started, ended, _ := v.ProcessFrame(silence)
		if started || ended {
			t.Fatalf("unexpected event at frame %d", index)
		}
	}
}

func TestVADStartsAtFrameTen(t *testing.T) {
	v := &VADState{}
	loud := makeFrame(12000, 320)
	for index := 1; index <= 10; index++ {
		started, _, _ := v.ProcessFrame(loud)
		if index < 10 && started {
			t.Fatalf("started too early at frame %d", index)
		}
		if index == 10 && !started {
			t.Fatalf("expected start at frame 10")
		}
	}
}

func TestVADEndsAfterSixteenRedemptionFrames(t *testing.T) {
	v := &VADState{}
	loud := makeFrame(12000, 320)
	silence := makeFrame(0, 320)
	for index := 0; index < 10; index++ {
		v.ProcessFrame(loud)
	}
	for index := 1; index <= 16; index++ {
		_, ended, _ := v.ProcessFrame(silence)
		if index < 16 && ended {
			t.Fatalf("ended too early at frame %d", index)
		}
		if index == 16 && !ended {
			t.Fatalf("expected ended at frame 16")
		}
	}
}
