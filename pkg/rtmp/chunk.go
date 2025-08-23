package rtmp

import "sol/pkg/media"

type Chunk struct {
	basicHeader   basicHeader
	messageHeader *messageHeader
	payload       *media.Buffer
}

func NewChunk(basicHeader basicHeader, messageHeader *messageHeader, payload *media.Buffer) *Chunk {
	c := &Chunk{
		basicHeader:   basicHeader,
		messageHeader: messageHeader,
		payload:       payload,
	}
	return c
}

// Release 청크의 페이로드 버퍼 해제
func (c *Chunk) Release() {
	if c.payload != nil {
		c.payload.Release()
	}
}