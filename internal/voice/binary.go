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

// BinaryAudioFrame is the canonical websocket binary voice frame.
type BinaryAudioFrame struct {
	FrameType   byte
	Seq         uint64
	CaptureTSMs int64
	DurationMS  uint16
	Data        []byte
}

func ParseBinaryAudioFrame(raw []byte) (*BinaryAudioFrame, error) {
	if len(raw) < BinaryHeaderSize {
		return nil, errors.New("binary frame too short")
	}
	if raw[0] != BinaryMagic {
		return nil, fmt.Errorf("invalid binary frame magic: %x", raw[0])
	}

	seqBytes := [8]byte{}
	copy(seqBytes[2:], raw[2:8])
	seq := binary.BigEndian.Uint64(seqBytes[:])

	frame := &BinaryAudioFrame{
		FrameType:   raw[1],
		Seq:         seq,
		CaptureTSMs: int64(binary.BigEndian.Uint64(raw[8:16])),
		DurationMS:  binary.BigEndian.Uint16(raw[16:18]),
		Data:        append([]byte(nil), raw[18:]...),
	}
	return frame, nil
}

func EncodeBinaryAudioFrame(frame BinaryAudioFrame) []byte {
	buf := make([]byte, BinaryHeaderSize+len(frame.Data))
	buf[0] = BinaryMagic
	buf[1] = frame.FrameType

	seqBytes := [8]byte{}
	binary.BigEndian.PutUint64(seqBytes[:], frame.Seq)
	copy(buf[2:8], seqBytes[2:])

	binary.BigEndian.PutUint64(buf[8:16], uint64(frame.CaptureTSMs))
	binary.BigEndian.PutUint16(buf[16:18], frame.DurationMS)
	copy(buf[18:], frame.Data)
	return buf
}
