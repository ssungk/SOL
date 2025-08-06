package rtmp2

import (
	"context"
	"fmt"
	"log/slog"
	"sol/pkg/media"
)

// RTMPSource RTMP 발행자를 위한 MediaSource 구현체
type RTMPSource struct {
	*media.BaseMediaSource
	
	// RTMP 세션 정보
	sessionInfo RTMPSessionInfo
	stream      *media.Stream
	
	// 상태 관리
	isActive bool
}

// NewRTMPSource 새로운 RTMP 소스 생성
func NewRTMPSource(sessionInfo RTMPSessionInfo, address string) *RTMPSource {
	baseSource := media.NewBaseMediaSource(media.MediaTypeRTMP, address)
	
	return &RTMPSource{
		BaseMediaSource: baseSource,
		sessionInfo:     sessionInfo,
		isActive:        false,
	}
}

// GetSourceId 소스 ID 반환 (MediaSource 인터페이스)
func (s *RTMPSource) GetSourceId() string {
	return s.sessionInfo.SessionID
}

// GetSourceInfo 소스 정보 반환 (MediaSource 인터페이스)
func (s *RTMPSource) GetSourceInfo() string {
	return fmt.Sprintf("RTMP Publisher: %s/%s", s.sessionInfo.AppName, s.sessionInfo.StreamName)
}

// IsActive 활성 상태 반환 (MediaSource 인터페이스)
func (s *RTMPSource) IsActive() bool {
	return s.isActive
}

// SetStream 스트림 설정
func (s *RTMPSource) SetStream(stream *media.Stream) {
	s.stream = stream
	s.isActive = true
	
	slog.Info("RTMP source activated", 
		"sourceId", s.GetSourceId(),
		"streamId", stream.GetId(),
		"sourceInfo", s.GetSourceInfo())
}

// RemoveStream 스트림 제거
func (s *RTMPSource) RemoveStream() {
	s.stream = nil
	s.isActive = false
	
	slog.Info("RTMP source deactivated", 
		"sourceId", s.GetSourceId(),
		"sourceInfo", s.GetSourceInfo())
}

// SendVideoFrame 비디오 프레임 전송
func (s *RTMPSource) SendVideoFrame(frameType RTMPFrameType, timestamp uint32, data [][]byte) error {
	if !s.isActive || s.stream == nil {
		return fmt.Errorf("source not active or stream not set")
	}
	
	// RTMP 프레임을 media.Frame으로 변환
	frame := convertRTMPFrameToMediaFrame(frameType, timestamp, data, true)
	
	// 스트림에 전송
	s.stream.SendFrame(frame)
	
	slog.Debug("Video frame sent from RTMP source", 
		"sourceId", s.GetSourceId(),
		"frameType", string(frameType),
		"timestamp", timestamp,
		"dataSize", s.calculateDataSize(data))
	
	return nil
}

// SendAudioFrame 오디오 프레임 전송
func (s *RTMPSource) SendAudioFrame(frameType RTMPFrameType, timestamp uint32, data [][]byte) error {
	if !s.isActive || s.stream == nil {
		return fmt.Errorf("source not active or stream not set")
	}
	
	// RTMP 프레임을 media.Frame으로 변환
	frame := convertRTMPFrameToMediaFrame(frameType, timestamp, data, false)
	
	// 스트림에 전송
	s.stream.SendFrame(frame)
	
	slog.Debug("Audio frame sent from RTMP source", 
		"sourceId", s.GetSourceId(),
		"frameType", string(frameType),
		"timestamp", timestamp,
		"dataSize", s.calculateDataSize(data))
	
	return nil
}

// SendMetadata 메타데이터 전송
func (s *RTMPSource) SendMetadata(metadata map[string]any) error {
	if !s.isActive || s.stream == nil {
		return fmt.Errorf("source not active or stream not set")
	}
	
	// any 값을 string으로 변환
	stringMetadata := make(map[string]string)
	for k, v := range metadata {
		stringMetadata[k] = fmt.Sprintf("%v", v)
	}
	
	// 스트림에 메타데이터 전송
	s.stream.SendMetadata(stringMetadata)
	
	slog.Info("Metadata sent from RTMP source", 
		"sourceId", s.GetSourceId(),
		"metadataKeys", len(metadata))
	
	return nil
}

// Start 소스 시작 (BaseMediaSource 오버라이드)
func (s *RTMPSource) Start() error {
	if err := s.BaseMediaSource.Start(); err != nil {
		return err
	}
	
	slog.Info("RTMP source started", 
		"sourceId", s.GetSourceId(),
		"sourceInfo", s.GetSourceInfo())
	
	return nil
}

// Stop 소스 중지 (BaseMediaSource 오버라이드)  
func (s *RTMPSource) Stop() error {
	if err := s.BaseMediaSource.Stop(); err != nil {
		return err
	}
	
	s.RemoveStream()
	
	slog.Info("RTMP source stopped", 
		"sourceId", s.GetSourceId(),
		"sourceInfo", s.GetSourceInfo())
	
	return nil
}

// GetSessionInfo 세션 정보 반환
func (s *RTMPSource) GetSessionInfo() RTMPSessionInfo {
	return s.sessionInfo
}

// GetStream 연결된 스트림 반환
func (s *RTMPSource) GetStream() *media.Stream {
	return s.stream
}

// calculateDataSize 데이터 크기 계산 헬퍼 함수
func (s *RTMPSource) calculateDataSize(data [][]byte) int {
	totalSize := 0
	for _, chunk := range data {
		totalSize += len(chunk)
	}
	return totalSize
}

// GetContext 컨텍스트 반환 (내부 사용)
func (s *RTMPSource) GetContext() context.Context {
	return s.Context()
}