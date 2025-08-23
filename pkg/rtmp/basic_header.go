package rtmp

type basicHeader struct {
	fmt           byte
	chunkStreamID uint32
}

// newBasicHeader creates a new basic header
func newBasicHeader(fmt byte, chunkStreamID uint32) basicHeader {
	return basicHeader{
		fmt:           fmt,
		chunkStreamID: chunkStreamID,
	}
}
