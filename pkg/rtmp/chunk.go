package rtmp

import "sol/pkg/core"

type Chunk struct {
	basicHeader basicHeader
	msgHeader   msgHeader
	payload     *core.Buffer
}

func NewChunk(basicHeader basicHeader, msgHeader msgHeader, payload *core.Buffer) Chunk {
	return Chunk{
		basicHeader: basicHeader,
		msgHeader:   msgHeader,
		payload:     payload,
	}
}

// Release 청크의 페이로드 버퍼 해제
func (c *Chunk) Release() {
	if c.payload != nil {
		c.payload.Release()
	}
}