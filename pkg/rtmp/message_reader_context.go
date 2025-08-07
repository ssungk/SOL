package rtmp

import (
	"fmt"
	"log/slog"
	"sol/pkg/media"
	"sync"
)

type messageReaderContext struct {
	messageHeaders map[uint32]*messageHeader
	payloads       map[uint32][][]byte
	payloadLengths map[uint32]uint32
	chunkSize      uint32
	bufferPool     *sync.Pool
	poolManager    *media.PoolManager // Pool manager 추가
}

func newMessageReaderContext() *messageReaderContext {
	return &messageReaderContext{
		messageHeaders: make(map[uint32]*messageHeader),
		payloads:       make(map[uint32][][]byte),
		payloadLengths: make(map[uint32]uint32),
		chunkSize:      DEFAULT_CHUNK_SIZE,
		bufferPool:     NewBufferPool(DEFAULT_CHUNK_SIZE),
		poolManager:    media.NewPoolManager(), // Pool manager 초기화
	}
}

func (mrc *messageReaderContext) setChunkSize(size uint32) {
	mrc.chunkSize = size
	mrc.bufferPool = NewBufferPool(mrc.chunkSize)
}

func (ms *messageReaderContext) updateMsgHeader(chunkStreamId uint32, messageHeader *messageHeader) {
	ms.messageHeaders[chunkStreamId] = messageHeader
}

func (ms *messageReaderContext) appendPayload(chunkStreamId uint32, payload []byte) {
	ms.payloads[chunkStreamId] = append(ms.payloads[chunkStreamId], payload)
	ms.payloadLengths[chunkStreamId] = ms.payloadLengths[chunkStreamId] + uint32(len(payload))
}

func (ms *messageReaderContext) isInitialChunk(chunkStreamId uint32) bool {
	_, ok := ms.payloads[chunkStreamId]
	return !ok
}

func (ms *messageReaderContext) nextChunkSize(chunkStreamId uint32) uint32 {
	header, ok := ms.messageHeaders[chunkStreamId]
	if !ok {
		slog.Warn("message header not found", "chunkStreamId", chunkStreamId)
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
