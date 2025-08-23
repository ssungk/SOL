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
	payloads       map[uint32][][]byte
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
		payloads:       make(map[uint32][][]byte),
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

// storeMediaHeader 미디어 헤더 저장
func (ms *messageReaderContext) storeMediaHeader(chunkStreamId uint32, header []byte) {
	ms.mediaHeaders[chunkStreamId] = header
}

// addNewChunk 새로운 청크를 추가
func (ms *messageReaderContext) addNewChunk(chunkStreamId uint32, chunkData []byte) {
	if ms.payloads[chunkStreamId] == nil {
		ms.payloads[chunkStreamId] = make([][]byte, 0)
	}
	ms.payloads[chunkStreamId] = append(ms.payloads[chunkStreamId], chunkData)
	// length 업데이트는 addMediaBuffer에서만 수행
}

// addMediaBuffer media.Buffer를 추가
func (ms *messageReaderContext) addMediaBuffer(chunkStreamId uint32, buffer *media.Buffer) {
	if ms.payload[chunkStreamId] == nil {
		ms.payload[chunkStreamId] = make([]*media.Buffer, 0)
	}
	ms.payload[chunkStreamId] = append(ms.payload[chunkStreamId], buffer)
	ms.payloadLengths[chunkStreamId] += uint32(buffer.Len())
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
	slog.Debug("popMessageIfPossible called", "headers_count", len(ms.messageHeaders))
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
			slog.Debug("Message incomplete", "chunkStreamId", chunkStreamId, 
				"payloadLength", payloadLength, "headerSize", headerSize, 
				"expectedLength", messageHeader.length)
			continue
		}
		
		slog.Info("Complete message found", "chunkStreamId", chunkStreamId, "typeId", messageHeader.typeId)

		// 메시지 생성
		var msg *Message

		// 원본 청크 배열 보관 (zero-copy용) - payload 수정 전에 보관
		originalChunks := make([][]byte, len(payload))
		copy(originalChunks, payload)

		// 비디오/오디오 메시지의 경우 payload에 순수 데이터만 저장 (기존 호환성)
		if mediaHeader, hasHeader := ms.mediaHeaders[chunkStreamId]; hasHeader {
			// 첫 번째 청크에 헤더 포함하여 처리 (기존 호환성)
			if len(payload) > 0 {
				// 첫 번째 청크에 헤더 결합
				firstChunk := make([]byte, len(mediaHeader)+len(payload[0]))
				copy(firstChunk, mediaHeader)
				copy(firstChunk[len(mediaHeader):], payload[0])
				payload[0] = firstChunk
			}

			// 전체 데이터 연결 (기존 호환성을 위해)
			totalLen := 0
			for _, chunk := range payload {
				totalLen += len(chunk)
			}
			fullPayload := make([]byte, totalLen)
			offset := 0
			for _, chunk := range payload {
				copy(fullPayload[offset:], chunk)
				offset += len(chunk)
			}

			msg = NewMessage(messageHeader, fullPayload)
			msg.mediaHeader = make([]byte, len(mediaHeader))
			copy(msg.mediaHeader, mediaHeader)
			if len(fullPayload) > len(mediaHeader) {
				msg.payload = fullPayload[len(mediaHeader):] // 헤더 제외한 부분
			}
		} else {
			// 컨트롤 메시지는 payload 전체 연결
			totalLen := 0
			for _, chunk := range payload {
				totalLen += len(chunk)
			}
			fullPayload := make([]byte, totalLen)
			offset := 0
			for _, chunk := range payload {
				copy(fullPayload[offset:], chunk)
				offset += len(chunk)
			}
			msg = NewMessage(messageHeader, fullPayload)
		}

		// 원본 청크 배열 설정 (zero-copy용)
		msg.chunks = originalChunks

		// 청크 배열은 Pool 사용하지 않으므로 반납 불필요

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
