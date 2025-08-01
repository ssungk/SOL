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

	if c0[0] != RTMP_VERSION {
		return fmt.Errorf("unsupported RTMP version: %d", c0[0])
	}

	// S0
	if _, err := rw.Write(c0); err != nil {
		return fmt.Errorf("failed to write S0: %w", err)
	}

	// S1
	s1 := make([]byte, HANDSHAKE_SIZE)
	copy(s1[0:4], []byte{0, 0, 0, 0}) // time field
	copy(s1[4:8], []byte{0, 0, 0, 0}) // zero field
	_, _ = rand.Read(s1[8:])          // random field

	if _, err := rw.Write(s1); err != nil {
		return fmt.Errorf("failed to write S1: %w", err)
	}

	// C1
	c1 := make([]byte, HANDSHAKE_SIZE)
	if _, err := io.ReadFull(rw, c1); err != nil {
		return fmt.Errorf("failed to read C1: %w", err)
	}

	// S2
	if _, err := rw.Write(c1); err != nil {
		return fmt.Errorf("failed to write S2: %w", err)
	}

	// C2
	c2 := make([]byte, HANDSHAKE_SIZE)
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
		slog.Info("read chunk", "chunk.messageHeader", chunk.messageHeader)

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

	// 모든 경우에 헤더를 업데이트 (Fmt1/2/3의 경우 상속받은 완전한 헤더로 업데이트)
	ms.readerContext.updateMsgHeader(basicHeader.chunkStreamID, messageHeader)

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
		// 2바이트 basic header: chunk stream ID = 64 + 다음 바이트 값
		buf := [1]byte{}
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return nil, fmt.Errorf("failed to read 2-byte basic header: %w", err)
		}
		chunkStreamId = 64 + uint32(buf[0])
		
		// 범위 검증 (64-319)
		if chunkStreamId > 319 {
			return nil, fmt.Errorf("invalid chunk stream ID %d for 2-byte header (must be 64-319)", chunkStreamId)
		}
		
	} else if chunkStreamId == 1 {
		// 3바이트 basic header: chunk stream ID = 64 + 리틀엔디안 16비트
		buf := [2]byte{}
		if _, err := io.ReadFull(r, buf[:2]); err != nil {
			return nil, fmt.Errorf("failed to read 3-byte basic header: %w", err)
		}
		value := uint32(binary.LittleEndian.Uint16(buf[:]))
		chunkStreamId = 64 + value
		
		// 범위 검증 (320-65599)
		if chunkStreamId < 320 || chunkStreamId > 65599 {
			return nil, fmt.Errorf("invalid chunk stream ID %d for 3-byte header (must be 320-65599)", chunkStreamId)
		}
		
	} else if chunkStreamId < 2 {
		// chunk stream ID 0과 1은 특별한 의미로 사용되어 일반 사용 불가
		return nil, fmt.Errorf("invalid chunk stream ID %d (must be >= 2)", chunkStreamId)
	} else {
		// 1바이트 basic header: chunk stream ID = 2-63 (직접 사용)
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

	if timestamp == EXTENDED_TIMESTAMP_THRESHOLD {
		var err error
		timestamp, err = readExtendedTimestamp(r)
		if err != nil {
			return nil, err
		}
	}

	// Fmt0에서 타임스탬프 단조성 검증 (이전 헤더가 있는 경우)
	if header != nil && timestamp < header.Timestamp {
		slog.Warn("Non-monotonic timestamp detected in Fmt0", "previousTimestamp", header.Timestamp, "newTimestamp", timestamp)
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

	if timestampDelta == EXTENDED_TIMESTAMP_THRESHOLD {
		var err error
		timestampDelta, err = readExtendedTimestamp(r)
		if err != nil {
			return nil, err
		}
	}

	// 이전 타임스탬프에 델타를 더해서 새로운 타임스탬프 계산
	newTimestamp := header.Timestamp + timestampDelta

	// 타임스탬프 단조성 검증 (델타가 0이 아닌 경우)
	// 오버플로우 상황은 제외 (newTimestamp가 작아진 경우가 정상적인 wrap-around인지 확인)
	if timestampDelta > 0 && newTimestamp <= header.Timestamp {
		// 32비트 오버플로우가 아닌 실제 단조성 위반인지 확인
		if header.Timestamp < 0xF0000000 || newTimestamp > 0x10000000 {
			slog.Warn("Non-monotonic timestamp detected in Fmt1",
				"previousTimestamp", header.Timestamp,
				"timestampDelta", timestampDelta,
				"newTimestamp", newTimestamp)
		}
	}

	return newMessageHeader(newTimestamp, length, typeId, header.streamId), nil
}

func readFmt2MessageHeader(r io.Reader, header *messageHeader) (*messageHeader, error) {
	buf := [3]byte{}
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return nil, err
	}

	timestampDelta := readUint24BE(buf[:])
	if timestampDelta == EXTENDED_TIMESTAMP_THRESHOLD {
		var err error
		timestampDelta, err = readExtendedTimestamp(r)
		if err != nil {
			return nil, err
		}
	}

	// 이전 타임스탬프에 델타를 더해서 새로운 타임스탬프 계산
	newTimestamp := header.Timestamp + timestampDelta

	// 타임스탬프 단조성 검증 (델타가 0이 아닌 경우)
	// 오버플로우 상황은 제외 (newTimestamp가 작아진 경우가 정상적인 wrap-around인지 확인)
	if timestampDelta > 0 && newTimestamp <= header.Timestamp {
		// 32비트 오버플로우가 아닌 실제 단조성 위반인지 확인
		if header.Timestamp < 0xF0000000 || newTimestamp > 0x10000000 {
			slog.Warn("Non-monotonic timestamp detected in Fmt2",
				"previousTimestamp", header.Timestamp,
				"timestampDelta", timestampDelta,
				"newTimestamp", newTimestamp)
		}
	}

	return newMessageHeader(newTimestamp, header.length, header.typeId, header.streamId), nil
}

func readFmt3MessageHeader(r io.Reader, header *messageHeader) (*messageHeader, error) {
	// FMT3은 이전 메시지의 헤더와 동일. 여기선 아무것도 읽지 않음
	return newMessageHeader(header.Timestamp, header.length, header.typeId, header.streamId), nil
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
