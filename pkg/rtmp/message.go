package rtmp

import "sol/pkg/media"

type Message struct {
	messageHeader *messageHeader

	// 미디어 메시지 전용 (비디오/오디오)
	mediaHeader []byte // 미디어 헤더 (비디오: 5바이트, 오디오: 2바이트)
	
	// 청크 배열 (zero-copy 처리용)
	payloads []*media.Buffer
}

func NewMessage(messageHeader *messageHeader) *Message {
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
