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
