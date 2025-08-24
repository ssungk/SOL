package rtmp

import (
	"fmt"
	"log/slog"
	"sol/pkg/media"
)

type messageReaderContext struct {
	messageHeaders map[uint32]messageHeader
	payloads       map[uint32][]*media.Buffer
	payloadLengths map[uint32]uint32
	mediaHeaders   map[uint32][]byte // 미디어 헤더 (비디오: 5바이트, 오디오: 2바이트)
	chunkSize      uint32
}

func newMessageReaderContext() *messageReaderContext {
	return &messageReaderContext{
		messageHeaders: make(map[uint32]messageHeader),
		payloads:       make(map[uint32][]*media.Buffer),
		payloadLengths: make(map[uint32]uint32),
		mediaHeaders:   make(map[uint32][]byte),
		chunkSize:      DefaultChunkSize,
	}
}

func (mrc *messageReaderContext) setChunkSize(size uint32) {
	mrc.chunkSize = size
}

func (mrc *messageReaderContext) abortChunkStream(chunkStreamId uint32) {
	// 버퍼들 해제
	if buffers, exists := mrc.payloads[chunkStreamId]; exists {
		for _, buffer := range buffers {
			buffer.Release()
		}
	}
	// 해당 청크 스트림의 모든 상태 제거
	delete(mrc.messageHeaders, chunkStreamId)
	delete(mrc.payloads, chunkStreamId)
	delete(mrc.payloadLengths, chunkStreamId)
	delete(mrc.mediaHeaders, chunkStreamId)
}

func (ms *messageReaderContext) updateMsgHeader(chunkStreamId uint32, messageHeader *messageHeader) {
	ms.messageHeaders[chunkStreamId] = *messageHeader
}

// storeMediaHeader 미디어 헤더 저장
func (ms *messageReaderContext) storeMediaHeader(chunkStreamId uint32, header []byte) {
	ms.mediaHeaders[chunkStreamId] = header
}

// addMediaBuffer 미디어 버퍼를 추가
func (ms *messageReaderContext) addMediaBuffer(chunkStreamId uint32, buffer *media.Buffer) {
	if ms.payloads[chunkStreamId] == nil {
		ms.payloads[chunkStreamId] = make([]*media.Buffer, 0)
	}

	ms.payloads[chunkStreamId] = append(ms.payloads[chunkStreamId], buffer)
	ms.payloadLengths[chunkStreamId] += uint32(len(buffer.Data()))
}

func (ms *messageReaderContext) isInitialChunk(chunkStreamId uint32) bool {
	_, ok := ms.payloads[chunkStreamId]
	return !ok
}

func (ms *messageReaderContext) nextChunkSize(chunkStreamId uint32) uint32 {
	header, ok := ms.messageHeaders[chunkStreamId]
	if !ok {
		slog.Error("message header not found", "chunkStreamId", chunkStreamId)
		return 0
	}

	// 현재 읽은 순수 데이터 길이
	currentPayloadLength := ms.payloadLengths[chunkStreamId]

	// 헤더 크기 계산
	headerSize := uint32(0)
	if mediaHeader, exists := ms.mediaHeaders[chunkStreamId]; exists {
		headerSize = uint32(len(mediaHeader))
	}

	// 전체 메시지에서 헤더와 현재까지 읽은 데이터를 제외한 남은 크기
	totalRead := headerSize + currentPayloadLength
	remain := header.length - totalRead

	if remain > ms.chunkSize {
		return ms.chunkSize
	}
	return remain
}

func (ms *messageReaderContext) getMsgHeader(chunkStreamId uint32) *messageHeader {
	header, ok := ms.messageHeaders[chunkStreamId]
	if !ok {
		return nil
	}
	return &header
}

func (ms *messageReaderContext) popMessageIfPossible() (*Message, error) {
	for chunkStreamId, messageHeader := range ms.messageHeaders {
		payloadLength, ok := ms.payloadLengths[chunkStreamId]
		if !ok {
			continue
		}

		buffers, ok := ms.payloads[chunkStreamId]
		if !ok {
			continue
		}

		// 헤더 크기 계산
		headerSize := uint32(0)
		if mediaHeader, exists := ms.mediaHeaders[chunkStreamId]; exists {
			headerSize = uint32(len(mediaHeader))
		}

		// 전체 메시지 길이와 비교 (헤더 + 순수 데이터)
		if payloadLength+headerSize != messageHeader.length {
			continue
		}


		// 메시지 생성
		msg := NewMessage(messageHeader)

		// 비디오/오디오 메시지의 경우 미디어 헤더 정보 설정 (별도 저장)
		if mediaHeader, hasHeader := ms.mediaHeaders[chunkStreamId]; hasHeader {
			msg.mediaHeader = make([]byte, len(mediaHeader))
			copy(msg.mediaHeader, mediaHeader)
		}

		// 버퍼들을 직접 전달 (zero-copy)
		msg.payloads = make([]*media.Buffer, len(buffers))
		for i, buffer := range buffers {
			msg.payloads[i] = buffer.AddRef() // 참조 카운트 증가
		}

		// 원래 버퍼들 해제 (Message가 새로운 참조를 가지므로)
		for _, buffer := range buffers {
			buffer.Release()
		}

		// 상태 정리
		delete(ms.payloadLengths, chunkStreamId)
		delete(ms.payloads, chunkStreamId)
		delete(ms.mediaHeaders, chunkStreamId)

		return msg, nil
	}
	return nil, fmt.Errorf("no complete message available")
}
