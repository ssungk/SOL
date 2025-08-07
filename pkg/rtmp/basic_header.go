package rtmp

type basicHeader struct {
	fmt           byte
	chunkStreamID uint32
}

// NewBasicHeader creates a new basic header
func NewBasicHeader(fmt byte, chunkStreamID uint32) *basicHeader {
	return &basicHeader{
		fmt:           fmt,
		chunkStreamID: chunkStreamID,
	}
}
