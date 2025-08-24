package rtmp

type msgHeader struct {
	timestamp uint32
	length    uint32
	typeId    uint8
	streamID  uint32
}

// newMsgHeader creates a new message header
func newMsgHeader(timestamp uint32, length uint32, typeId uint8, streamID uint32) msgHeader {
	return msgHeader{
		timestamp: timestamp,
		length:    length,
		typeId:    typeId,
		streamID:  streamID,
	}
}
