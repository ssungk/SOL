package rtmp

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sol/pkg/media"
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

	payload, err := readPooledPayload(r, ms.readerContext.bufferPool, ms.readerContext.poolManager, ms.readerContext.nextChunkSize(basicHeader.chunkStreamID))
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

	format := (buf[0] & 0xC0) >> 6
	firstByte := buf[0] & 0x3F
	var chunkStreamId uint32

	slog.Info("fmt", "fmt", format)

	switch firstByte {
	case 0:
		// 2바이트 basic header: chunk stream ID = 64 + 다음 바이트 값
		secondByte := [1]byte{}
		if _, err := io.ReadFull(r, secondByte[:]); err != nil {
			return nil, fmt.Errorf("failed to read 2-byte basic header: %w", err)
		}
		chunkStreamId = 64 + uint32(secondByte[0])
		
		// 범위 검증 (64-319)
		if chunkStreamId > 319 {
			return nil, fmt.Errorf("invalid chunk stream ID %d for 2-byte header (must be 64-319)", chunkStreamId)
		}
		
	case 1:
		// 3바이트 basic header: chunk stream ID = 64 + 리틀엔디안 16비트
		extraBytes := [2]byte{}
		if _, err := io.ReadFull(r, extraBytes[:2]); err != nil {
			return nil, fmt.Errorf("failed to read 3-byte basic header: %w", err)
		}
		value := uint32(binary.LittleEndian.Uint16(extraBytes[:]))
		chunkStreamId = 64 + value
		
		// 범위 검증 (320-65599)
		if chunkStreamId < 320 || chunkStreamId > 65599 {
			return nil, fmt.Errorf("invalid chunk stream ID %d for 3-byte header (must be 320-65599)", chunkStreamId)
		}
		
	default:
		// 1바이트 basic header: chunk stream ID = 2-63 (직접 사용)
		chunkStreamId = uint32(firstByte)
		
		// 유효한 범위 검증 (2-63)
		if chunkStreamId < 2 {
			return nil, fmt.Errorf("invalid chunk stream ID %d (must be >= 2)", chunkStreamId)
		}
		
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

	// Fmt0에서 타임스탬프 단조성 검증 및 수정 (이전 헤더가 있는 경우)
	// H.264 B-frame을 고려하여 더 관대하게 처리
	if header != nil && timestamp < header.Timestamp {
		// 32비트 오버플로우가 아닌 실제 역순 감지
		timestampDiff := header.Timestamp - timestamp
		// B-frame에서는 약간의 역순이 정상일 수 있으므로, 큰 차이가 날 때만 수정
		if timestampDiff > 5000 && (header.Timestamp < 0xF0000000 || timestamp > 0x10000000) {
			// 5초 이상의 큰 역순은 비정상 - 강제로 단조 증가 유지
			timestamp = header.Timestamp + 1
			slog.Warn("Fixed large non-monotonic timestamp in Fmt0", "previousTimestamp", header.Timestamp, "originalTimestamp", readUint24BE(buf[0:3]), "correctedTimestamp", timestamp, "diff", timestampDiff)
		} else {
			// 작은 역순은 B-frame으로 간주하고 허용
			slog.Debug("Small timestamp reorder detected (likely B-frame)", "previousTimestamp", header.Timestamp, "currentTimestamp", timestamp, "diff", timestampDiff)
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

	if timestampDelta == EXTENDED_TIMESTAMP_THRESHOLD {
		var err error
		timestampDelta, err = readExtendedTimestamp(r)
		if err != nil {
			return nil, err
		}
	}

	// 올바른 타임스탬프 계산 (32비트 산술로 오버플로우 자동 처리)
	newTimestamp := header.Timestamp + timestampDelta

	// 단조성 검증 및 수정 (델타가 0이 아닌 경우만)
	if timestampDelta > 0 {
		// 32비트 오버플로우는 정상적인 상황 (약 49일마다 발생)
		// 실제 문제는 델타가 양수인데 타임스탬프가 감소하는 경우
		// H.264 B-frame을 고려하여 더 관대하게 처리
		if newTimestamp < header.Timestamp && timestampDelta < 0x80000000 {
			timestampDiff := header.Timestamp - newTimestamp
			// 큰 차이가 날 때만 수정 (B-frame 허용)
			if timestampDiff > 5000 {
				// 5초 이상의 큰 역순은 비정상 - 강제로 단조 증가 유지
				newTimestamp = header.Timestamp + 1
				slog.Warn("Fixed large non-monotonic timestamp in Fmt1", "previousTimestamp", header.Timestamp, "timestampDelta", timestampDelta, "correctedTimestamp", newTimestamp, "diff", timestampDiff)
			} else {
				// 작은 역순은 B-frame으로 간주하고 허용
				slog.Debug("Small timestamp reorder in Fmt1 (likely B-frame)", "previousTimestamp", header.Timestamp, "timestampDelta", timestampDelta, "newTimestamp", newTimestamp, "diff", timestampDiff)
			}
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

	// 올바른 타임스탬프 계산
	newTimestamp := header.Timestamp + timestampDelta

	// 단조성 검증 및 수정 (델타가 0이 아닌 경우만)
	if timestampDelta > 0 {
		if newTimestamp < header.Timestamp && timestampDelta < 0x80000000 {
			timestampDiff := header.Timestamp - newTimestamp
			// 큰 차이가 날 때만 수정 (B-frame 허용)
			if timestampDiff > 5000 {
				// 5초 이상의 큰 역순은 비정상 - 강제로 단조 증가 유지
				newTimestamp = header.Timestamp + 1
				slog.Warn("Fixed large non-monotonic timestamp in Fmt2", "previousTimestamp", header.Timestamp, "timestampDelta", timestampDelta, "correctedTimestamp", newTimestamp, "diff", timestampDiff)
			} else {
				// 작은 역순은 B-frame으로 간주하고 허용
				slog.Debug("Small timestamp reorder in Fmt2 (likely B-frame)", "previousTimestamp", header.Timestamp, "timestampDelta", timestampDelta, "newTimestamp", newTimestamp, "diff", timestampDiff)
			}
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
		bufferPool.Put(buf[:cap(buf)]) // 오류 시에도 버퍼 반환
		return nil, err
	}

	// 데이터를 복사해서 반환 (버퍼 풀 안전성 보장)
	result := make([]byte, size)
	copy(result, buf)
	bufferPool.Put(buf[:cap(buf)]) // 버퍼 풀에 반환

	return result, nil
}

// readPooledPayload reads payload using pool manager for tracking
func readPooledPayload(r io.Reader, bufferPool *sync.Pool, poolManager *media.PoolManager, size uint32) ([]byte, error) {
	// PoolManager를 통해 버퍼 할당
	pb := poolManager.AllocateBuffer(bufferPool, size)
	
	if _, err := io.ReadFull(r, pb.Data); err != nil {
		// 오류 시 pool manager를 통해 반환
		poolManager.ReleaseBuffer(pb.Data)
		return nil, err
	}

	// 추적되는 버퍼를 그대로 반환 (복사하지 않음)
	return pb.Data, nil
}

func readUint24BE(buf []byte) uint32 {
	return uint32(buf[0])<<16 | uint32(buf[1])<<8 | uint32(buf[2])
}
