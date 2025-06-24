package rtmp

type messageHeader struct {
	Timestamp uint32
	length    uint32
	typeId    uint8
	streamId  uint32
}

func newMessageHeader(Timestamp uint32, length uint32, typeId uint8, streamId uint32) *messageHeader {
	return &messageHeader{
		Timestamp: Timestamp,
		length:    length,
		typeId:    typeId,
		streamId:  streamId,
	}
}
