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

	// 첫 번째 청크인 경우 특별 처리
	if ms.readerContext.isInitialChunk(basicHeader.chunkStreamID) {
		// 비디오/오디오 메시지인 경우 헤더 먼저 읽고 분리
		if messageHeader.typeId == MsgTypeVideo || messageHeader.typeId == MsgTypeAudio {
			err := ms.readAndSeparateMediaHeader(r, basicHeader.chunkStreamID, messageHeader.typeId, &chunkSize)
			if err != nil {
				ms.readerContext.abortChunkStream(basicHeader.chunkStreamID)
				return nil, err
			}
		}

		// 순수 데이터 크기만큼 버퍼 할당 (헤더 크기 제외)
		ms.readerContext.allocateMessageBuffer(basicHeader.chunkStreamID)
	}

	// 제로카피: 메시지 버퍼에 직접 읽기
	messageBuffer, offset := ms.readerContext.getMessageBuffer(basicHeader.chunkStreamID)
	if messageBuffer == nil {
		return nil, fmt.Errorf("failed to get message buffer for chunkStreamId %d", basicHeader.chunkStreamID)
	}

	// 남은 데이터가 있을 때만 읽기
	if chunkSize > 0 {
		// 메시지 버퍼의 적절한 위치에 직접 읽기
		readBuffer := messageBuffer[offset : offset+chunkSize]
		if _, err := io.ReadFull(r, readBuffer); err != nil {
			// Pool 버퍼는 abortChunkStream에서 자동 반환
			ms.readerContext.abortChunkStream(basicHeader.chunkStreamID)
			return nil, err
		}
	}

	// 읽은 데이터 길이 업데이트
	ms.readerContext.updatePayloadLength(basicHeader.chunkStreamID, chunkSize)

	// ReadBuffer는 실제로 읽은 데이터 참조 (chunkSize가 0일 수 있음)
	var readData []byte
	if chunkSize > 0 {
		readData = messageBuffer[offset : offset+chunkSize]
	}

	return NewChunk(basicHeader, messageHeader, readData), nil
}

// readAndSeparateMediaHeader 비디오/오디오 메시지의 첫 번째 청크에서 RTMP 헤더를 읽어서 분리
func (ms *messageReader) readAndSeparateMediaHeader(r io.Reader, chunkStreamId uint32, messageType uint8, chunkSize *uint32) error {
	var headerSize uint32

	// 메시지 타입에 따라 헤더 크기 결정
	switch messageType {
	case MsgTypeVideo:
		headerSize = 5 // Frame Type(1) + AVC Packet Type(1) + Composition Time(3)
	case MsgTypeAudio:
		headerSize = 2 // Audio Info(1) + AAC Packet Type(1)
	default:
		return fmt.Errorf("unsupported message type for header separation: %d", messageType)
	}

	// 현재 청크에서 읽을 수 있는 헤더 크기 계산
	availableHeaderSize := headerSize
	if *chunkSize < headerSize {
		availableHeaderSize = *chunkSize
	}

	// 헤더 읽기
	header := make([]byte, availableHeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return fmt.Errorf("failed to read media header: %w", err)
	}

	// 헤더 저장
	ms.readerContext.storeRtmpHeader(chunkStreamId, header)

	// 읽은 헤더 크기만큼 청크 크기에서 차감
	*chunkSize -= availableHeaderSize

	return nil
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

func readFmt0MessageHeader(r io.Reader, _ *messageHeader) (*messageHeader, error) {
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

	// 타임스탬프 계산 (32비트 산술로 오버플로우 자동 처리)
	newTimestamp := header.timestamp + timestampDelta

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

	// 타임스탬프 계산
	newTimestamp := header.timestamp + timestampDelta

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
