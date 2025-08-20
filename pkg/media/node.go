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

	// SendFrame sends a frame to the sink
	SendFrame(streamID string, frame Frame) error
	// SendMetadata sends metadata to the sink
	SendMetadata(streamID string, metadata map[string]string) error
	
	// 이 싱크가 구독 중인 스트림 ID 목록 반환
	SubscribedStreams() []string
}
