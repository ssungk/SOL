package rtmp2

import (
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
	
	// MediaServer와의 통신을 위한 채널
	eventChannel chan<- interface{}
	
	// 상태 관리
	isActive bool
}

// NewRTMPSource 새로운 RTMP 소스 생성
func NewRTMPSource(sessionInfo RTMPSessionInfo, address string, eventChannel chan<- interface{}) *RTMPSource {
	baseSource := media.NewBaseMediaSource(media.MediaNodeTypeRTMP, address)
	
	source := &RTMPSource{
		BaseMediaSource: baseSource,
		sessionInfo:     sessionInfo,
		eventChannel:    eventChannel,
		isActive:        false,
	}
	
	// NodeConnected 이벤트 전송
	source.sendEvent(media.NodeConnected{
		BaseNodeEvent: media.BaseNodeEvent{
			SessionId:  sessionInfo.SessionID,
			StreamName: sessionInfo.AppName + "/" + sessionInfo.StreamName,
			NodeType:   media.MediaNodeTypeRTMP,
		},
		NodeId:  source.ID(),
		Address: address,
	})
	
	return source
}

// GetSourceId 소스 ID 반환 (내부 사용용)
func (s *RTMPSource) GetSourceId() string {
	return s.sessionInfo.SessionID
}

// GetSourceInfo 소스 정보 반환 (내부 사용용)
func (s *RTMPSource) GetSourceInfo() string {
	return fmt.Sprintf("RTMP Publisher: %s/%s", s.sessionInfo.AppName, s.sessionInfo.StreamName)
}

// IsActive 활성 상태 반환 (내부 사용용)
func (s *RTMPSource) IsActive() bool {
	return s.isActive
}

// SetStream 스트림 설정
func (s *RTMPSource) SetStream(stream *media.Stream) {
	s.stream = stream
	s.isActive = true
	
	// PublishStarted 이벤트 전송
	s.sendEvent(media.PublishStarted{
		BaseNodeEvent: media.BaseNodeEvent{
			SessionId:  s.sessionInfo.SessionID,
			StreamName: s.sessionInfo.AppName + "/" + s.sessionInfo.StreamName,
			NodeType:   media.MediaNodeTypeRTMP,
		},
		NodeId: s.ID(),
	})
	
	slog.Info("RTMP source activated", "sourceId", s.GetSourceId(), "streamId", stream.GetId(), "sourceInfo", s.GetSourceInfo())
}

// RemoveStream 스트림 제거
func (s *RTMPSource) RemoveStream() {
	// PublishStopped 이벤트 전송
	s.sendEvent(media.PublishStopped{
		BaseNodeEvent: media.BaseNodeEvent{
			SessionId:  s.sessionInfo.SessionID,
			StreamName: s.sessionInfo.AppName + "/" + s.sessionInfo.StreamName,
			NodeType:   media.MediaNodeTypeRTMP,
		},
		NodeId: s.ID(),
	})
	
	s.stream = nil
	s.isActive = false
	
	slog.Info("RTMP source deactivated", "sourceId", s.GetSourceId(), "sourceInfo", s.GetSourceInfo())
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
	
	slog.Debug("Video frame sent from RTMP source", "sourceId", s.GetSourceId(), "frameType", string(frameType), "timestamp", timestamp, "dataSize", s.calculateDataSize(data))
	
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
	
	slog.Debug("Audio frame sent from RTMP source", "sourceId", s.GetSourceId(), "frameType", string(frameType), "timestamp", timestamp, "dataSize", s.calculateDataSize(data))
	
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
	
	slog.Info("Metadata sent from RTMP source", "sourceId", s.GetSourceId(), "metadataKeys", len(metadata))
	
	return nil
}

// Start 소스 시작 (BaseMediaSource 오버라이드)
func (s *RTMPSource) Start() error {
	if err := s.BaseMediaSource.Start(); err != nil {
		return err
	}
	
	slog.Info("RTMP source started", "sourceId", s.GetSourceId(), "sourceInfo", s.GetSourceInfo())
	
	return nil
}

// Stop 소스 중지 (BaseMediaSource 오버라이드)  
func (s *RTMPSource) Stop() error {
	if err := s.BaseMediaSource.Stop(); err != nil {
		return err
	}
	
	s.RemoveStream()
	
	// NodeDisconnected 이벤트 전송
	s.sendEvent(media.NodeDisconnected{
		BaseNodeEvent: media.BaseNodeEvent{
			SessionId:  s.sessionInfo.SessionID,
			StreamName: s.sessionInfo.AppName + "/" + s.sessionInfo.StreamName,
			NodeType:   media.MediaNodeTypeRTMP,
		},
		NodeId: s.ID(),
	})
	
	slog.Info("RTMP source stopped", "sourceId", s.GetSourceId(), "sourceInfo", s.GetSourceInfo())
	
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

// sendEvent 이벤트를 MediaServer로 직접 전송
func (s *RTMPSource) sendEvent(event interface{}) {
	if s.eventChannel == nil {
		return
	}
	
	select {
	case s.eventChannel <- event:
		// 이벤트 전송 성공
	default:
		// 채널이 꽉 찬 경우 이벤트 드롭
		slog.Warn("event channel full, dropping event", "sourceId", s.GetSourceId(), "eventType", fmt.Sprintf("%T", event))
	}
}

