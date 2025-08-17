package media

import (
	"strings"
)

// StringToFrameType 문자열을 FrameType으로 변환
func StringToFrameType(frameType string, isVideo bool) FrameType {
	frameTypeLower := strings.ToLower(frameType)
	
	if isVideo {
		if strings.Contains(frameTypeLower, "sequence header") ||
		   strings.Contains(frameTypeLower, "sps") ||
		   strings.Contains(frameTypeLower, "pps") ||
		   strings.Contains(frameTypeLower, "vps") {
			return TypeConfig
		}
		if strings.Contains(frameTypeLower, "key frame") ||
		   strings.Contains(frameTypeLower, "i-frame") ||
		   strings.Contains(frameTypeLower, "idr") {
			return TypeKey
		}
		// 나머지는 모두 데이터 프레임 (P-frame, B-frame 등)
		return TypeData
	} else {
		if strings.Contains(frameTypeLower, "sequence header") ||
		   strings.Contains(frameTypeLower, "audio specific config") {
			return TypeConfig
		}
		// 기본값
		return TypeData
	}
}

// IsVideoConfigFrame determines if frame type represents video config frame
func IsVideoConfigFrame(frameType string) bool {
	frameTypeLower := strings.ToLower(frameType)
	return strings.Contains(frameTypeLower, "sequence header") ||
		   strings.Contains(frameTypeLower, "sps") ||
		   strings.Contains(frameTypeLower, "pps") ||
		   strings.Contains(frameTypeLower, "vps")
}

// IsAudioConfigFrame determines if frame type represents audio config frame
func IsAudioConfigFrame(frameType string) bool {
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