package rtmp2

import (
	"encoding/binary"
	"fmt"
	"io"
	"sol/pkg/amf"
)

type messageWriter struct {
	chunkSize uint32
}

func newMessageWriter() *messageWriter {
	return &messageWriter{
		chunkSize: DEFAULT_CHUNK_SIZE,
	}
}

// 공통 메시지 쓰기 함수 - 모든 RTMP 메시지는 이 함수를 통해 전송
func (mw *messageWriter) writeMessage(w io.Writer, msg *Message) error {
	chunks, err := mw.buildChunks(msg)
	if err != nil {
		return err
	}

	// 모든 청크를 순차적으로 전송 (zero-copy)
	for _, chunk := range chunks {
		if err := mw.writeChunk(w, chunk); err != nil {
			return err
		}
	}

	return nil
}

// 메시지를 청크 배열로 구성 (zero-copy)
func (mw *messageWriter) buildChunks(msg *Message) ([]*Chunk, error) {
	// payload의 전체 길이 계산
	totalPayloadLength := 0
	for _, chunk := range msg.payload {
		totalPayloadLength += len(chunk)
	}

	if totalPayloadLength == 0 {
		// 페이로드가 없는 메시지 (예: Set Chunk Size)
		return []*Chunk{mw.buildFirstChunk(msg, 0, 0, totalPayloadLength)}, nil
	}

	var chunks []*Chunk
	offset := 0

	for offset < totalPayloadLength {
		chunkSize := int(mw.chunkSize)
		remaining := totalPayloadLength - offset
		if remaining < chunkSize {
			chunkSize = remaining
		}

		if offset == 0 {
			// 첫 번째 청크: Full header (fmt=0)
			chunks = append(chunks, mw.buildFirstChunk(msg, offset, chunkSize, totalPayloadLength))
		} else {
			// 나머지 청크: Type 3 header (fmt=3)
			chunks = append(chunks, mw.buildContinuationChunk(msg, offset, chunkSize))
		}

		offset += chunkSize
	}

	return chunks, nil
}

// [][]byte payload에서 지정된 오프셋과 크기만큼 데이터를 추출 (zero-copy)
func extractPayloadSlice(payload [][]byte, offset, size int) []byte {
	if size == 0 {
		return nil
	}

	// 전체 페이로드를 하나로 합치기 (필요시에만)
	var result []byte
	currentOffset := 0
	startFound := false
	remaining := size

	for _, chunk := range payload {
		chunkLen := len(chunk)

		if !startFound {
			if currentOffset+chunkLen <= offset {
				// 아직 시작 지점에 도달하지 않음
				currentOffset += chunkLen
				continue
			}
			// 시작 지점을 찾음
			startFound = true
			startIdx := offset - currentOffset
			copyLen := chunkLen - startIdx
			if copyLen > remaining {
				copyLen = remaining
			}
			result = append(result, chunk[startIdx:startIdx+copyLen]...)
			remaining -= copyLen
		} else {
			// 이미 시작지점을 지나서 계속 복사
			copyLen := chunkLen
			if copyLen > remaining {
				copyLen = remaining
			}
			result = append(result, chunk[:copyLen]...)
			remaining -= copyLen
		}

		if remaining <= 0 {
			break
		}
		currentOffset += chunkLen
	}

	return result
}

// 메시지 타입에 따라 적절한 청크 스트림 ID를 결정
func getChunkStreamIDForMessageType(messageType byte) byte {
	switch messageType {
	case MSG_TYPE_SET_CHUNK_SIZE, MSG_TYPE_ABORT, MSG_TYPE_ACKNOWLEDGEMENT,
		MSG_TYPE_USER_CONTROL, MSG_TYPE_WINDOW_ACK_SIZE, MSG_TYPE_SET_PEER_BW:
		return CHUNK_STREAM_PROTOCOL
	case MSG_TYPE_AUDIO:
		return CHUNK_STREAM_AUDIO
	case MSG_TYPE_VIDEO:
		return CHUNK_STREAM_VIDEO
	case MSG_TYPE_AMF0_DATA, MSG_TYPE_AMF3_DATA:
		return CHUNK_STREAM_SCRIPT
	case MSG_TYPE_AMF0_COMMAND, MSG_TYPE_AMF3_COMMAND:
		return CHUNK_STREAM_COMMAND
	default:
		return CHUNK_STREAM_COMMAND // 기본값
	}
}

// 첫 번째 청크 생성 (fmt=0 - full header)
func (mw *messageWriter) buildFirstChunk(msg *Message, offset, chunkSize, totalPayloadLength int) *Chunk {
	// 확장 타임스탬프 처리
	headerTimestamp := msg.messageHeader.Timestamp
	if msg.messageHeader.Timestamp >= EXTENDED_TIMESTAMP_THRESHOLD {
		headerTimestamp = EXTENDED_TIMESTAMP_THRESHOLD
	}

	// 메시지 타입에 따라 청크 스트림 ID 결정
	chunkStreamID := getChunkStreamIDForMessageType(msg.messageHeader.typeId)
	basicHdr := newBasicHeader(FMT_TYPE_0, uint32(chunkStreamID))
	msgHdr := newMessageHeader(
		headerTimestamp,
		uint32(totalPayloadLength),
		msg.messageHeader.typeId,
		msg.messageHeader.streamId,
	)

	// payload 슬라이스 (복사 없이 참조)
	var payloadSlice []byte
	if chunkSize > 0 {
		payloadSlice = extractPayloadSlice(msg.payload, offset, chunkSize)
	}

	return NewChunk(basicHdr, msgHdr, payloadSlice)
}

// 연속 청크 생성 (fmt=3 - no header)
func (mw *messageWriter) buildContinuationChunk(msg *Message, offset, chunkSize int) *Chunk {
	// 메시지 타입에 따라 청크 스트림 ID 결정
	chunkStreamID := getChunkStreamIDForMessageType(msg.messageHeader.typeId)
	basicHdr := newBasicHeader(FMT_TYPE_3, uint32(chunkStreamID))

	// Type 3는 message header가 없음
	var msgHdr *messageHeader = nil

	// payload 슬라이스 (복사 없이 참조)
	payloadSlice := extractPayloadSlice(msg.payload, offset, chunkSize)

	return NewChunk(basicHdr, msgHdr, payloadSlice)
}

// 단일 청크 전송
func (mw *messageWriter) writeChunk(w io.Writer, chunk *Chunk) error {
	// Basic Header 전송
	if err := mw.writeBasicHeader(w, chunk.basicHeader); err != nil {
		return err
	}

	// Message Header 전송 (fmt=3인 경우 nil)
	var needsExtendedTimestamp bool
	var extendedTimestamp uint32
	if chunk.messageHeader != nil {
		if err := mw.writeMessageHeader(w, chunk.messageHeader); err != nil {
			return err
		}
		// 확장 타임스탬프가 필요한지 확인
		if chunk.messageHeader.Timestamp >= EXTENDED_TIMESTAMP_THRESHOLD {
			needsExtendedTimestamp = true
			extendedTimestamp = chunk.messageHeader.Timestamp
		}
	}

	// Extended Timestamp 전송 (필요한 경우)
	if needsExtendedTimestamp {
		if err := mw.writeExtendedTimestamp(w, extendedTimestamp); err != nil {
			return err
		}
	}

	// Payload 전송 (zero-copy)
	if len(chunk.payload) > 0 {
		if _, err := w.Write(chunk.payload); err != nil {
			return err
		}
	}

	return nil
}

// Extended Timestamp 인코딩 및 전송
func (mw *messageWriter) writeExtendedTimestamp(w io.Writer, timestamp uint32) error {
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, timestamp)
	_, err := w.Write(header)
	return err
}

// Basic Header 인코딩 및 전송
func (mw *messageWriter) writeBasicHeader(w io.Writer, bh *basicHeader) error {
	var header []byte

	if bh.chunkStreamID < 64 {
		// 1바이트 basic header (chunk stream ID: 2-63)
		header = []byte{(bh.fmt << 6) | byte(bh.chunkStreamID)}
	} else if bh.chunkStreamID < 320 {
		// 2바이트 basic header (chunk stream ID: 64-319)
		header = make([]byte, 2)
		header[0] = (bh.fmt << 6) | 0 // chunk stream ID = 0 의미
		header[1] = byte(bh.chunkStreamID - 64)
	} else if bh.chunkStreamID < 65600 {
		// 3바이트 basic header (chunk stream ID: 320-65599)
		header = make([]byte, 3)
		header[0] = (bh.fmt << 6) | 1 // chunk stream ID = 1 의미
		value := bh.chunkStreamID - 64
		header[1] = byte(value & 0xFF)        // 하위 8비트
		header[2] = byte((value >> 8) & 0xFF) // 상위 8비트
	} else {
		return fmt.Errorf("chunk stream ID %d exceeds maximum allowed value (65599)", bh.chunkStreamID)
	}

	_, err := w.Write(header)
	return err
}

// Message Header 인코딩 및 전송
func (mw *messageWriter) writeMessageHeader(w io.Writer, mh *messageHeader) error {
	header := make([]byte, 11)
	PutUint24(header[0:], mh.Timestamp)                    // 3 bytes timestamp
	PutUint24(header[3:], mh.length)                       // 3 bytes message length
	header[6] = mh.typeId                                  // 1 byte type ID
	binary.LittleEndian.PutUint32(header[7:], mh.streamId) // 4 bytes stream ID
	_, err := w.Write(header)
	return err
}

func (mw *messageWriter) writeCommand(w io.Writer, payload []byte) error {
	header := newMessageHeader(0, uint32(len(payload)), MSG_TYPE_AMF0_COMMAND, 0)
	msg := NewMessage(header, [][]byte{payload})
	return mw.writeMessage(w, msg)
}

func (mw *messageWriter) writeSetChunkSize(w io.Writer, chunkSize uint32) error {
	// 페이로드 생성 (4바이트 빅엔디안)
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, chunkSize)

	header := newMessageHeader(0, 4, MSG_TYPE_SET_CHUNK_SIZE, 0)
	msg := NewMessage(header, [][]byte{payload})

	if err := mw.writeMessage(w, msg); err != nil {
		return err
	}

	// 청크 크기 업데이트
	mw.chunkSize = chunkSize
	return nil
}

func PutUint24(b []byte, v uint32) {
	b[0] = byte((v >> 16) & 0xFF)
	b[1] = byte((v >> 8) & 0xFF)
	b[2] = byte(v & 0xFF)
}

// 오디오 데이터 전송 (zero-copy)
func (mw *messageWriter) writeAudioData(w io.Writer, audioData [][]byte, timestamp uint32) error {
	// 전체 데이터 크기 계산
	totalLength := 0
	for _, chunk := range audioData {
		totalLength += len(chunk)
	}

	header := newMessageHeader(timestamp, uint32(totalLength), MSG_TYPE_AUDIO, 0)
	msg := NewMessage(header, audioData) // [][]byte 그대로 전달
	return mw.writeMessage(w, msg)
}

// 비디오 데이터 전송 (zero-copy)
func (mw *messageWriter) writeVideoData(w io.Writer, videoData [][]byte, timestamp uint32) error {
	// 전체 데이터 크기 계산
	totalLength := 0
	for _, chunk := range videoData {
		totalLength += len(chunk)
	}

	header := newMessageHeader(timestamp, uint32(totalLength), MSG_TYPE_VIDEO, 0)
	msg := NewMessage(header, videoData) // [][]byte 그대로 전달
	return mw.writeMessage(w, msg)
}

// 메타데이터 전송
func (mw *messageWriter) writeScriptData(w io.Writer, commandName string, metadata map[string]any) error {
	// AMF 데이터 인코딩
	payload, err := amf.EncodeAMF0Sequence(commandName, metadata)
	if err != nil {
		return err
	}

	header := newMessageHeader(0, uint32(len(payload)), MSG_TYPE_AMF0_DATA, 0) // 메타데이터는 timestamp 0
	msg := NewMessage(header, [][]byte{payload})
	return mw.writeMessage(w, msg)
}

// writeVideoMessage 비디오 메시지 전송 (pkg/media Frame 지원)
func (mw *messageWriter) writeVideoMessage(w io.Writer, msg *Message) error {
	return mw.writeMessage(w, msg)
}

// writeAudioMessage 오디오 메시지 전송 (pkg/media Frame 지원)
func (mw *messageWriter) writeAudioMessage(w io.Writer, msg *Message) error {
	return mw.writeMessage(w, msg)
}

// writeScriptMessage 스크립트 데이터 메시지 전송 (pkg/media MetadataFrame 지원)
func (mw *messageWriter) writeScriptMessage(w io.Writer, msg *Message) error {
	return mw.writeMessage(w, msg)
}
