package rtmp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
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


func (ms *messageReader) setChunkSize(size uint32) {
	ms.readerContext.setChunkSize(size)
}

func (ms *messageReader) readNextMessage(r io.Reader) (*Message, error) {
	for {
		_, err := ms.readChunk(r)
		if err != nil {
			return nil, err
		}

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

	chunkSize := ms.readerContext.nextChunkSize(basicHeader.chunkStreamID)
	
	// 첫 번째 청크인 경우 전체 메시지 버퍼 할당
	if ms.readerContext.isInitialChunk(basicHeader.chunkStreamID) {
		ms.readerContext.allocateMessageBuffer(basicHeader.chunkStreamID)
	}
	
	// 제로카피: 메시지 버퍼에 직접 읽기
	messageBuffer, offset := ms.readerContext.getMessageBuffer(basicHeader.chunkStreamID)
	if messageBuffer == nil {
		return nil, fmt.Errorf("failed to get message buffer for chunkStreamId %d", basicHeader.chunkStreamID)
	}
	
	// 메시지 버퍼의 적절한 위치에 직접 읽기
	readBuffer := messageBuffer[offset : offset+chunkSize]
	if _, err := io.ReadFull(r, readBuffer); err != nil {
		// Pool에서 할당된 버퍼인 경우 반환 필요
		ms.readerContext.poolManager.ReleaseBuffer(messageBuffer)
		return nil, err
	}
	
	// 읽은 데이터 길이 업데이트
	ms.readerContext.updatePayloadLength(basicHeader.chunkStreamID, chunkSize)

	return NewChunk(basicHeader, messageHeader, readBuffer), nil
}

func readBasicHeader(r io.Reader) (*basicHeader, error) {
	buf := [1]byte{}
	if _, err := io.ReadFull(r, buf[:1]); err != nil {
		return nil, err
	}

	format := (buf[0] & 0xC0) >> 6
	firstByte := buf[0] & 0x3F
	var chunkStreamId uint32

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

	}

	return NewBasicHeader(format, chunkStreamId), nil
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

	if timestamp == ExtendedTimestampThreshold {
		var err error
		timestamp, err = readExtendedTimestamp(r)
		if err != nil {
			return nil, err
		}
	}

	// Fmt0에서 타임스탬프 단조성 검증 및 수정 (이전 헤더가 있는 경우)
	// H.264 B-frame을 고려하여 더 관대하게 처리
	if header != nil && timestamp < header.timestamp {
		// 32비트 오버플로우가 아닌 실제 역순 감지
		timestampDiff := header.timestamp - timestamp
		// B-frame에서는 약간의 역순이 정상일 수 있으므로, 큰 차이가 날 때만 수정
		if timestampDiff > 5000 && (header.timestamp < 0xF0000000 || timestamp > 0x10000000) {
			// 5초 이상의 큰 역순은 비정상 - 강제로 단조 증가 유지
			timestamp = header.timestamp + 1
			slog.Warn("Fixed large non-monotonic timestamp in Fmt0", "previousTimestamp", header.timestamp, "originalTimestamp", readUint24BE(buf[0:3]), "correctedTimestamp", timestamp, "diff", timestampDiff)
		} else {
			// 작은 역순은 B-frame으로 간주하고 허용
			slog.Debug("Small timestamp reorder detected (likely B-frame)", "previousTimestamp", header.timestamp, "currentTimestamp", timestamp, "diff", timestampDiff)
		}
	}

	return NewMessageHeader(timestamp, length, typeId, streamId), nil
}

func readFmt1MessageHeader(r io.Reader, header *messageHeader) (*messageHeader, error) {
	buf := [7]byte{}
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return nil, err
	}

	timestampDelta := readUint24BE(buf[0:3])
	length := readUint24BE(buf[3:6])
	typeId := buf[6]

	if timestampDelta == ExtendedTimestampThreshold {
		var err error
		timestampDelta, err = readExtendedTimestamp(r)
		if err != nil {
			return nil, err
		}
	}

	// 올바른 타임스탬프 계산 (32비트 산술로 오버플로우 자동 처리)
	newTimestamp := header.timestamp + timestampDelta

	// 단조성 검증 및 수정 (델타가 0이 아닌 경우만)
	if timestampDelta > 0 {
		// 32비트 오버플로우는 정상적인 상황 (약 49일마다 발생)
		// 실제 문제는 델타가 양수인데 타임스탬프가 감소하는 경우
		// H.264 B-frame을 고려하여 더 관대하게 처리
		if newTimestamp < header.timestamp && timestampDelta < 0x80000000 {
			timestampDiff := header.timestamp - newTimestamp
			// 큰 차이가 날 때만 수정 (B-frame 허용)
			if timestampDiff > 5000 {
				// 5초 이상의 큰 역순은 비정상 - 강제로 단조 증가 유지
				newTimestamp = header.timestamp + 1
				slog.Warn("Fixed large non-monotonic timestamp in Fmt1", "previousTimestamp", header.timestamp, "timestampDelta", timestampDelta, "correctedTimestamp", newTimestamp, "diff", timestampDiff)
			} else {
				// 작은 역순은 B-frame으로 간주하고 허용
				slog.Debug("Small timestamp reorder in Fmt1 (likely B-frame)", "previousTimestamp", header.timestamp, "timestampDelta", timestampDelta, "newTimestamp", newTimestamp, "diff", timestampDiff)
			}
		}
	}

	return NewMessageHeader(newTimestamp, length, typeId, header.streamId), nil
}

func readFmt2MessageHeader(r io.Reader, header *messageHeader) (*messageHeader, error) {
	buf := [3]byte{}
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return nil, err
	}

	timestampDelta := readUint24BE(buf[:])
	if timestampDelta == ExtendedTimestampThreshold {
		var err error
		timestampDelta, err = readExtendedTimestamp(r)
		if err != nil {
			return nil, err
		}
	}

	// 올바른 타임스탬프 계산
	newTimestamp := header.timestamp + timestampDelta

	// 단조성 검증 및 수정 (델타가 0이 아닌 경우만)
	if timestampDelta > 0 {
		if newTimestamp < header.timestamp && timestampDelta < 0x80000000 {
			timestampDiff := header.timestamp - newTimestamp
			// 큰 차이가 날 때만 수정 (B-frame 허용)
			if timestampDiff > 5000 {
				// 5초 이상의 큰 역순은 비정상 - 강제로 단조 증가 유지
				newTimestamp = header.timestamp + 1
				slog.Warn("Fixed large non-monotonic timestamp in Fmt2", "previousTimestamp", header.timestamp, "timestampDelta", timestampDelta, "correctedTimestamp", newTimestamp, "diff", timestampDiff)
			} else {
				// 작은 역순은 B-frame으로 간주하고 허용
				slog.Debug("Small timestamp reorder in Fmt2 (likely B-frame)", "previousTimestamp", header.timestamp, "timestampDelta", timestampDelta, "newTimestamp", newTimestamp, "diff", timestampDiff)
			}
		}
	}

	return NewMessageHeader(newTimestamp, header.length, header.typeId, header.streamId), nil
}

func readFmt3MessageHeader(r io.Reader, header *messageHeader) (*messageHeader, error) {
	// FMT3은 이전 메시지의 헤더와 동일. 여기선 아무것도 읽지 않음
	return NewMessageHeader(header.timestamp, header.length, header.typeId, header.streamId), nil
}

func readExtendedTimestamp(r io.Reader) (uint32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(buf[:]), nil
}


func readUint24BE(buf []byte) uint32 {
	return uint32(buf[0])<<16 | uint32(buf[1])<<8 | uint32(buf[2])
}

// abortChunkStream aborts a specific chunk stream
func (mr *messageReader) abortChunkStream(chunkStreamId uint32) {
	mr.readerContext.abortChunkStream(chunkStreamId)
}
