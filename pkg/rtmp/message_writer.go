package rtmp

import (
	"encoding/binary"
	"io"
	"sol/pkg/amf"
)

// RTMP 메시지 구조체
type rtmpMessage struct {
	ChunkStreamID   byte
	MessageTypeID   byte
	MessageStreamID uint32
	Timestamp       uint32
	Payload         []byte
}

type messageWriter struct {
	chunkSize uint32
}

func newMessageWriter() *messageWriter {
	return &messageWriter{
		chunkSize: DEFAULT_CHUNK_SIZE,
	}
}

// 공통 메시지 쓰기 함수 - 모든 RTMP 메시지는 이 함수를 통해 전송
func (mw *messageWriter) writeMessage(w io.Writer, msg *rtmpMessage) error {
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
func (mw *messageWriter) buildChunks(msg *rtmpMessage) ([]*Chunk, error) {
	payloadLength := len(msg.Payload)
	if payloadLength == 0 {
		// 페이로드가 없는 메시지 (예: Set Chunk Size)
		return []*Chunk{mw.buildFirstChunk(msg, 0, 0)}, nil
	}

	var chunks []*Chunk
	offset := 0

	for offset < payloadLength {
		chunkSize := int(mw.chunkSize)
		remaining := payloadLength - offset
		if remaining < chunkSize {
			chunkSize = remaining
		}

		if offset == 0 {
			// 첫 번째 청크: Full header (fmt=0)
			chunks = append(chunks, mw.buildFirstChunk(msg, offset, chunkSize))
		} else {
			// 나머지 청크: Type 3 header (fmt=3)
			chunks = append(chunks, mw.buildContinuationChunk(msg, offset, chunkSize))
		}

		offset += chunkSize
	}

	return chunks, nil
}

// 첫 번째 청크 생성 (fmt=0 - full header)
func (mw *messageWriter) buildFirstChunk(msg *rtmpMessage, offset, chunkSize int) *Chunk {
	// 확장 타임스탬프 처리
	headerTimestamp := msg.Timestamp
	if msg.Timestamp >= EXTENDED_TIMESTAMP_THRESHOLD {
		headerTimestamp = EXTENDED_TIMESTAMP_THRESHOLD
	}

	basicHdr := newBasicHeader(FMT_TYPE_0, uint32(msg.ChunkStreamID)) // fmt=0
	msgHdr := newMessageHeader(
		headerTimestamp,
		uint32(len(msg.Payload)),
		msg.MessageTypeID,
		msg.MessageStreamID,
	)

	// payload 슬라이스 (복사 없이 참조)
	var payloadSlice []byte
	if chunkSize > 0 {
		payloadSlice = msg.Payload[offset : offset+chunkSize]
	}

	return NewChunk(basicHdr, msgHdr, payloadSlice)
}

// 연속 청크 생성 (fmt=3 - no header)
func (mw *messageWriter) buildContinuationChunk(msg *rtmpMessage, offset, chunkSize int) *Chunk {
	basicHdr := newBasicHeader(FMT_TYPE_3, uint32(msg.ChunkStreamID)) // fmt=3

	// Type 3는 message header가 없음
	var msgHdr *messageHeader = nil

	// payload 슬라이스 (복사 없이 참조)
	payloadSlice := msg.Payload[offset : offset+chunkSize]

	return NewChunk(basicHdr, msgHdr, payloadSlice)
}

// 단일 청크 전송
func (mw *messageWriter) writeChunk(w io.Writer, chunk *Chunk) error {
	// Basic Header 전송
	if err := mw.writeBasicHeader(w, chunk.basicHeader); err != nil {
		return err
	}

	// Message Header 전송 (fmt=3인 경우 nil)
	if chunk.messageHeader != nil {
		if err := mw.writeMessageHeader(w, chunk.messageHeader); err != nil {
			return err
		}
	}

	// Extended Timestamp 전송 (필요한 경우)
	// TODO: 확장 타임스탬프 처리 추가 필요

	// Payload 전송 (zero-copy)
	if len(chunk.payload) > 0 {
		if _, err := w.Write(chunk.payload); err != nil {
			return err
		}
	}

	return nil
}

// Basic Header 인코딩 및 전송
func (mw *messageWriter) writeBasicHeader(w io.Writer, bh *basicHeader) error {
	// 1바이트 basic header 인코딩
	header := []byte{(bh.fmt << 6) | byte(bh.chunkStreamID)}
	_, err := w.Write(header)
	return err
}

// Message Header 인코딩 및 전송
func (mw *messageWriter) writeMessageHeader(w io.Writer, mh *messageHeader) error {
	header := make([]byte, 11)
	PutUint24(header[0:], mh.Timestamp)   // 3 bytes timestamp
	PutUint24(header[3:], mh.length)      // 3 bytes message length
	header[6] = mh.typeId                 // 1 byte type ID
	binary.LittleEndian.PutUint32(header[7:], mh.streamId) // 4 bytes stream ID
	_, err := w.Write(header)
	return err
}

func (mw *messageWriter) writeCommand(w io.Writer, payload []byte) error {
	msg := &rtmpMessage{
		ChunkStreamID:   CHUNK_STREAM_COMMAND,
		MessageTypeID:   MSG_TYPE_AMF0_COMMAND,
		MessageStreamID: 0,
		Timestamp:       0,
		Payload:         payload,
	}
	return mw.writeMessage(w, msg)
}

func (mw *messageWriter) writeSetChunkSize(w io.Writer, chunkSize uint32) error {
	// 페이로드 생성 (4바이트 빅엔디안)
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, chunkSize)

	msg := &rtmpMessage{
		ChunkStreamID:   CHUNK_STREAM_PROTOCOL,
		MessageTypeID:   MSG_TYPE_SET_CHUNK_SIZE,
		MessageStreamID: 0,
		Timestamp:       0,
		Payload:         payload,
	}

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

// 오디오 데이터 전송
func (mw *messageWriter) writeAudioData(w io.Writer, audioData []byte, timestamp uint32) error {
	msg := &rtmpMessage{
		ChunkStreamID:   CHUNK_STREAM_AUDIO,
		MessageTypeID:   MSG_TYPE_AUDIO,
		MessageStreamID: 0,
		Timestamp:       timestamp,
		Payload:         audioData,
	}
	return mw.writeMessage(w, msg)
}

// 비디오 데이터 전송
func (mw *messageWriter) writeVideoData(w io.Writer, videoData []byte, timestamp uint32) error {
	msg := &rtmpMessage{
		ChunkStreamID:   CHUNK_STREAM_VIDEO,
		MessageTypeID:   MSG_TYPE_VIDEO,
		MessageStreamID: 0,
		Timestamp:       timestamp,
		Payload:         videoData,
	}
	return mw.writeMessage(w, msg)
}

// 메타데이터 전송
func (mw *messageWriter) writeScriptData(w io.Writer, commandName string, metadata map[string]any) error {
	// AMF 데이터 인코딩
	payload, err := amf.EncodeAMF0Sequence(commandName, metadata)
	if err != nil {
		return err
	}

	msg := &rtmpMessage{
		ChunkStreamID:   CHUNK_STREAM_SCRIPT,
		MessageTypeID:   MSG_TYPE_AMF0_DATA,
		MessageStreamID: 0,
		Timestamp:       0, // 메타데이터는 timestamp 0
		Payload:         payload,
	}
	return mw.writeMessage(w, msg)
}
