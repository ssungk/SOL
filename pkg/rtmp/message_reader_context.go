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
	payloads       map[uint32][]byte
	payloadLengths map[uint32]uint32
	chunkSize      uint32
	
	// 크기별 Pool 계층
	smallPool   *sync.Pool // ~4KB
	mediumPool  *sync.Pool // ~64KB  
	largePool   *sync.Pool // ~1MB
	poolManager *media.PoolManager
}

func newMessageReaderContext() *messageReaderContext {
	return &messageReaderContext{
		messageHeaders: make(map[uint32]*messageHeader),
		payloads:       make(map[uint32][]byte),
		payloadLengths: make(map[uint32]uint32),
		chunkSize:      DefaultChunkSize,
		
		// 크기별 Pool 계층 초기화
		smallPool:   NewBufferPool(SmallBufferSize),
		mediumPool:  NewBufferPool(MediumBufferSize),
		largePool:   NewBufferPool(LargeBufferSize),
		poolManager: media.NewPoolManager(),
	}
}

func (mrc *messageReaderContext) setChunkSize(size uint32) {
	mrc.chunkSize = size
	// Pool 크기는 고정이므로 청크 크기 변경 시에도 기존 Pool 사용
}

func (mrc *messageReaderContext) abortChunkStream(chunkStreamId uint32) {
	// 해당 청크 스트림의 모든 상태 제거
	delete(mrc.messageHeaders, chunkStreamId)
	delete(mrc.payloads, chunkStreamId)
	delete(mrc.payloadLengths, chunkStreamId)
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

// allocateMessageBuffer 전체 메시지 크기만큼 버퍼 할당 (제로카피를 위해)
func (ms *messageReaderContext) allocateMessageBuffer(chunkStreamId uint32) []byte {
	header := ms.messageHeaders[chunkStreamId]
	if header == nil {
		return nil
	}
	
	// 메시지 크기에 따라 적절한 Pool 선택
	pool := ms.selectAppropriatePool(header.length)
	
	// PoolManager를 통해 전체 메시지 크기만큼 버퍼 할당
	pb := ms.poolManager.AllocateBuffer(pool, header.length)
	ms.payloads[chunkStreamId] = pb.Data
	ms.payloadLengths[chunkStreamId] = 0
	
	return pb.Data
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

		msg := NewMessage(messageHeader, payload)
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
