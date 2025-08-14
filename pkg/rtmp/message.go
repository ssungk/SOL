package rtmp

type Message struct {
	messageHeader *messageHeader
	payload       []byte
	
	// RTMP 미디어 메시지 전용 (비디오/오디오)
	rtmpHeader    []byte // RTMP 태그 헤더 (1-5바이트)
	rawData       []byte // 순수 미디어 데이터 (H.264, AAC 등)
}

func NewMessage(messageHeader *messageHeader, payload []byte) *Message {
	msg := &Message{
		messageHeader: messageHeader,
		payload:       payload,
	}
	
	// 비디오/오디오 메시지인 경우 헤더와 데이터 분리
	if messageHeader.typeId == MsgTypeVideo || messageHeader.typeId == MsgTypeAudio {
		msg.parseMediaMessage()
	}
	
	return msg
}

// NewMediaMessage 미디어 프레임용 메시지 생성자
func NewMediaMessage(msgType uint8, streamId uint32, timestamp uint32, data []byte) *Message {
	return &Message{
		messageHeader: &messageHeader{
			timestamp: timestamp,
			length:    uint32(len(data)),
			typeId:    msgType,
			streamId:  streamId,
		},
		payload: data,
	}
}

// parseMediaMessage 비디오/오디오 메시지에서 RTMP 헤더와 순수 데이터 분리
func (m *Message) parseMediaMessage() {
	if len(m.payload) == 0 {
		return
	}
	
	switch m.messageHeader.typeId {
	case MsgTypeVideo:
		// 비디오: [Frame Type+Codec][AVC Packet Type][Composition Time 3바이트][H.264 Data...]
		if len(m.payload) >= 5 {
			m.rtmpHeader = m.payload[:5]
			m.rawData = m.payload[5:]
		} else {
			// 헤더가 불완전한 경우 전체를 rawData로 처리
			m.rawData = m.payload
		}
		
	case MsgTypeAudio:
		// 오디오: [Audio Info][AAC Packet Type][AAC Data...]  
		if len(m.payload) >= 2 {
			m.rtmpHeader = m.payload[:2]
			m.rawData = m.payload[2:]
		} else {
			// 헤더가 불완전한 경우 전체를 rawData로 처리
			m.rawData = m.payload
		}
	}
}

