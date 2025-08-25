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
	avTagHeader *media.Buffer // AV 태그 헤더 (비디오: 5바이트, 오디오: 2바이트)

	// 청크 배열 (zero-copy 처리용)
	payloads []*media.Buffer
}

func NewMessage(msgHeader msgHeader) *Message {
	return &Message{
		messageHeader: msgHeader,
	}
}

// Release 모든 버퍼의 참조 카운트를 감소시킴
func (m *Message) Release() {
	if m.avTagHeader != nil {
		m.avTagHeader.Release()
	}
	for _, buffer := range m.payloads {
		buffer.Release()
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

	// 다중 버퍼이거나 크기가 다른 경우
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

// FullReader AV 태그 헤더 + 페이로드 데이터를 하나의 Reader로 제공 (zero-copy)
func (m *Message) FullReader() io.Reader {
	if m.avTagHeader == nil || m.avTagHeader.Len() == 0 {
		return m.Reader()
	}
	// AV 태그 헤더 + 페이로드 데이터 결합된 Reader
	headerReader := bytes.NewReader(m.avTagHeader.Data())
	return io.MultiReader(headerReader, m.Reader())
}

// TotalFullPayloadLen AV 태그 헤더 포함 전체 페이로드 길이
func (m *Message) TotalFullPayloadLen() int {
	tagHeaderLen := 0
	if m.avTagHeader != nil {
		tagHeaderLen = m.avTagHeader.Len()
	}
	return tagHeaderLen + m.TotalPayloadLen()
}
