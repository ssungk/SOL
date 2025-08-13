package rtmp

type Message struct {
	messageHeader *messageHeader
	payload       [][]byte
}

func NewMessage(messageHeader *messageHeader, payload [][]byte) *Message {
	msg := &Message{
		messageHeader: messageHeader,
		payload:       payload,
	}
	return msg
}

// NewMediaMessage 미디어 프레임용 메시지 생성자
func NewMediaMessage(msgType uint8, streamId uint32, timestamp uint32, data [][]byte, dataSize uint32) *Message {
	return &Message{
		messageHeader: &messageHeader{
			timestamp: timestamp,
			length:    dataSize,
			typeId:    msgType,
			streamId:  streamId,
		},
		payload: data,
	}
}
