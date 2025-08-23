package rtmp

import (
	"fmt"
	"log/slog"
	"sol/pkg/media"
	"sync"
)

const (
	SmallBufferSize  = 4 * 1024    // 4KB
	MediumBufferSize = 64 * 1024   // 64KB
	LargeBufferSize  = 1024 * 1024 // 1MB
)

type messageReaderContext struct {
	messageHeaders map[uint32]*messageHeader
	payload        map[uint32][]*media.Buffer
	payloads       map[uint32][]byte
	payloadLengths map[uint32]uint32
	pooledBuffers  map[uint32]*sync.Pool // 각 chunkStreamId별 pool 추적
	mediaHeaders   map[uint32][]byte     // 미디어 헤더 (비디오: 5바이트, 오디오: 2바이트)
	chunkSize      uint32

	// 크기별 Pool 계층
	smallPool  *sync.Pool // ~4KB
	mediumPool *sync.Pool // ~64KB
	largePool  *sync.Pool // ~1MB
}

func newMessageReaderContext() *messageReaderContext {
	return &messageReaderContext{
		messageHeaders: make(map[uint32]*messageHeader),
		payload:        make(map[uint32][]*media.Buffer),
		payloads:       make(map[uint32][]byte),
		payloadLengths: make(map[uint32]uint32),
		pooledBuffers:  make(map[uint32]*sync.Pool),
		mediaHeaders:   make(map[uint32][]byte),
		chunkSize:      DefaultChunkSize,

		// 크기별 Pool 계층 초기화
		smallPool:  NewBufferPool(SmallBufferSize),
		mediumPool: NewBufferPool(MediumBufferSize),
		largePool:  NewBufferPool(LargeBufferSize),
	}
}

func (mrc *messageReaderContext) setChunkSize(size uint32) {
	mrc.chunkSize = size
	// Pool 크기는 고정이므로 청크 크기 변경 시에도 기존 Pool 사용
}

func (mrc *messageReaderContext) abortChunkStream(chunkStreamId uint32) {
	// Pool 버퍼 반납 후 상태 제거
	if pool, exists := mrc.pooledBuffers[chunkStreamId]; exists {
		if payload, payloadExists := mrc.payloads[chunkStreamId]; payloadExists {
			pool.Put(payload[:cap(payload)]) // 전체 용량으로 반납
		}
	}

	// 해당 청크 스트림의 모든 상태 제거
	delete(mrc.messageHeaders, chunkStreamId)
	delete(mrc.payloads, chunkStreamId)
	delete(mrc.payloadLengths, chunkStreamId)
	delete(mrc.pooledBuffers, chunkStreamId)
	delete(mrc.mediaHeaders, chunkStreamId)
}

func (ms *messageReaderContext) updateMsgHeader(chunkStreamId uint32, messageHeader *messageHeader) {
	ms.messageHeaders[chunkStreamId] = messageHeader
}

// selectAppropriatePool 메시지 크기에 따라 적절한 Pool 선택
func (ms *messageReaderContext) selectAppropriatePool(size uint32) *sync.Pool {
	switch {
	case size <= SmallBufferSize:
		return ms.smallPool
	case size <= MediumBufferSize:
		return ms.mediumPool
	default:
		return ms.largePool
	}
}

// allocateMessageBuffer 순수 데이터 크기만큼 버퍼 할당 (헤더 크기 제외)
func (ms *messageReaderContext) allocateMessageBuffer(chunkStreamId uint32) []byte {
	header := ms.messageHeaders[chunkStreamId]
	if header == nil {
		return nil
	}

	// 비디오/오디오 메시지의 경우 헤더 크기를 제외한 순수 데이터 크기 계산
	payloadSize := header.length
	if header.typeId == MsgTypeVideo {
		if header.length < 5 {
			slog.Warn("Video message too short for header", "length", header.length)
		} else {
			payloadSize = header.length - 5 // 비디오 헤더 5바이트 제외
		}
	} else if header.typeId == MsgTypeAudio {
		if header.length < 2 {
			slog.Warn("Audio message too short for header", "length", header.length)
		} else {
			payloadSize = header.length - 2 // 오디오 헤더 2바이트 제외
		}
	}

	// 메시지 크기에 따라 적절한 Pool 선택 (전체 메시지 크기 기준)
	pool := ms.selectAppropriatePool(header.length)

	// Pool에서 버퍼 할당하고 순수 데이터 크기로 슬라이싱
	buf := pool.Get().([]byte)[:payloadSize]
	ms.payloads[chunkStreamId] = buf
	ms.payloadLengths[chunkStreamId] = 0
	ms.pooledBuffers[chunkStreamId] = pool // pool 추적

	return buf
}

// storeMediaHeader 미디어 헤더 저장
func (ms *messageReaderContext) storeMediaHeader(chunkStreamId uint32, header []byte) {
	ms.mediaHeaders[chunkStreamId] = header
}

// getMessageBuffer 현재 메시지 버퍼와 쓰기 위치 반환
func (ms *messageReaderContext) getMessageBuffer(chunkStreamId uint32) ([]byte, uint32) {
	buffer := ms.payloads[chunkStreamId]
	offset := ms.payloadLengths[chunkStreamId]
	return buffer, offset
}

// updatePayloadLength 읽은 데이터 길이 업데이트
func (ms *messageReaderContext) updatePayloadLength(chunkStreamId uint32, readSize uint32) {
	ms.payloadLengths[chunkStreamId] += readSize
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
	return header
}

func (ms *messageReaderContext) popMessageIfPossible() (*Message, error) {
	for chunkStreamId, messageHeader := range ms.messageHeaders {
		payloadLength, ok := ms.payloadLengths[chunkStreamId]
		if !ok {
			continue
		}

		payload, ok := ms.payloads[chunkStreamId]
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
		var msgPayload []byte
		var msg *Message

		// 비디오/오디오 메시지의 경우 payload에 순수 데이터만 저장
		if mediaHeader, hasHeader := ms.mediaHeaders[chunkStreamId]; hasHeader {
			// payload는 순수 데이터만 (헤더 제외)
			msgPayload = make([]byte, len(payload))
			copy(msgPayload, payload)

			// 전체 메시지 생성 (기존 호환성을 위해 헤더+데이터 합친 fullPayload 생성)
			fullPayload := make([]byte, len(mediaHeader)+len(payload))
			copy(fullPayload, mediaHeader)
			copy(fullPayload[len(mediaHeader):], payload)

			msg = NewMessage(messageHeader, fullPayload)

			// 미디어 헤더 저장 및 payload를 순수 데이터로 교체
			msg.mediaHeader = make([]byte, len(mediaHeader))
			copy(msg.mediaHeader, mediaHeader)
			msg.payload = msgPayload
		} else {
			// 컨트롤 메시지는 payload에 전체 데이터 저장
			msgPayload = make([]byte, len(payload))
			copy(msgPayload, payload)
			msg = NewMessage(messageHeader, msgPayload)
		}

		// Pool 버퍼 반납
		if pool, exists := ms.pooledBuffers[chunkStreamId]; exists {
			pool.Put(payload[:cap(payload)]) // 전체 용량으로 반납
			delete(ms.pooledBuffers, chunkStreamId)
		}

		// 상태 정리
		delete(ms.payloadLengths, chunkStreamId)
		delete(ms.payloads, chunkStreamId)
		delete(ms.mediaHeaders, chunkStreamId)

		return msg, nil
	}
	return nil, fmt.Errorf("no complete message available")
}

func NewBufferPool(size uint32) *sync.Pool {
	return &sync.Pool{
		New: func() any {
			return make([]byte, size)
		},
	}
}
