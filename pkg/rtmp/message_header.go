package rtmp

type messageHeader struct {
	timestamp uint32
	length    uint32
	typeId    uint8
	streamID  uint32
}

// newMessageHeader creates a new message header
func newMessageHeader(timestamp uint32, length uint32, typeId uint8, streamID uint32) *messageHeader {
	return &messageHeader{
		timestamp: timestamp,
		length:    length,
		typeId:    typeId,
		streamID:  streamID,
	}
}
