package stream

// StreamDst represents a stream destination interface (player)
type StreamDst interface {
	Session
	
	// Additional destination methods
	GetDstInfo() string
	
	// External channel setup for event-driven communication
	SetExternalChannel(chan<- interface{})
}
