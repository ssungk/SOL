package srt

import (
	"fmt"
	"net/url"
	"strings"
)

// SRTStreamIDExtension SRT StreamID 확장 구조
type SRTStreamIDExtension struct {
	Type   uint16 // 확장 타입 (StreamID = 1)
	Length uint16 // 확장 길이
	Data   []byte // StreamID 데이터
}

// StreamIDInfo 파싱된 StreamID 정보
type StreamIDInfo struct {
	Mode     string            // "publish" 또는 "play"
	Resource string            // 스트림 리소스 이름
	Params   map[string]string // 추가 파라미터들
}

// ParseStreamID StreamID 문자열 파싱
func ParseStreamID(streamID string) (*StreamIDInfo, error) {
	if streamID == "" {
		return nil, fmt.Errorf("empty streamID")
	}
	
	info := &StreamIDInfo{
		Params: make(map[string]string),
	}
	
	// URL 형태 파싱 시도: srt://host/mode/resource?params
	if strings.HasPrefix(streamID, "srt://") {
		return parseURLStreamID(streamID, info)
	}
	
	// 간단한 형태 파싱: mode:resource 또는 mode/resource
	return parseSimpleStreamID(streamID, info)
}

// parseURLStreamID URL 형태 StreamID 파싱
func parseURLStreamID(streamID string, info *StreamIDInfo) (*StreamIDInfo, error) {
	u, err := url.Parse(streamID)
	if err != nil {
		return nil, fmt.Errorf("invalid URL format: %w", err)
	}
	
	// 경로에서 모드와 리소스 추출: /mode/resource
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(pathParts) < 1 {
		return nil, fmt.Errorf("missing mode in URL path")
	}
	
	info.Mode = pathParts[0]
	if len(pathParts) > 1 {
		info.Resource = strings.Join(pathParts[1:], "/")
	}
	
	// 쿼리 파라미터 추출
	for key, values := range u.Query() {
		if len(values) > 0 {
			info.Params[key] = values[0]
		}
	}
	
	return info, nil
}

// parseSimpleStreamID 간단한 형태 StreamID 파싱
func parseSimpleStreamID(streamID string, info *StreamIDInfo) (*StreamIDInfo, error) {
	// 콜론으로 분리: mode:resource
	if strings.Contains(streamID, ":") {
		parts := strings.SplitN(streamID, ":", 2)
		info.Mode = parts[0]
		if len(parts) > 1 {
			info.Resource = parts[1]
		}
		return info, nil
	}
	
	// 슬래시로 분리: mode/resource
	if strings.Contains(streamID, "/") {
		parts := strings.SplitN(streamID, "/", 2)
		info.Mode = parts[0]
		if len(parts) > 1 {
			info.Resource = parts[1]
		}
		return info, nil
	}
	
	// 파라미터가 있는 경우: mode,key1=value1,key2=value2
	if strings.Contains(streamID, ",") {
		parts := strings.Split(streamID, ",")
		info.Mode = parts[0]
		
		for i := 1; i < len(parts); i++ {
			if kv := strings.SplitN(parts[i], "=", 2); len(kv) == 2 {
				info.Params[kv[0]] = kv[1]
			}
		}
		return info, nil
	}
	
	// 단순 모드만: publish 또는 play
	info.Mode = streamID
	return info, nil
}

// IsPublishMode 발행 모드인지 확인
func (info *StreamIDInfo) IsPublishMode() bool {
	mode := strings.ToLower(info.Mode)
	return mode == "publish" || mode == "pub" || mode == "push" || mode == "send"
}

// IsPlayMode 재생 모드인지 확인
func (info *StreamIDInfo) IsPlayMode() bool {
	mode := strings.ToLower(info.Mode)
	return mode == "play" || mode == "sub" || mode == "pull" || mode == "receive" || mode == "request"
}

// GetStreamKey 스트림 키 반환 (리소스 이름 기반)
func (info *StreamIDInfo) GetStreamKey() string {
	if info.Resource != "" {
		return info.Resource
	}
	
	// 파라미터에서 스트림 키 찾기
	if streamKey, exists := info.Params["streamid"]; exists {
		return streamKey
	}
	if streamKey, exists := info.Params["stream"]; exists {
		return streamKey
	}
	if streamKey, exists := info.Params["key"]; exists {
		return streamKey
	}
	
	// 기본값: 모드 기반
	return fmt.Sprintf("stream_%s", info.Mode)
}

// GetParameter 파라미터 값 반환
func (info *StreamIDInfo) GetParameter(key string) (string, bool) {
	value, exists := info.Params[key]
	return value, exists
}

// SetParameter 파라미터 설정
func (info *StreamIDInfo) SetParameter(key, value string) {
	if info.Params == nil {
		info.Params = make(map[string]string)
	}
	info.Params[key] = value
}

// ToString StreamID 문자열로 변환
func (info *StreamIDInfo) ToString() string {
	if info.Resource != "" {
		base := fmt.Sprintf("%s:%s", info.Mode, info.Resource)
		
		// 파라미터 추가
		var params []string
		for key, value := range info.Params {
			params = append(params, fmt.Sprintf("%s=%s", key, value))
		}
		
		if len(params) > 0 {
			return fmt.Sprintf("%s,%s", base, strings.Join(params, ","))
		}
		return base
	}
	
	return info.Mode
}

// ValidateStreamID StreamID 검증
func ValidateStreamID(streamID string) error {
	if streamID == "" {
		return fmt.Errorf("empty streamID")
	}
	
	info, err := ParseStreamID(streamID)
	if err != nil {
		return fmt.Errorf("failed to parse streamID: %w", err)
	}
	
	// 모드 검증
	if !info.IsPublishMode() && !info.IsPlayMode() {
		return fmt.Errorf("invalid mode: %s (must be publish or play)", info.Mode)
	}
	
	// 리소스 이름 검증 (선택적)
	streamKey := info.GetStreamKey()
	if len(streamKey) > 255 {
		return fmt.Errorf("stream key too long: %d (max 255)", len(streamKey))
	}
	
	// 금지된 문자 확인
	forbidden := []string{"<", ">", "\"", "'", "&", "\n", "\r", "\t"}
	for _, char := range forbidden {
		if strings.Contains(streamKey, char) {
			return fmt.Errorf("stream key contains forbidden character: %s", char)
		}
	}
	
	return nil
}

// ExtractStreamIDFromHandshake 핸드셰이크에서 StreamID 추출
func ExtractStreamIDFromHandshake(handshake *SRTHandshakePacket, payload []byte) (string, error) {
	// SRT 핸드셰이크 확장 부분에서 StreamID 찾기
	// 실제 SRT 구현에서는 핸드셰이크 확장에 StreamID가 포함됨
	
	// 현재 간단한 구현에서는 페이로드에서 문자열 추출 시도
	if len(payload) > 72 { // 기본 핸드셰이크 크기 이후
		extensionData := payload[72:]
		return extractStreamIDFromExtension(extensionData)
	}
	
	return "", fmt.Errorf("no StreamID extension found")
}

// extractStreamIDFromExtension 확장 데이터에서 StreamID 추출
func extractStreamIDFromExtension(data []byte) (string, error) {
	if len(data) < 4 {
		return "", fmt.Errorf("extension data too small")
	}
	
	// SRT 확장 헤더 파싱: Type(2) + Length(2) + Data(n)
	offset := 0
	for offset+4 <= len(data) {
		extType := uint16(data[offset])<<8 | uint16(data[offset+1])
		extLength := uint16(data[offset+2])<<8 | uint16(data[offset+3])
		offset += 4
		
		if extType == 1 && extLength > 0 { // StreamID 확장
			if offset+int(extLength) <= len(data) {
				streamID := string(data[offset : offset+int(extLength)])
				return strings.TrimRight(streamID, "\x00"), nil // null 종료 문자 제거
			}
		}
		
		offset += int(extLength)
	}
	
	return "", fmt.Errorf("StreamID extension not found")
}

// CreateStreamIDExtension StreamID 확장 데이터 생성
func CreateStreamIDExtension(streamID string) []byte {
	streamIDBytes := []byte(streamID)
	
	// 확장 헤더: Type(2) + Length(2) + Data(n)
	extension := make([]byte, 4+len(streamIDBytes))
	
	// Type = 1 (StreamID)
	extension[0] = 0x00
	extension[1] = 0x01
	
	// Length
	extension[2] = byte(len(streamIDBytes) >> 8)
	extension[3] = byte(len(streamIDBytes))
	
	// Data
	copy(extension[4:], streamIDBytes)
	
	return extension
}