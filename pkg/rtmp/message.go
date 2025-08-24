package rtmp

import (
	"bytes"
	"encoding/binary"
	"io"
	"sol/pkg/media"
)

type Message struct {
	messageHeader msgHeader

	// 미디어 메시지 전용 (비디오/오디오)
	mediaHeader []byte // 미디어 헤더 (비디오: 5바이트, 오디오: 2바이트)
	
	// 청크 배열 (zero-copy 처리용)
	payloads []*media.Buffer
}

func NewMessage(messageHeader msgHeader) *Message {
	return &Message{
		messageHeader: messageHeader,
	}
}

// Payload 버퍼들을 연결하여 단일 바이트 슬라이스로 반환 (순수 데이터만)
func (m *Message) Payload() []byte {
	if len(m.payloads) == 0 {
		return nil
	}
	
	if len(m.payloads) == 1 {
		return m.payloads[0].Data()
	}
	
	// 여러 버퍼를 연결
	totalLen := 0
	for _, buffer := range m.payloads {
		totalLen += len(buffer.Data())
	}
	
	result := make([]byte, totalLen)
	offset := 0
	for _, buffer := range m.payloads {
		data := buffer.Data()
		copy(result[offset:], data)
		offset += len(data)
	}
	
	return result
}

// FullPayload 미디어 헤더 + 버퍼들을 연결하여 완전한 RTMP 페이로드 반환
func (m *Message) FullPayload() []byte {
	payloadData := m.Payload()
	headerLen := len(m.mediaHeader)
	payloadLen := len(payloadData)
	
	if headerLen == 0 && payloadLen == 0 {
		return nil
	}
	
	// 헤더 + 데이터 결합
	result := make([]byte, headerLen+payloadLen)
	if headerLen > 0 {
		copy(result, m.mediaHeader)
	}
	if payloadLen > 0 {
		copy(result[headerLen:], payloadData)
	}
	
	return result
}


// Release 모든 버퍼의 참조 카운트를 감소시킴
func (m *Message) Release() {
	for _, buffer := range m.payloads {
		if buffer != nil {
			buffer.Release()
		}
	}
}

// TotalPayloadLen 전체 페이로드 길이를 계산 (메모리 할당 없이)
func (m *Message) TotalPayloadLen() int {
	total := 0
	for _, buffer := range m.payloads {
		total += len(buffer.Data())
	}
	return total
}

// GetFixedSizePayload 고정 크기 페이로드 직접 접근 (zero-copy 시도)
func (m *Message) GetFixedSizePayload(expectedSize int) ([]byte, bool) {
	if len(m.payloads) == 1 {
		data := m.payloads[0].Data()
		if len(data) == expectedSize {
			return data, true // zero-copy 성공
		}
	}
	
	// 다중 버퍼이거나 크기가 다른 경우 - fallback 필요 신호
	if m.TotalPayloadLen() == expectedSize {
		return nil, false // fallback 사용하라는 신호
	}
	
	return nil, false
}

// GetUint32FromPayload 4바이트 페이로드에서 uint32 추출 (zero-copy 시도)
func (m *Message) GetUint32FromPayload() (uint32, bool) {
	if data, ok := m.GetFixedSizePayload(4); ok {
		return binary.BigEndian.Uint32(data), true
	}
	return 0, false
}

// Reader 메시지 페이로드를 io.Reader로 반환 (zero-copy 최적화)
func (m *Message) Reader() io.Reader {
	if len(m.payloads) == 0 {
		return bytes.NewReader(nil)
	}
	
	// MultiReader 사용하여 연결된 reader 제공 (단일/다중 버퍼 통합 처리)
	readers := make([]io.Reader, len(m.payloads))
	for i, buffer := range m.payloads {
		readers[i] = bytes.NewReader(buffer.Data())
	}
	return io.MultiReader(readers...)
}
