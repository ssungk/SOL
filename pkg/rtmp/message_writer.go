package rtmp

import (
	"encoding/binary"
	"fmt"
	"io"
	"sol/pkg/media"
	"sol/pkg/rtmp/amf"
)

type messageWriter struct {
	chunkSize uint32
}

func newMessageWriter() *messageWriter {
	return &messageWriter{
		chunkSize: DefaultChunkSize,
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
			// 오류 시 남은 청크들 해제
			for _, remainingChunk := range chunks {
				remainingChunk.Release()
			}
			return err
		}
		chunk.Release() // 전송 완료 후 청크 해제
	}

	return nil
}

// 메시지를 청크 배열로 구성 (zero-copy)
func (mw *messageWriter) buildChunks(msg *Message) ([]*Chunk, error) {
	// 실제 전송될 페이로드 길이 계산 (미디어 메시지는 헤더 포함)
	var totalPayloadLength int
	if msg.messageHeader.typeId == MsgTypeVideo || msg.messageHeader.typeId == MsgTypeAudio {
		totalPayloadLength = len(msg.FullPayload())
	} else {
		for _, buffer := range msg.payloads {
			totalPayloadLength += len(buffer.Data())
		}
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

// []byte payload에서 지정된 오프셋과 크기만큼 데이터를 추출 (zero-copy)
func extractPayloadSlice(payload []byte, offset, size int) []byte {
	if size == 0 || offset >= len(payload) {
		return nil
	}

	end := offset + size
	if end > len(payload) {
		end = len(payload)
	}

	return payload[offset:end]
}

// 메시지 타입에 따라 적절한 청크 스트림 ID를 결정
func getChunkStreamIDForMessageType(messageType byte) byte {
	switch messageType {
	case MsgTypeSetChunkSize, MsgTypeAbort, MsgTypeAcknowledgement,
		MsgTypeUserControl, MsgTypeWindowAckSize, MsgTypeSetPeerBW:
		return ChunkStreamProtocol
	case MsgTypeAudio:
		return ChunkStreamAudio
	case MsgTypeVideo:
		return ChunkStreamVideo
	case MsgTypeAMF0Data, MsgTypeAMF3Data:
		return ChunkStreamScript
	case MsgTypeAMF0Command, MsgTypeAMF3Command:
		return ChunkStreamCommand
	default:
		return ChunkStreamCommand // 기본값
	}
}

// 첫 번째 청크 생성 (fmt=0 - full header)
func (mw *messageWriter) buildFirstChunk(msg *Message, offset, chunkSize, totalPayloadLength int) *Chunk {
	// 확장 타임스탬프 처리
	headerTimestamp := msg.messageHeader.timestamp
	if msg.messageHeader.timestamp >= ExtendedTimestampThreshold {
		headerTimestamp = ExtendedTimestampThreshold
	}

	// 메시지 타입에 따라 청크 스트림 ID 결정
	chunkStreamID := getChunkStreamIDForMessageType(msg.messageHeader.typeId)
	basicHdr := NewBasicHeader(FmtType0, uint32(chunkStreamID))
	msgHdr := NewMessageHeader(
		headerTimestamp,
		uint32(totalPayloadLength),
		msg.messageHeader.typeId,
		msg.messageHeader.streamID,
	)

	// payload 버퍼 생성 - 미디어 메시지는 FullPayload 사용
	var payloadBuffer *media.Buffer
	if chunkSize > 0 {
		var payloadSlice []byte
		if msg.messageHeader.typeId == MsgTypeVideo || msg.messageHeader.typeId == MsgTypeAudio {
			payloadSlice = extractPayloadSlice(msg.FullPayload(), offset, chunkSize)
		} else {
			payloadSlice = extractPayloadSlice(msg.Payload(), offset, chunkSize)
		}
		payloadBuffer = media.NewBuffer(len(payloadSlice))
		copy(payloadBuffer.Data(), payloadSlice)
	} else {
		payloadBuffer = media.NewBuffer(0)
	}

	return NewChunk(basicHdr, msgHdr, payloadBuffer)
}

// 연속 청크 생성 (fmt=3 - no header)
func (mw *messageWriter) buildContinuationChunk(msg *Message, offset, chunkSize int) *Chunk {
	// 메시지 타입에 따라 청크 스트림 ID 결정
	chunkStreamID := getChunkStreamIDForMessageType(msg.messageHeader.typeId)
	basicHdr := NewBasicHeader(FmtType3, uint32(chunkStreamID))

	// Type 3는 message header가 없음
	var msgHdr *messageHeader = nil

	// payload 버퍼 생성 - 미디어 메시지는 FullPayload 사용
	var payloadBuffer *media.Buffer
	var payloadSlice []byte
	if msg.messageHeader.typeId == MsgTypeVideo || msg.messageHeader.typeId == MsgTypeAudio {
		payloadSlice = extractPayloadSlice(msg.FullPayload(), offset, chunkSize)
	} else {
		payloadSlice = extractPayloadSlice(msg.Payload(), offset, chunkSize)
	}
	payloadBuffer = media.NewBuffer(len(payloadSlice))
	copy(payloadBuffer.Data(), payloadSlice)

	return NewChunk(basicHdr, msgHdr, payloadBuffer)
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
		if chunk.messageHeader.timestamp >= ExtendedTimestampThreshold {
			needsExtendedTimestamp = true
			extendedTimestamp = chunk.messageHeader.timestamp
		}
	}

	// Extended Timestamp 전송 (필요한 경우)
	if needsExtendedTimestamp {
		if err := mw.writeExtendedTimestamp(w, extendedTimestamp); err != nil {
			return err
		}
	}

	// Payload 전송 (zero-copy)
	if chunk.payload != nil && len(chunk.payload.Data()) > 0 {
		if _, err := w.Write(chunk.payload.Data()); err != nil {
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
	PutUint24(header[0:], mh.timestamp)                    // 3 bytes timestamp
	PutUint24(header[3:], mh.length)                       // 3 bytes message length
	header[6] = mh.typeId                                  // 1 byte type ID
	binary.LittleEndian.PutUint32(header[7:], mh.streamID) // 4 bytes stream ID
	_, err := w.Write(header)
	return err
}

func (mw *messageWriter) writeCommand(w io.Writer, payload []byte) error {
	header := NewMessageHeader(0, uint32(len(payload)), MsgTypeAMF0Command, 0)
	msg := NewMessage(header)
	buffer := media.NewBuffer(len(payload))
	copy(buffer.Data(), payload)
	msg.payloads = []*media.Buffer{buffer}
	return mw.writeMessage(w, msg)
}

func (mw *messageWriter) writeSetChunkSize(w io.Writer, chunkSize uint32) error {
	// 페이로드 생성 (4바이트 빅엔디안)
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, chunkSize)

	header := NewMessageHeader(0, 4, MsgTypeSetChunkSize, 0)
	msg := NewMessage(header)
	buffer := media.NewBuffer(len(payload))
	copy(buffer.Data(), payload)
	msg.payloads = []*media.Buffer{buffer}

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
func (mw *messageWriter) writeAudioData(w io.Writer, audioData []byte, timestamp uint32) error {
	header := NewMessageHeader(timestamp, uint32(len(audioData)), MsgTypeAudio, 0)
	msg := NewMessage(header)
	audioBuffer := media.NewBuffer(len(audioData))
	copy(audioBuffer.Data(), audioData)
	msg.payloads = []*media.Buffer{audioBuffer}
	return mw.writeMessage(w, msg)
}

// 비디오 데이터 전송 (zero-copy)
func (mw *messageWriter) writeVideoData(w io.Writer, videoData []byte, timestamp uint32) error {
	header := NewMessageHeader(timestamp, uint32(len(videoData)), MsgTypeVideo, 0)
	msg := NewMessage(header)
	videoBuffer := media.NewBuffer(len(videoData))
	copy(videoBuffer.Data(), videoData)
	msg.payloads = []*media.Buffer{videoBuffer}
	return mw.writeMessage(w, msg)
}

// 메타데이터 전송
func (mw *messageWriter) writeScriptData(w io.Writer, commandName string, metadata map[string]any) error {
	// AMF 데이터 인코딩
	payload, err := amf.EncodeAMF0Sequence(commandName, metadata)
	if err != nil {
		return err
	}

	header := NewMessageHeader(0, uint32(len(payload)), MsgTypeAMF0Data, 0) // 메타데이터는 timestamp 0
	msg := NewMessage(header)
	buffer := media.NewBuffer(len(payload))
	copy(buffer.Data(), payload)
	msg.payloads = []*media.Buffer{buffer}
	return mw.writeMessage(w, msg)
}

