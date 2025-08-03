package stream

// MediaType represents the type of media
type MediaType uint8

const (
	MediaTypeAudio MediaType = iota + 1
	MediaTypeVideo
	MediaTypeMetadata
)

// MediaFrame represents a generic media frame
type MediaFrame struct {
	Type      MediaType   // Media type (audio/video/metadata)
	FrameType string      // Frame type string (e.g., "key frame", "AAC sequence header")
	Timestamp uint32      // Frame timestamp
	Data      [][]byte    // Zero-copy payload chunks
}

// VideoFrame represents a video frame with video-specific information
type VideoFrame struct {
	MediaFrame
	IsKeyFrame       bool   // Whether this is a key frame
	IsSequenceHeader bool   // Whether this is a sequence header (e.g., AVC sequence header)
}

// AudioFrame represents an audio frame with audio-specific information
type AudioFrame struct {
	MediaFrame
	IsSequenceHeader bool   // Whether this is a sequence header (e.g., AAC sequence header)
}

// MetadataFrame represents metadata information
type MetadataFrame struct {
	Data map[string]any // Metadata key-value pairs
}

// StreamEvent represents various stream events
type StreamEvent interface {
	GetStreamName() string
	GetSessionId() string
}

// MediaDataEvent represents media data being published
type MediaDataEvent struct {
	SessionId  string
	StreamName string
	Frame      MediaFrame
}

func (e MediaDataEvent) GetStreamName() string { return e.StreamName }
func (e MediaDataEvent) GetSessionId() string  { return e.SessionId }

// MetadataEvent represents metadata being published
type MetadataEvent struct {
	SessionId  string
	StreamName string
	Metadata   MetadataFrame
}

func (e MetadataEvent) GetStreamName() string { return e.StreamName }
func (e MetadataEvent) GetSessionId() string  { return e.SessionId }

// Source disconnection event
type SourceDisconnectedEvent struct {
	SessionId string
	Reason    string
}

func (e SourceDisconnectedEvent) GetStreamName() string { return "" }
func (e SourceDisconnectedEvent) GetSessionId() string  { return e.SessionId }

// Destination disconnection event
type DestinationDisconnectedEvent struct {
	SessionId string
	Reason    string
}

func (e DestinationDisconnectedEvent) GetStreamName() string { return "" }
func (e DestinationDisconnectedEvent) GetSessionId() string  { return e.SessionId }

// Destination error event
type DestinationErrorEvent struct {
	SessionId string
	Error     error
}

func (e DestinationErrorEvent) GetStreamName() string { return "" }
func (e DestinationErrorEvent) GetSessionId() string  { return e.SessionId }

// Session represents a generic session interface
type Session interface {
	GetSessionId() string
	SendMediaFrame(frame MediaFrame) error
	SendMetadata(metadata MetadataFrame) error
	IsActive() bool
}
