package media

import (
	"strings"
)

// IsVideoSequenceHeader determines if frame type represents video sequence header
func IsVideoSequenceHeader(frameType string) bool {
	frameTypeLower := strings.ToLower(frameType)
	return strings.Contains(frameTypeLower, "sequence header") ||
		   strings.Contains(frameTypeLower, "sps") ||
		   strings.Contains(frameTypeLower, "pps") ||
		   strings.Contains(frameTypeLower, "vps")
}

// IsAudioSequenceHeader determines if frame type represents audio sequence header
func IsAudioSequenceHeader(frameType string) bool {
	frameTypeLower := strings.ToLower(frameType)
	return strings.Contains(frameTypeLower, "sequence header") ||
		   strings.Contains(frameTypeLower, "audio specific config")
}

// IsKeyFrame determines if frame type represents a key frame
func IsKeyFrame(frameType string) bool {
	frameTypeLower := strings.ToLower(frameType)
	return strings.Contains(frameTypeLower, "key frame") ||
		   strings.Contains(frameTypeLower, "i-frame") ||
		   strings.Contains(frameTypeLower, "idr")
}