package stream

// StreamSrc represents a stream source interface (publisher)
type StreamSrc interface {
	// Basic identification
	GetSessionId() string
	GetProtocol() string
	GetSrcInfo() string
	IsActive() bool
	
	// External channel setup for event-driven communication
	SetExternalChannel(chan<- interface{})
	
	// Lifecycle management
	Close() error
}
