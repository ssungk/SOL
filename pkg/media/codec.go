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

// IsBFrame determines if frame type represents a B-frame (bidirectional frame)
func IsBFrame(frameType string) bool {
	frameTypeLower := strings.ToLower(frameType)
	return strings.Contains(frameTypeLower, "disposable inter frame") ||
		   strings.Contains(frameTypeLower, "b-frame") ||
		   strings.Contains(frameTypeLower, "bi-directional")
}

// IsPFrame determines if frame type represents a P-frame (predictive frame)  
func IsPFrame(frameType string) bool {
	frameTypeLower := strings.ToLower(frameType)
	return strings.Contains(frameTypeLower, "inter frame") && !IsBFrame(frameType)
}