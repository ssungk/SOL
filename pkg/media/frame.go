package media

// Type represents the type of media
type Type uint8

const (
	TypeAudio Type = iota + 1
	TypeVideo
	TypeMetadata
)

// Frame represents a generic media frame
type Frame struct {
	Type      Type        // Media type (audio/video/metadata)
	FrameType string      // Frame type string (e.g., "key frame", "AAC sequence header")
	Timestamp uint32      // Frame timestamp in milliseconds
	Data      [][]byte    // Zero-copy payload chunks
}

// VideoFrame represents a video frame with video-specific information
type VideoFrame struct {
	Frame
	IsKeyFrame       bool   // Whether this is a key frame
	IsSequenceHeader bool   // Whether this is a sequence header (e.g., AVC sequence header)
}

// AudioFrame represents an audio frame with audio-specific information
type AudioFrame struct {
	Frame
	IsSequenceHeader bool   // Whether this is a sequence header (e.g., AAC sequence header)
}

// NewVideoFrame creates a new video frame
func NewVideoFrame(frameType string, timestamp uint32, data [][]byte) VideoFrame {
	return VideoFrame{
		Frame: Frame{
			Type:      TypeVideo,
			FrameType: frameType,
			Timestamp: timestamp,
			Data:      data,
		},
		IsKeyFrame:       IsKeyFrame(frameType),
		IsSequenceHeader: IsVideoSequenceHeader(frameType),
	}
}

// NewAudioFrame creates a new audio frame
func NewAudioFrame(frameType string, timestamp uint32, data [][]byte) AudioFrame {
	return AudioFrame{
		Frame: Frame{
			Type:      TypeAudio,
			FrameType: frameType,
			Timestamp: timestamp,
			Data:      data,
		},
		IsSequenceHeader: IsAudioSequenceHeader(frameType),
	}
}