package rtmp2

type Chunk struct {
	basicHeader   *basicHeader
	messageHeader *messageHeader
	payload       []byte
}

func NewChunk(basicHeader *basicHeader, messageHeader *messageHeader, payload []byte) *Chunk {
	c := &Chunk{
		basicHeader:   basicHeader,
		messageHeader: messageHeader,
		payload:       payload,
	}
	return c
}
