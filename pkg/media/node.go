package media

// 모든 미디어 노드의 공통 인터페이스
type MediaNode interface {
	// Basic identification
	ID() uintptr
	NodeType() NodeType
	Address() string

	// Node lifecycle
	Close() error
}

// 미디어 소스 (publisher/encoder/file)
type MediaSource interface {
	MediaNode
	
	// 이 소스가 발행 중인 스트림 객체들 반환
	PublishingStreams() []*Stream
}

// 미디어 싱크 (viewer/decoder/file)
type MediaSink interface {
	MediaNode

	// SendMediaFrame sends a media frame to the sink
	SendMediaFrame(streamId string, frame Frame) error
	// SendMetadata sends metadata to the sink
	SendMetadata(streamId string, metadata map[string]string) error
	
	// 이 싱크가 구독 중인 스트림 ID 목록 반환
	SubscribedStreams() []string
	
	// 선호하는 포맷 타입 반환 (H264의 경우 AVCC 또는 StartCode)
	PreferredFormat(codecType CodecType) FormatType
}
