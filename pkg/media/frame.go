package media

// Type represents the type of media
type Type uint8

const (
	TypeAudio Type = iota + 1
	TypeVideo
	TypeMetadata
)

// FrameSubType 미디어 프레임의 세부 타입
type FrameSubType uint8

const (
	// Video subtypes
	VideoKeyFrame FrameSubType = iota + 1
	VideoInterFrame
	VideoDisposableInterFrame // B-frame
	VideoGeneratedKeyFrame
	VideoInfoFrame
	VideoSequenceHeader // SPS/PPS/VPS 등
	VideoEndOfSequence

	// Audio subtypes
	AudioRawData
	AudioSequenceHeader // Audio Specific Config 등
)

// Frame represents a generic media frame
type Frame struct {
	Type      Type         // Media type (audio/video/metadata)
	SubType   FrameSubType // Frame sub type (VideoKeyFrame, AudioRawData 등)
	Timestamp uint32       // Frame timestamp in milliseconds
	Data      [][]byte     // Zero-copy payload chunks
}

// VideoFrame represents a video frame with video-specific information
type VideoFrame struct {
	Frame
	IsKeyFrame    bool // Whether this is a key frame
	IsConfigFrame bool // Whether this is a config frame (e.g., SPS/PPS/VPS)
}

// AudioFrame represents an audio frame with audio-specific information
type AudioFrame struct {
	Frame
	IsConfigFrame bool // Whether this is a config frame (e.g., Audio Specific Config)
}

// NewVideoFrame creates a new video frame
func NewVideoFrame(subType FrameSubType, timestamp uint32, data [][]byte) VideoFrame {
	return VideoFrame{
		Frame: Frame{
			Type:      TypeVideo,
			SubType:   subType,
			Timestamp: timestamp,
			Data:      data,
		},
		IsKeyFrame:    IsVideoKeyFrame(subType),
		IsConfigFrame: IsVideoConfigFrame(subType),
	}
}

// NewAudioFrame creates a new audio frame
func NewAudioFrame(subType FrameSubType, timestamp uint32, data [][]byte) AudioFrame {
	return AudioFrame{
		Frame: Frame{
			Type:      TypeAudio,
			SubType:   subType,
			Timestamp: timestamp,
			Data:      data,
		},
		IsConfigFrame: IsAudioConfigFrame(subType),
	}
}

// IsVideoKeyFrame 비디오 키프레임 여부 확인
func IsVideoKeyFrame(subType FrameSubType) bool {
	return subType == VideoKeyFrame || subType == VideoGeneratedKeyFrame
}

// IsVideoConfigFrame 비디오 설정 프레임 여부 확인
func IsVideoConfigFrame(subType FrameSubType) bool {
	return subType == VideoSequenceHeader
}

// IsAudioConfigFrame 오디오 설정 프레임 여부 확인
func IsAudioConfigFrame(subType FrameSubType) bool {
	return subType == AudioSequenceHeader
}