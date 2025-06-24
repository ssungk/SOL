package rtmp

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

type messageReader struct {
	readerContext *messageReaderContext
}

func newMessageReader() *messageReader {
	ms := &messageReader{
		readerContext: newMessageReaderContext(),
	}
	return ms
}

func handshake(rw io.ReadWriter) error {
	// C0
	c0 := make([]byte, 1)
	if _, err := io.ReadFull(rw, c0); err != nil {
		return fmt.Errorf("failed to read C0: %w", err)
	}

	if c0[0] != 0x03 {
		return fmt.Errorf("unsupported RTMP version: %d", c0[0])
	}

	// S0
	if _, err := rw.Write(c0); err != nil {
		return fmt.Errorf("failed to write S0: %w", err)
	}

	// S1
	s1 := make([]byte, 1536)
	copy(s1[0:4], []byte{0, 0, 0, 0}) // time field
	copy(s1[4:8], []byte{0, 0, 0, 0}) // zero field
	_, _ = rand.Read(s1[8:])          // random field

	if _, err := rw.Write(s1); err != nil {
		return fmt.Errorf("failed to write S1: %w", err)
	}

	// C1
	c1 := make([]byte, 1536)
	if _, err := io.ReadFull(rw, c1); err != nil {
		return fmt.Errorf("failed to read C1: %w", err)
	}

	// S2
	if _, err := rw.Write(c1); err != nil {
		return fmt.Errorf("failed to write S2: %w", err)
	}

	// C2
	c2 := make([]byte, 1536)
	if _, err := io.ReadFull(rw, c2); err != nil {
		return fmt.Errorf("failed to read C2: %w", err)
	}

	return nil
}

func (ms *messageReader) setChunkSize(size uint32) {
	ms.readerContext.setChunkSize(size)
}

func (ms *messageReader) readNextMessage(r io.Reader) (*Message, error) {
	for {
		chunk, err := ms.readChunk(r)
		if err != nil {
			return nil, err
		}
		slog.Info("read chunk", "chunk", chunk)

		message, err := ms.readerContext.popMessageIfPossible()
		if err == nil {
			return message, err
		}
	}
}

func (ms *messageReader) readChunk(r io.Reader) (*Chunk, error) {
	basicHeader, err := readBasicHeader(r)
	if err != nil {
		slog.Error("Failed to read basic header", "err", err)
		return nil, err
	}

	messageHeader, err := readMessageHeader(r, basicHeader.fmt, ms.readerContext.getMsgHeader(basicHeader.chunkStreamID))
	if err != nil {
		return nil, err
	}

	if ms.readerContext.isInitialChunk(basicHeader.chunkStreamID) {
		ms.readerContext.updateMsgHeader(basicHeader.chunkStreamID, messageHeader)
	}

	payload, err := readPayload(r, ms.readerContext.bufferPool, ms.readerContext.nextChunkSize(basicHeader.chunkStreamID))
	if err != nil {
		return nil, err
	}

	ms.readerContext.appendPayload(basicHeader.chunkStreamID, payload)

	slog.Info("msg", "messageHeader", messageHeader.Timestamp)

	return NewChunk(basicHeader, messageHeader, payload), nil
}

func readBasicHeader(r io.Reader) (*basicHeader, error) {
	buf := [1]byte{}
	if _, err := io.ReadFull(r, buf[:1]); err != nil {
		return nil, err
	}

	var chunkStreamId uint32

	format := (buf[0] & 0xC0) >> 6
	chunkStreamId = uint32(buf[0] & 0x3F)

	slog.Info("fmt", "fmt", format)

	if chunkStreamId == 0 {
		buf := [1]byte{}
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return nil, err
		}
		chunkStreamId = chunkStreamId + uint32(buf[0])
	} else if chunkStreamId == 1 {
		buf := [2]byte{}
		if _, err := io.ReadFull(r, buf[:2]); err != nil {
			return nil, err
		}
		chunkStreamId = 64 + uint32(binary.LittleEndian.Uint16(buf[:]))

	} else {
		slog.Info("chunkStreamId", "chunkStreamId", chunkStreamId)
	}

	return newBasicHeader(format, chunkStreamId), nil
}

func readMessageHeader(r io.Reader, fmt byte, header *messageHeader) (*messageHeader, error) {
	switch fmt {
	case 0:
		return readFmt0MessageHeader(r, header)
	case 1:
		return readFmt1MessageHeader(r, header)
	case 2:
		return readFmt2MessageHeader(r, header)
	case 3:
		return readFmt3MessageHeader(r, header)
	}
	return nil, errors.New("fmt must be 0-3")
}

func readFmt0MessageHeader(r io.Reader, header *messageHeader) (*messageHeader, error) {
	buf := [11]byte{}
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return nil, err
	}

	timestamp := readUint24BE(buf[0:3])
	length := readUint24BE(buf[3:6])
	typeId := buf[6]
	streamId := binary.LittleEndian.Uint32(buf[7:11])

	if timestamp == 0xFFFFFF {
		var err error
		timestamp, err = readExtendedTimestamp(r)
		if err != nil {
			return nil, err
		}
	}

	slog.Info("Fmt0MessageHeade", "timestamp", timestamp, "MessageLength", length, "MessageTypeID", typeId, "MessageStreamID", streamId)

	return newMessageHeader(timestamp, length, typeId, streamId), nil
}

func readFmt1MessageHeader(r io.Reader, header *messageHeader) (*messageHeader, error) {
	buf := [7]byte{}
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return nil, err
	}

	timestampDelta := readUint24BE(buf[0:3])
	length := readUint24BE(buf[3:6])
	typeId := buf[6]

	if timestampDelta == 0xFFFFFF {
		var err error
		timestampDelta, err = readExtendedTimestamp(r)
		if err != nil {
			return nil, err
		}
	}

	return newMessageHeader(timestampDelta, length, typeId, 0), nil // streamId는 이전 헤더에서 유지
}

func readFmt2MessageHeader(r io.Reader, header *messageHeader) (*messageHeader, error) {
	buf := [3]byte{}
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return nil, err
	}

	timestampDelta := readUint24BE(buf[:])
	if timestampDelta == 0xFFFFFF {
		var err error
		timestampDelta, err = readExtendedTimestamp(r)
		if err != nil {
			return nil, err
		}
	}

	return newMessageHeader(timestampDelta, 0, 0, 0), nil // 길이, typeId, streamId는 이전 헤더 유지
}

func readFmt3MessageHeader(r io.Reader, header *messageHeader) (*messageHeader, error) {
	// FMT3은 이전 메시지의 헤더와 동일. 여기선 아무것도 읽지 않음
	return newMessageHeader(0, 0, 0, 0), nil
}

func readExtendedTimestamp(r io.Reader) (uint32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(buf[:]), nil
}

func readPayload(r io.Reader, bufferPool *sync.Pool, size uint32) ([]byte, error) {
	buf := bufferPool.Get().([]byte)[:size]
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

func readUint24BE(buf []byte) uint32 {
	return uint32(buf[0])<<16 | uint32(buf[1])<<8 | uint32(buf[2])
}
