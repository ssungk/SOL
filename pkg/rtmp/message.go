package rtmp

type Message struct {
	messageHeader *messageHeader
	payload       []byte

	// 미디어 메시지 전용 (비디오/오디오)
	mediaHeader []byte // 미디어 헤더 (비디오: 5바이트, 오디오: 2바이트)
	
	// 청크 배열 (zero-copy 처리용)
	chunks [][]byte
}

func NewMessage(messageHeader *messageHeader, payload []byte) *Message {
	return &Message{
		messageHeader: messageHeader,
		payload:       payload,
	}
}
