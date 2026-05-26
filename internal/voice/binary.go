package voice

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	BinaryMagic byte = 0xA1

	FrameTypeAudioIn  byte = 0x01
	FrameTypeAudioOut byte = 0x02
	FrameTypeFlush    byte = 0x03

	BinaryHeaderSize = 18
)

const maxUint48 = uint64(1<<48 - 1)

// BinaryAudioFrame is the canonical websocket binary voice frame.
type BinaryAudioFrame struct {
	FrameType          byte
	Seq                uint64
	CaptureTimestampMS int64
	DurationMS         uint16
	Data               []byte
}

func ParseBinaryAudioFrame(raw []byte) (*BinaryAudioFrame, error) {
	if len(raw) < BinaryHeaderSize {
		return nil, errors.New("voice: binary frame too short")
	}
	if raw[0] != BinaryMagic {
		return nil, fmt.Errorf("voice: invalid binary frame magic: %x", raw[0])
	}
	if raw[1] != FrameTypeAudioIn && raw[1] != FrameTypeAudioOut && raw[1] != FrameTypeFlush {
		return nil, fmt.Errorf("voice: invalid frame type: %d", raw[1])
	}

	seqBytes := [8]byte{}
	copy(seqBytes[2:], raw[2:8])
	seq := binary.BigEndian.Uint64(seqBytes[:])

	frame := &BinaryAudioFrame{
		FrameType:          raw[1],
		Seq:                seq,
		CaptureTimestampMS: int64(binary.BigEndian.Uint64(raw[8:16])),
		DurationMS:         binary.BigEndian.Uint16(raw[16:18]),
		Data:               append([]byte(nil), raw[18:]...),
	}
	if frame.FrameType == FrameTypeFlush && len(frame.Data) != 0 {
		return nil, errors.New("voice: flush frame must not include payload")
	}
	return frame, nil
}

func EncodeBinaryAudioFrame(frame BinaryAudioFrame) []byte {
	if frame.Seq > maxUint48 {
		frame.Seq = frame.Seq & maxUint48
	}
	if frame.FrameType == FrameTypeFlush {
		frame.Data = nil
		frame.DurationMS = 0
	}
	buffer := make([]byte, BinaryHeaderSize+len(frame.Data))
	buffer[0] = BinaryMagic
	buffer[1] = frame.FrameType

	seqBytes := [8]byte{}
	binary.BigEndian.PutUint64(seqBytes[:], frame.Seq)
	copy(buffer[2:8], seqBytes[2:])

	binary.BigEndian.PutUint64(buffer[8:16], uint64(frame.CaptureTimestampMS))
	binary.BigEndian.PutUint16(buffer[16:18], frame.DurationMS)
	copy(buffer[18:], frame.Data)
	return buffer
}
