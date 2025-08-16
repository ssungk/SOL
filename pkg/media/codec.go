package media

import (
	"strings"
)

// StringToFrameSubType 문자열을 FrameSubType으로 변환
func StringToFrameSubType(frameType string, isVideo bool) FrameSubType {
	frameTypeLower := strings.ToLower(frameType)
	
	if isVideo {
		if strings.Contains(frameTypeLower, "sequence header") ||
		   strings.Contains(frameTypeLower, "sps") ||
		   strings.Contains(frameTypeLower, "pps") ||
		   strings.Contains(frameTypeLower, "vps") {
			return VideoSequenceHeader
		}
		if strings.Contains(frameTypeLower, "disposable inter frame") ||
		   strings.Contains(frameTypeLower, "b-frame") ||
		   strings.Contains(frameTypeLower, "bi-directional") {
			return VideoDisposableInterFrame
		}
		if strings.Contains(frameTypeLower, "key frame") ||
		   strings.Contains(frameTypeLower, "i-frame") ||
		   strings.Contains(frameTypeLower, "idr") {
			return VideoKeyFrame
		}
		if strings.Contains(frameTypeLower, "inter frame") {
			return VideoInterFrame
		}
		// 기본값
		return VideoInterFrame
	} else {
		if strings.Contains(frameTypeLower, "sequence header") ||
		   strings.Contains(frameTypeLower, "audio specific config") {
			return AudioSequenceHeader
		}
		// 기본값
		return AudioRawData
	}
}

// IsVideoSequenceHeader determines if frame type represents video sequence header (기존 호환성 유지)
func IsVideoSequenceHeader(frameType string) bool {
	frameTypeLower := strings.ToLower(frameType)
	return strings.Contains(frameTypeLower, "sequence header") ||
		   strings.Contains(frameTypeLower, "sps") ||
		   strings.Contains(frameTypeLower, "pps") ||
		   strings.Contains(frameTypeLower, "vps")
}

// IsAudioSequenceHeader determines if frame type represents audio sequence header (기존 호환성 유지)
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