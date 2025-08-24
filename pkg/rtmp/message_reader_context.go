package rtmp

import (
	"fmt"
	"log/slog"
	"sol/pkg/media"
)

type msgReaderContext struct {
	messageHeaders map[uint32]msgHeader
	payloads       map[uint32][]*media.Buffer
	payloadLengths map[uint32]uint32
	mediaHeaders   map[uint32][]byte // 미디어 헤더 (비디오: 5바이트, 오디오: 2바이트)
	chunkSize      uint32
}

func newMsgReaderContext() *msgReaderContext {
	return &msgReaderContext{
		messageHeaders: make(map[uint32]msgHeader),
		payloads:       make(map[uint32][]*media.Buffer),
		payloadLengths: make(map[uint32]uint32),
		mediaHeaders:   make(map[uint32][]byte),
		chunkSize:      DefaultChunkSize,
	}
}

func (mrc *msgReaderContext) setChunkSize(size uint32) {
	mrc.chunkSize = size
}

func (mrc *msgReaderContext) abortChunkStream(chunkStreamId uint32) {
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

func (mrc *msgReaderContext) updateMsgHeader(chunkStreamId uint32, messageHeader *msgHeader) {
	mrc.messageHeaders[chunkStreamId] = *messageHeader
}

// storeMediaHeader 미디어 헤더 저장
func (mrc *msgReaderContext) storeMediaHeader(chunkStreamId uint32, header []byte) {
	mrc.mediaHeaders[chunkStreamId] = header
}

// addMediaBuffer 미디어 버퍼를 추가
func (mrc *msgReaderContext) addMediaBuffer(chunkStreamId uint32, buffer *media.Buffer) {
	if mrc.payloads[chunkStreamId] == nil {
		mrc.payloads[chunkStreamId] = make([]*media.Buffer, 0)
	}

	mrc.payloads[chunkStreamId] = append(mrc.payloads[chunkStreamId], buffer)
	mrc.payloadLengths[chunkStreamId] += uint32(len(buffer.Data()))
}

func (mrc *msgReaderContext) isInitialChunk(chunkStreamId uint32) bool {
	_, ok := mrc.payloads[chunkStreamId]
	return !ok
}

func (mrc *msgReaderContext) nextChunkSize(chunkStreamId uint32) uint32 {
	header, ok := mrc.messageHeaders[chunkStreamId]
	if !ok {
		slog.Error("message header not found", "chunkStreamId", chunkStreamId)
		return 0
	}

	// 현재 읽은 순수 데이터 길이
	currentPayloadLength := mrc.payloadLengths[chunkStreamId]

	// 헤더 크기 계산
	headerSize := uint32(0)
	if mediaHeader, exists := mrc.mediaHeaders[chunkStreamId]; exists {
		headerSize = uint32(len(mediaHeader))
	}

	// 전체 메시지에서 헤더와 현재까지 읽은 데이터를 제외한 남은 크기
	totalRead := headerSize + currentPayloadLength
	remain := header.length - totalRead

	if remain > mrc.chunkSize {
		return mrc.chunkSize
	}
	return remain
}

func (mrc *msgReaderContext) getMsgHeader(chunkStreamId uint32) *msgHeader {
	header, ok := mrc.messageHeaders[chunkStreamId]
	if !ok {
		return nil
	}
	return &header
}

func (mrc *msgReaderContext) popMessageIfPossible() (*Message, error) {
	for chunkStreamId, messageHeader := range mrc.messageHeaders {
		payloadLength, ok := mrc.payloadLengths[chunkStreamId]
		if !ok {
			continue
		}

		buffers, ok := mrc.payloads[chunkStreamId]
		if !ok {
			continue
		}

		// 헤더 크기 계산
		headerSize := uint32(0)
		if mediaHeader, exists := mrc.mediaHeaders[chunkStreamId]; exists {
			headerSize = uint32(len(mediaHeader))
		}

		// 전체 메시지 길이와 비교 (헤더 + 순수 데이터)
		if payloadLength+headerSize != messageHeader.length {
			continue
		}


		// 메시지 생성
		mrcg := NewMessage(messageHeader)

		// 비디오/오디오 메시지의 경우 미디어 헤더 정보 설정 (별도 저장)
		if mediaHeader, hasHeader := mrc.mediaHeaders[chunkStreamId]; hasHeader {
			mrcg.mediaHeader = make([]byte, len(mediaHeader))
			copy(mrcg.mediaHeader, mediaHeader)
		}

		// 버퍼들을 직접 전달 (zero-copy)
		mrcg.payloads = make([]*media.Buffer, len(buffers))
		for i, buffer := range buffers {
			mrcg.payloads[i] = buffer.AddRef() // 참조 카운트 증가
		}

		// 원래 버퍼들 해제 (Message가 새로운 참조를 가지므로)
		for _, buffer := range buffers {
			buffer.Release()
		}

		// 상태 정리
		delete(mrc.payloadLengths, chunkStreamId)
		delete(mrc.payloads, chunkStreamId)
		delete(mrc.mediaHeaders, chunkStreamId)

		return mrcg, nil
	}
	return nil, fmt.Errorf("no complete message available")
}
