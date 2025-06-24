package rtmp

type basicHeader struct {
	fmt           byte
	chunkStreamID uint32
}

func newBasicHeader(fmt byte, chunkStreamID uint32) *basicHeader {
	return &basicHeader{
		fmt:           fmt,
		chunkStreamID: chunkStreamID,
	}
}
