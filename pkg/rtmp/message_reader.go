package rtmp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sol/pkg/core"
)

// 메시지 헤더 크기 상수
const (
	FMT0_HEADER_SIZE = 11
	FMT1_HEADER_SIZE = 7
	FMT2_HEADER_SIZE = 3
)

type msgReader struct {
	msgHeaders map[uint32]msgHeader
	payloads       map[uint32][]*core.Buffer
	payloadLengths map[uint32]uint32
	avTagHeaders   map[uint32]*core.Buffer // AV 태그 헤더 (비디오: 5바이트, 오디오: 2바이트)
	chunkSize      uint32
}

func newMsgReader() *msgReader {
	return &msgReader{
		msgHeaders: make(map[uint32]msgHeader),
		payloads:       make(map[uint32][]*core.Buffer),
		payloadLengths: make(map[uint32]uint32),
		avTagHeaders:   make(map[uint32]*core.Buffer),
		chunkSize:      DefaultChunkSize,
	}
}

func (mr *msgReader) setChunkSize(size uint32) {
	mr.chunkSize = size
}

func (mr *msgReader) readNextMessage(r io.Reader) (Message, error) {
	for {
		err := mr.readChunk(r)
		if err != nil {
			slog.Error("readChunk failed", "err", err)
			return Message{}, err
		}

		message, err := mr.popMessageIfPossible()
		if err == nil {
			// slog.Info("Message ready", "typeId", message.msgHeader.typeId) // 너무 빈번하거나 주석 처리
			return message, err
		}
	}
}

func (mr *msgReader) readChunk(r io.Reader) error {
	basicHeader, err := readBasicHeader(r)
	if err != nil {
		slog.Error("Failed to read basic header", "err", err)
		return err
	}

	previousHeader := mr.getMsgHeader(basicHeader.chunkStreamID)
	messageHeader, err := readMessageHeader(r, basicHeader.fmt, previousHeader)
	if err != nil {
		return err
	}

	// 모든 경우에 헤더를 업데이트
	mr.updateMsgHeader(basicHeader.chunkStreamID, &messageHeader)

	chunkSize := mr.nextChunkSize(basicHeader.chunkStreamID)

	// 첫 번째 청크인 경우 특별 처리
	if mr.isInitialChunk(basicHeader.chunkStreamID) {
		// 비디오/오디오 메시지인 경우 헤더 먼저 읽고 분리
		if messageHeader.typeId == MsgTypeVideo || messageHeader.typeId == MsgTypeAudio {
			err := mr.readAndSeparateMediaHeader(r, basicHeader.chunkStreamID, messageHeader.typeId, &chunkSize)
			if err != nil {
				mr.abortChunkStream(basicHeader.chunkStreamID)
				return err
			}
		}

		// 청크 배열은 addNewChunk에서 자동 초기화
	}

	// 청크 데이터 읽기
	if chunkSize > 0 {
		// core.Buffer를 사용한 풀링된 버퍼 할당
		buffer := core.NewBuffer(int(chunkSize))
		if _, err := io.ReadFull(r, buffer.Data()); err != nil {
			buffer.Release() // 실패시 버퍼 해제
			mr.abortChunkStream(basicHeader.chunkStreamID)
			return err
		}

		// core.Buffer를 직접 사용하여 컨텍스트에 추가
		mr.addMediaBuffer(basicHeader.chunkStreamID, buffer)
		// slog.Debug("Chunk read", "chunkStreamID", basicHeader.chunkStreamID, "payload_len", len(buffer.Data())) // 너무 빈번함

		return nil
	}

	// 빈 청크인 경우
	emptyBuffer := core.NewBuffer(0)
	mr.addMediaBuffer(basicHeader.chunkStreamID, emptyBuffer)
	// slog.Debug("Chunk read", "chunkStreamID", basicHeader.chunkStreamID, "payload_len", 0) // 너무 빈번함
	return nil
}

// readAndSeparateMediaHeader 비디오/오디오 메시지의 첫 번째 청크에서 RTMP 헤더를 읽어서 분리
func (mr *msgReader) readAndSeparateMediaHeader(r io.Reader, chunkStreamId uint32, messageType uint8, chunkSize *uint32) error {
	// 최소한 첫 바이트는 읽어서 코덱을 판단해야 함
	if *chunkSize == 0 {
		return fmt.Errorf("chunk size is 0, cannot read media header")
	}

	// 첫 바이트 읽기 (코덱 정보가 포함됨)
	firstByte := make([]byte, 1)
	if _, err := io.ReadFull(r, firstByte); err != nil {
		return fmt.Errorf("failed to read first byte of media header: %w", err)
	}

	// 첫 바이트를 기준으로 헤더 크기 동적 계산
	headerSize, err := getMediaHeaderSize(messageType, firstByte[0])
	if err != nil {
		return fmt.Errorf("failed to determine media header size: %w", err)
	}

	// 현재 청크에서 읽을 수 있는 헤더 크기 계산 (첫 바이트는 이미 읽음)
	remainingHeaderSize := uint32(headerSize - 1) // 첫 바이트 제외
	availableRemainingSize := remainingHeaderSize
	if *chunkSize-1 < remainingHeaderSize {
		availableRemainingSize = *chunkSize - 1
	}

	// 나머지 헤더 읽기
	var remainingHeader []byte
	if availableRemainingSize > 0 {
		remainingHeader = make([]byte, availableRemainingSize)
		if _, err := io.ReadFull(r, remainingHeader); err != nil {
			return fmt.Errorf("failed to read remaining media header: %w", err)
		}
	}

	// 전체 헤더 조합 (첫 바이트 + 나머지)
	fullHeader := make([]byte, 1+len(remainingHeader))
	fullHeader[0] = firstByte[0]
	copy(fullHeader[1:], remainingHeader)

	// 헤더 저장
	mr.storeAVTagHeader(chunkStreamId, fullHeader)

	// 읽은 헤더 크기만큼 청크 크기에서 차감
	*chunkSize -= (1 + availableRemainingSize)

	return nil
}

func readBasicHeader(r io.Reader) (header basicHeader, err error) {
	header1 := [1]byte{}
	if _, err = io.ReadFull(r, header1[:]); err != nil {
		// slog.Debug("readBasicHeader ReadFull failed", "err", err) // 너무 빈번함
		return
	}

	format := (header1[0] & 0xC0) >> 6
	chunkStreamId := uint32(header1[0] & 0x3F)

	switch chunkStreamId {
	case 0:
		// 2바이트 basic header: chunk stream ID = 64 + 다음 바이트 값
		header2 := [1]byte{}
		if _, err = io.ReadFull(r, header2[:]); err != nil {
			err = fmt.Errorf("failed to read 2-byte basic header: %w", err)
			return
		}
		chunkStreamId = 64 + uint32(header2[0]) // 자동으로 64-319 범위
	case 1:
		// 3바이트 basic header: chunk stream ID = 64 + 리틀엔디안 16비트
		header23 := [2]byte{}
		if _, err = io.ReadFull(r, header23[:]); err != nil {
			err = fmt.Errorf("failed to read 3-byte basic header: %w", err)
			return
		}
		value := uint32(binary.LittleEndian.Uint16(header23[:]))
		chunkStreamId = 64 + value // 64-65599 범위 (겹치는 구간 허용)
	default:
		// 1바이트 basic header: chunk stream ID = 2-63 (이미 올바른 값)
		// switch문으로 0과 1은 이미 처리되었으므로 2-63 범위 보장됨
	}

	header = newBasicHeader(format, chunkStreamId)
	return
}

func readMessageHeader(r io.Reader, format byte, header *msgHeader) (msgHeader, error) {
	switch format {
	case FmtType0:
		return readFmt0MessageHeader(r, header)
	case FmtType1:
		return readFmt1MessageHeader(r, header)
	case FmtType2:
		return readFmt2MessageHeader(r, header)
	case FmtType3:
		return readFmt3MessageHeader(r, header)
	}
	return msgHeader{}, errors.New("format must be 0-3")
}

func readFmt0MessageHeader(r io.Reader, _ *msgHeader) (msgHeader, error) {
	buf := [FMT0_HEADER_SIZE]byte{}
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return msgHeader{}, err
	}

	timestamp := readUint24BE(buf[0:3])
	length := readUint24BE(buf[3:6])
	typeId := buf[6]
	streamID := binary.LittleEndian.Uint32(buf[7:11])

	timestamp, err := readTimestampIfExtended(r, timestamp)
	if err != nil {
		return msgHeader{}, err
	}

	return newMsgHeader(timestamp, length, typeId, streamID), nil
}

func readFmt1MessageHeader(r io.Reader, header *msgHeader) (msgHeader, error) {
	if header == nil {
		return msgHeader{}, fmt.Errorf("fmt=1 requires previous message header but none found")
	}

	buf := [FMT1_HEADER_SIZE]byte{}
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return msgHeader{}, err
	}

	timestampDelta := readUint24BE(buf[0:3])
	length := readUint24BE(buf[3:6])
	typeId := buf[6]

	timestampDelta, err := readTimestampIfExtended(r, timestampDelta)
	if err != nil {
		return msgHeader{}, err
	}

	newTimestamp := calculateNewTimestamp(header.timestamp, timestampDelta)

	return newMsgHeader(newTimestamp, length, typeId, header.streamID), nil
}

func readFmt2MessageHeader(r io.Reader, header *msgHeader) (msgHeader, error) {
	if header == nil {
		return msgHeader{}, fmt.Errorf("fmt=2 requires previous message header but none found")
	}

	buf := [FMT2_HEADER_SIZE]byte{}
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return msgHeader{}, err
	}

	timestampDelta := readUint24BE(buf[:])
	timestampDelta, err := readTimestampIfExtended(r, timestampDelta)
	if err != nil {
		return msgHeader{}, err
	}

	newTimestamp := calculateNewTimestamp(header.timestamp, timestampDelta)

	return newMsgHeader(newTimestamp, header.length, header.typeId, header.streamID), nil
}

func readFmt3MessageHeader(r io.Reader, header *msgHeader) (msgHeader, error) {
	if header == nil {
		return msgHeader{}, fmt.Errorf("fmt=3 requires previous message header but none found")
	}
	// FMT3은 이전 메시지의 헤더와 동일. 여기선 아무것도 읽지 않음
	return newMsgHeader(header.timestamp, header.length, header.typeId, header.streamID), nil
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

// Extended timestamp 공통 처리 함수
func readTimestampIfExtended(r io.Reader, timestamp uint32) (uint32, error) {
	if timestamp == ExtendedTimestampThreshold {
		return readExtendedTimestamp(r)
	}
	return timestamp, nil
}

// Delta timestamp 계산 공통 함수 (32비트 산술로 오버플로우 자동 처리)
func calculateNewTimestamp(baseTimestamp, timestampDelta uint32) uint32 {
	return baseTimestamp + timestampDelta
}


// --- 코덱 감지 및 헤더 크기 계산 함수들 ---

// getMediaHeaderSize 메시지 타입과 첫 바이트를 기준으로 미디어 헤더 크기 계산
func getMediaHeaderSize(msgTypeId uint8, firstByte byte) (int, error) {
	switch msgTypeId {
	case MsgTypeVideo:
		return getVideoHeaderSize(firstByte)
	case MsgTypeAudio:
		return getAudioHeaderSize(firstByte)
	default:
		return 0, fmt.Errorf("not a media message type: %d", msgTypeId)
	}
}

// getVideoHeaderSize 비디오 첫 바이트를 기준으로 헤더 크기 계산
func getVideoHeaderSize(firstByte byte) (int, error) {
	codecId := firstByte & 0x0F // 하위 4비트: Codec ID

	switch codecId {
	case 2: // Sorenson H.263
		return 1, nil // Frame Type (4bits) + Codec ID (4bits)
	case 3: // Screen Video v1
		return 1, nil // Frame Type (4bits) + Codec ID (4bits)
	case 4: // VP6
		return 1, nil // Frame Type (4bits) + Codec ID (4bits)
	case 5: // VP6 with alpha
		return 1, nil // Frame Type (4bits) + Codec ID (4bits)
	case 6: // Screen Video
		return 1, nil // Frame Type (4bits) + Codec ID (4bits)
	case 7: // AVC (H.264)
		return 5, nil // Frame Type (4bits) + Codec ID (4bits) + AVC Packet Type (8bits) + Composition Time (24bits)
	case 12: // HEVC (H.265)
		return 5, nil // Frame Type (4bits) + Codec ID (4bits) + HEVC Packet Type (8bits) + Composition Time (24bits)
	case 13: // AV1 (Enhanced RTMP)
		return 5, nil // Frame Type (4bits) + Codec ID (4bits) + AV1 Packet Type (8bits) + Composition Time (24bits)
	default:
		return 0, fmt.Errorf("unsupported video codec: %d", codecId)
	}
}

// getAudioHeaderSize 오디오 첫 바이트를 기준으로 헤더 크기 계산
func getAudioHeaderSize(firstByte byte) (int, error) {
	soundFormat := (firstByte & 0xF0) >> 4 // 상위 4비트: Sound Format

	switch soundFormat {
	case 2: // MP3
		return 1, nil // Sound Format (4bits) + Sound Rate (2bits) + Sound Size (1bit) + Sound Type (1bit)
	case 5: // Nellymoser 8kHz mono
		return 1, nil
	case 6: // Nellymoser
		return 1, nil
	case 7: // G.711 A-law
		return 1, nil
	case 8: // G.711 μ-law
		return 1, nil
	case 10: // AAC
		return 2, nil // Sound Format (4bits) + Sound Rate (2bits) + Sound Size (1bit) + Sound Type (1bit) + AAC Packet Type (8bits)
	case 11: // Speex
		return 1, nil
	case 13: // Opus (Enhanced RTMP)
		return 2, nil // Sound Format (4bits) + Sound Rate (2bits) + Sound Size (1bit) + Sound Type (1bit) + Opus Packet Type (8bits)
	case 14: // MP3 8kHz
		return 1, nil
	case 15: // Device-specific sound
		return 1, nil
	default:
		return 0, fmt.Errorf("unsupported audio format: %d", soundFormat)
	}
}

// detectVideoCodec 비디오 첫 바이트에서 코덱 감지
func detectVideoCodec(firstByte byte) (core.Codec, error) {
	codecId := firstByte & 0x0F

	switch codecId {
	case 2: // Sorenson H.263
		return core.Unknown, fmt.Errorf("sorenson H.263 codec not supported")
	case 3: // Screen Video v1
		return core.Unknown, fmt.Errorf("screen video v1 codec not supported")
	case 4: // VP6
		return core.Unknown, fmt.Errorf("VP6 codec not supported")
	case 5: // VP6 with alpha
		return core.Unknown, fmt.Errorf("VP6 with alpha codec not supported")
	case 6: // Screen Video
		return core.Unknown, fmt.Errorf("screen video codec not supported")
	case 7: // AVC (H.264)
		return core.H264, nil
	case 12: // HEVC (H.265)
		return core.H265, nil
	case 13: // AV1
		return core.Unknown, fmt.Errorf("AV1 codec not supported")
	default:
		return core.Unknown, fmt.Errorf("unknown video codec: %d", codecId)
	}
}

// detectAudioCodec 오디오 첫 바이트에서 코덱 감지
func detectAudioCodec(firstByte byte) (core.Codec, error) {
	soundFormat := (firstByte & 0xF0) >> 4

	switch soundFormat {
	case 2: // MP3
		return core.Unknown, fmt.Errorf("MP3 codec not supported")
	case 5: // Nellymoser 8kHz mono
		return core.Unknown, fmt.Errorf("Nellymoser codec not supported")
	case 6: // Nellymoser
		return core.Unknown, fmt.Errorf("Nellymoser codec not supported")
	case 7: // G.711 A-law
		return core.Unknown, fmt.Errorf("G.711 A-law codec not supported")
	case 8: // G.711 μ-law
		return core.Unknown, fmt.Errorf("G.711 μ-law codec not supported")
	case 10: // AAC
		return core.AAC, nil
	case 11: // Speex
		return core.Unknown, fmt.Errorf("Speex codec not supported")
	case 13: // Opus
		return core.Unknown, fmt.Errorf("Opus codec not supported")
	case 14: // MP3 8kHz
		return core.Unknown, fmt.Errorf("MP3 8kHz codec not supported")
	case 15: // Device-specific
		return core.Unknown, fmt.Errorf("device-specific codec not supported")
	default:
		return core.Unknown, fmt.Errorf("unknown audio format: %d", soundFormat)
	}
}

// 이하 msgReaderContext에서 통합된 메서드들

func (mr *msgReader) abortChunkStream(chunkStreamId uint32) {
	// 버퍼들 해제
	if buffers, exists := mr.payloads[chunkStreamId]; exists {
		for _, buffer := range buffers {
			buffer.Release()
		}
	}
	// AV 태그 헤더 해제
	if avTagHeader, exists := mr.avTagHeaders[chunkStreamId]; exists && avTagHeader != nil {
		avTagHeader.Release()
	}
	// 해당 청크 스트림의 모든 상태 제거
	delete(mr.msgHeaders, chunkStreamId)
	delete(mr.payloads, chunkStreamId)
	delete(mr.payloadLengths, chunkStreamId)
	delete(mr.avTagHeaders, chunkStreamId)
}

func (mr *msgReader) updateMsgHeader(chunkStreamId uint32, msgHeader *msgHeader) {
	mr.msgHeaders[chunkStreamId] = *msgHeader
}

// storeAVTagHeader AV 태그 헤더 저장
func (mr *msgReader) storeAVTagHeader(chunkStreamId uint32, header []byte) {
	// 기존 헤더가 있으면 해제
	if existingHeader := mr.avTagHeaders[chunkStreamId]; existingHeader != nil {
		existingHeader.Release()
	}
	
	// 새 버퍼 생성 및 데이터 복사
	buffer := core.NewBuffer(len(header))
	copy(buffer.Data(), header)
	mr.avTagHeaders[chunkStreamId] = buffer
}

// addMediaBuffer 미디어 버퍼를 추가
func (mr *msgReader) addMediaBuffer(chunkStreamId uint32, buffer *core.Buffer) {
	if mr.payloads[chunkStreamId] == nil {
		mr.payloads[chunkStreamId] = make([]*core.Buffer, 0)
	}

	mr.payloads[chunkStreamId] = append(mr.payloads[chunkStreamId], buffer)
	mr.payloadLengths[chunkStreamId] += uint32(len(buffer.Data()))
}

func (mr *msgReader) isInitialChunk(chunkStreamId uint32) bool {
	_, ok := mr.payloads[chunkStreamId]
	return !ok
}

func (mr *msgReader) nextChunkSize(chunkStreamId uint32) uint32 {
	header, ok := mr.msgHeaders[chunkStreamId]
	if !ok {
		slog.Error("message header not found", "chunkStreamId", chunkStreamId)
		return 0
	}

	// 현재 읽은 순수 데이터 길이
	currentPayloadLength := mr.payloadLengths[chunkStreamId]

	// 헤더 크기 계산
	headerSize := uint32(0)
	if avTagHeader, exists := mr.avTagHeaders[chunkStreamId]; exists && avTagHeader != nil {
		headerSize = uint32(avTagHeader.Len())
	}

	// 전체 메시지에서 헤더와 현재까지 읽은 데이터를 제외한 남은 크기
	totalRead := headerSize + currentPayloadLength
	remain := header.length - totalRead

	if remain > mr.chunkSize {
		return mr.chunkSize
	}
	return remain
}

func (mr *msgReader) getMsgHeader(chunkStreamId uint32) *msgHeader {
	header, ok := mr.msgHeaders[chunkStreamId]
	if !ok {
		return nil
	}
	return &header
}

func (mr *msgReader) popMessageIfPossible() (Message, error) {
	for chunkStreamId, messageHeader := range mr.msgHeaders {
		payloadLength, ok := mr.payloadLengths[chunkStreamId]
		if !ok {
			continue
		}

		buffers, ok := mr.payloads[chunkStreamId]
		if !ok {
			continue
		}

		// 헤더 크기 계산
		headerSize := uint32(0)
		if avTagHeader, exists := mr.avTagHeaders[chunkStreamId]; exists && avTagHeader != nil {
			headerSize = uint32(avTagHeader.Len())
		}

		// 전체 메시지 길이와 비교 (헤더 + 순수 데이터)
		if payloadLength+headerSize != messageHeader.length {
			continue
		}


		// 메시지 생성
		msg := NewMessage(messageHeader)

		// 비디오/오디오 메시지의 경우 AV 태그 헤더 정보 설정 (별도 저장)
		if avTagHeader, hasHeader := mr.avTagHeaders[chunkStreamId]; hasHeader && avTagHeader != nil {
			msg.avTagHeader = avTagHeader.AddRef() // 참조 카운트 증가
		}

		// 버퍼들을 직접 전달 (zero-copy)
		msg.payloads = make([]*core.Buffer, len(buffers))
		for i, buffer := range buffers {
			msg.payloads[i] = buffer.AddRef() // 참조 카운트 증가
		}

		// 원래 버퍼들 해제 (Message가 새로운 참조를 가지므로)
		for _, buffer := range buffers {
			buffer.Release()
		}

		// 상태 정리 (messageHeaders는 다음 메시지에서 참조할 수 있으므로 유지)
		delete(mr.payloads, chunkStreamId)
		delete(mr.payloadLengths, chunkStreamId)
		delete(mr.avTagHeaders, chunkStreamId)

		return msg, nil
	}

	return Message{}, fmt.Errorf("no complete message available")
}
