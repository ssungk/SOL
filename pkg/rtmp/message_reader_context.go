package rtmp

import (
	"fmt"
	"log/slog"
	"sync"
)

const (
	SmallBufferSize  = 4 * 1024    // 4KB
	MediumBufferSize = 64 * 1024   // 64KB  
	LargeBufferSize  = 1024 * 1024 // 1MB
)

type messageReaderContext struct {
	messageHeaders map[uint32]*messageHeader
	payloads       map[uint32][]byte
	payloadLengths map[uint32]uint32
	pooledBuffers  map[uint32]*sync.Pool // 각 chunkStreamId별 pool 추적
	chunkSize      uint32
	
	// 크기별 Pool 계층
	smallPool  *sync.Pool // ~4KB
	mediumPool *sync.Pool // ~64KB  
	largePool  *sync.Pool // ~1MB
}

func newMessageReaderContext() *messageReaderContext {
	return &messageReaderContext{
		messageHeaders: make(map[uint32]*messageHeader),
		payloads:       make(map[uint32][]byte),
		payloadLengths: make(map[uint32]uint32),
		pooledBuffers:  make(map[uint32]*sync.Pool),
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

// allocateMessageBuffer 전체 메시지 크기만큼 버퍼 할당
func (ms *messageReaderContext) allocateMessageBuffer(chunkStreamId uint32) []byte {
	header := ms.messageHeaders[chunkStreamId]
	if header == nil {
		return nil
	}
	
	// 메시지 크기에 따라 적절한 Pool 선택
	pool := ms.selectAppropriatePool(header.length)
	
	// Pool에서 버퍼 할당하고 메시지 크기로 슬라이싱
	buf := pool.Get().([]byte)[:header.length]
	ms.payloads[chunkStreamId] = buf
	ms.payloadLengths[chunkStreamId] = 0
	ms.pooledBuffers[chunkStreamId] = pool // pool 추적
	
	return buf
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
	currentLength := ms.payloadLengths[chunkStreamId]
	remain := header.length - currentLength
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

		if payloadLength != messageHeader.length {
			continue
		}

		// 메시지 생성 (payload 데이터 복사)
		payloadCopy := make([]byte, len(payload))
		copy(payloadCopy, payload)
		msg := NewMessage(messageHeader, payloadCopy)
		
		// Pool 버퍼 반납
		if pool, exists := ms.pooledBuffers[chunkStreamId]; exists {
			pool.Put(payload[:cap(payload)]) // 전체 용량으로 반납
			delete(ms.pooledBuffers, chunkStreamId)
		}
		
		// 상태 정리
		delete(ms.payloadLengths, chunkStreamId)
		delete(ms.payloads, chunkStreamId)
		
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
