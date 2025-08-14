package rtmp

type Message struct {
	messageHeader *messageHeader
	payload       []byte
	
	// RTMP 미디어 메시지 전용 (비디오/오디오)
	rtmpHeader    []byte // RTMP 태그 헤더 (1-5바이트)
	rawData       []byte // 순수 미디어 데이터 (H.264, AAC 등)
}

func NewMessage(messageHeader *messageHeader, payload []byte) *Message {
	return &Message{
		messageHeader: messageHeader,
		payload:       payload,
	}
}

// NewMediaMessage 미디어 프레임용 메시지 생성자
func NewMediaMessage(msgType uint8, streamId uint32, timestamp uint32, data []byte) *Message {
	return &Message{
		messageHeader: &messageHeader{
			timestamp: timestamp,
			length:    uint32(len(data)),
			typeId:    msgType,
			streamId:  streamId,
		},
		payload: data,
	}
}


