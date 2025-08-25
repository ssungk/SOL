package rtmp

import "sol/pkg/core"

// sendPacketEvent MediaSink로부터 미디어 패킷을 수신했을 때 발생하는 이벤트
type sendPacketEvent struct {
	streamPath string
	packet     core.Packet
}

// sendMetadataEvent MediaSink로부터 메타데이터를 수신했을 때 발생하는 이벤트
type sendMetadataEvent struct {
	streamPath string
	metadata   map[string]string
}

// commandEvent handleRead 고루틴에서 디코딩된 RTMP 메시지를 받았을 때 발생하는 이벤트
type commandEvent struct {
	message *Message
}