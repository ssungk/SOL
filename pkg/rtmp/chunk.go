package rtmp

import "sol/pkg/media"

type Chunk struct {
	basicHeader   basicHeader
	messageHeader *msgHeader
	payload       *media.Buffer
	hasPayload    bool // payload 유효성 플래그
}

func NewChunk(basicHeader basicHeader, messageHeader *msgHeader, payload *media.Buffer) *Chunk {
	hasPayload := payload != nil // nil 체크로 유효성 확인
	c := &Chunk{
		basicHeader:   basicHeader,
		messageHeader: messageHeader,
		payload:       payload,
		hasPayload:    hasPayload,
	}
	return c
}

// Release 청크의 페이로드 버퍼 해제
func (c *Chunk) Release() {
	if c.hasPayload {
		c.payload.Release()
	}
}