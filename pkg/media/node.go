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

// 미디어 데이터 소스 (publisher/encoder/file)
type MediaSource interface {
	MediaNode
}

// 미디어 데이터 싱크 (viewer/decoder/file)
type MediaSink interface {
	MediaNode

	// SendMediaFrame sends a media frame to the sink
	SendMediaFrame(streamId string, frame Frame) error
	// SendMetadata sends metadata to the sink
	SendMetadata(streamId string, metadata map[string]string) error
}

