package rtmp

import (
	"encoding/binary"
	"fmt"
	"io"
	"sol/pkg/core"
	"sol/pkg/rtmp/amf"
)

type msgWriter struct {
	chunkSize uint32
}

func newMsgWriter() *msgWriter {
	return &msgWriter{
		chunkSize: DefaultChunkSize,
	}
}

// 공통 메시지 쓰기 함수 - 모든 RTMP 메시지는 이 함수를 통해 전송
func (mw *msgWriter) writeMessage(w io.Writer, msg *Message) error {
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
func (mw *msgWriter) buildChunks(msg *Message) ([]*Chunk, error) {
	// Reader와 전체 길이 결정
	var totalPayloadLength int
	var payloadReader io.Reader
	if msg.msgHeader.typeId == MsgTypeVideo || msg.msgHeader.typeId == MsgTypeAudio {
		totalPayloadLength = msg.TotalFullPayloadLen()
		payloadReader = msg.FullReader()
	} else {
		totalPayloadLength = msg.TotalPayloadLen()
		payloadReader = msg.Reader()
	}

	if totalPayloadLength == 0 {
		// 페이로드가 없는 메시지 (예: Set Chunk Size)
		return []*Chunk{mw.buildFirstChunk(msg, nil, 0, totalPayloadLength)}, nil
	}

	var chunks []*Chunk
	bytesRead := 0
	isFirstChunk := true

	for bytesRead < totalPayloadLength {
		chunkSize := int(mw.chunkSize)
		remaining := totalPayloadLength - bytesRead
		if remaining < chunkSize {
			chunkSize = remaining
		}

		// Reader에서 청크 크기만큼 읽기
		chunkData := make([]byte, chunkSize)
		n, err := io.ReadFull(payloadReader, chunkData)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("failed to read chunk data: %w", err)
		}
		
		// 실제 읽은 크기로 조정
		if n < chunkSize {
			chunkData = chunkData[:n]
		}

		if isFirstChunk {
			// 첫 번째 청크: Full header (fmt=0)
			chunks = append(chunks, mw.buildFirstChunk(msg, chunkData, n, totalPayloadLength))
			isFirstChunk = false
		} else {
			// 나머지 청크: Type 3 header (fmt=3)
			chunks = append(chunks, mw.buildContinuationChunk(msg, chunkData, n))
		}

		bytesRead += n
		if n == 0 {
			break // 더 이상 읽을 데이터가 없음
		}
	}

	return chunks, nil
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
func (mw *msgWriter) buildFirstChunk(msg *Message, chunkData []byte, chunkSize, totalPayloadLength int) *Chunk {
	// 확장 타임스탬프 처리
	headerTimestamp := msg.msgHeader.timestamp
	if msg.msgHeader.timestamp >= ExtendedTimestampThreshold {
		headerTimestamp = ExtendedTimestampThreshold
	}

	// 메시지 타입에 따라 청크 스트림 ID 결정
	chunkStreamID := getChunkStreamIDForMessageType(msg.msgHeader.typeId)
	basicHdr := newBasicHeader(FmtType0, uint32(chunkStreamID))
	msgHdr := newMsgHeader(
		headerTimestamp,
		uint32(totalPayloadLength),
		msg.msgHeader.typeId,
		msg.msgHeader.streamID,
	)

	// payload 버퍼 생성
	var payloadBuffer *core.Buffer
	if chunkSize > 0 && chunkData != nil {
		payloadBuffer = core.NewBuffer(len(chunkData))
		copy(payloadBuffer.Data(), chunkData)
	} else {
		payloadBuffer = core.NewBuffer(0)
	}

	return NewChunk(basicHdr, &msgHdr, payloadBuffer)
}

// 연속 청크 생성 (fmt=3 - no header)
func (mw *msgWriter) buildContinuationChunk(msg *Message, chunkData []byte, chunkSize int) *Chunk {
	// 메시지 타입에 따라 청크 스트림 ID 결정
	chunkStreamID := getChunkStreamIDForMessageType(msg.msgHeader.typeId)
	basicHdr := newBasicHeader(FmtType3, uint32(chunkStreamID))

	// Type 3는 message header가 없음
	var msgHdr *msgHeader = nil

	// payload 버퍼 생성
	payloadBuffer := core.NewBuffer(len(chunkData))
	copy(payloadBuffer.Data(), chunkData)

	return NewChunk(basicHdr, msgHdr, payloadBuffer)
}

// 단일 청크 전송
func (mw *msgWriter) writeChunk(w io.Writer, chunk *Chunk) error {
	// Basic Header 전송
	if err := mw.writeBasicHeader(w, chunk.basicHeader); err != nil {
		return err
	}

	// Message Header 전송 (fmt=3인 경우 nil)
	var needsExtendedTimestamp bool
	var extendedTimestamp uint32
	if chunk.msgHeader != nil {
		if err := mw.writeMessageHeader(w, chunk.msgHeader); err != nil {
			return err
		}
		// 확장 타임스탬프가 필요한지 확인
		if chunk.msgHeader.timestamp >= ExtendedTimestampThreshold {
			needsExtendedTimestamp = true
			extendedTimestamp = chunk.msgHeader.timestamp
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
func (mw *msgWriter) writeExtendedTimestamp(w io.Writer, timestamp uint32) error {
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, timestamp)
	_, err := w.Write(header)
	return err
}

// Basic Header 인코딩 및 전송
func (mw *msgWriter) writeBasicHeader(w io.Writer, bh basicHeader) error {
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
func (mw *msgWriter) writeMessageHeader(w io.Writer, mh *msgHeader) error {
	header := make([]byte, 11)
	PutUint24(header[0:], mh.timestamp)                    // 3 bytes timestamp
	PutUint24(header[3:], mh.length)                       // 3 bytes message length
	header[6] = mh.typeId                                  // 1 byte type ID
	binary.LittleEndian.PutUint32(header[7:], mh.streamID) // 4 bytes stream ID
	_, err := w.Write(header)
	return err
}

func (mw *msgWriter) writeCommand(w io.Writer, payload []byte) error {
	header := newMsgHeader(0, uint32(len(payload)), MsgTypeAMF0Command, 0)
	msg := NewMessage(header)
	buffer := core.NewBuffer(len(payload))
	copy(buffer.Data(), payload)
	msg.payloads = []*core.Buffer{buffer}
	return mw.writeMessage(w, msg)
}

func (mw *msgWriter) writeSetChunkSize(w io.Writer, chunkSize uint32) error {
	// 페이로드 생성 (4바이트 빅엔디안)
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, chunkSize)

	header := newMsgHeader(0, 4, MsgTypeSetChunkSize, 0)
	msg := NewMessage(header)
	buffer := core.NewBuffer(len(payload))
	copy(buffer.Data(), payload)
	msg.payloads = []*core.Buffer{buffer}

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
func (mw *msgWriter) writeAudioData(w io.Writer, audioData []byte, timestamp uint32) error {
	header := newMsgHeader(timestamp, uint32(len(audioData)), MsgTypeAudio, 0)
	msg := NewMessage(header)
	audioBuffer := core.NewBuffer(len(audioData))
	copy(audioBuffer.Data(), audioData)
	msg.payloads = []*core.Buffer{audioBuffer}
	return mw.writeMessage(w, msg)
}

// 비디오 데이터 전송 (zero-copy)
func (mw *msgWriter) writeVideoData(w io.Writer, videoData []byte, timestamp uint32) error {
	header := newMsgHeader(timestamp, uint32(len(videoData)), MsgTypeVideo, 0)
	msg := NewMessage(header)
	videoBuffer := core.NewBuffer(len(videoData))
	copy(videoBuffer.Data(), videoData)
	msg.payloads = []*core.Buffer{videoBuffer}
	return mw.writeMessage(w, msg)
}

// 메타데이터 전송
func (mw *msgWriter) writeScriptData(w io.Writer, commandName string, metadata map[string]any) error {
	// AMF 데이터 인코딩
	payload, err := amf.EncodeAMF0Sequence(commandName, metadata)
	if err != nil {
		return err
	}

	header := newMsgHeader(0, uint32(len(payload)), MsgTypeAMF0Data, 0) // 메타데이터는 timestamp 0
	msg := NewMessage(header)
	buffer := core.NewBuffer(len(payload))
	copy(buffer.Data(), payload)
	msg.payloads = []*core.Buffer{buffer}
	return mw.writeMessage(w, msg)
}