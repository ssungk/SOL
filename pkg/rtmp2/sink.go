package rtmp2

import (
	"fmt"
	"log/slog"
	"net"
	"sol/pkg/amf"
	"sol/pkg/media"
	"unsafe"
)

// RTMPSink RTMP 플레이어를 위한 MediaSink 구현체
type RTMPSink struct {
	*media.BaseMediaSink
	
	// RTMP 세션 정보
	sessionInfo RTMPSessionInfo
	conn        net.Conn
	
	// 메시지 전송을 위한 writer
	writer *messageWriter
	
	// MediaServer와의 통신을 위한 채널
	eventChannel chan<- interface{}
	
	// 상태 관리
	isActive bool
}

// NewRTMPSink 새로운 RTMP 싱크 생성
func NewRTMPSink(sessionInfo RTMPSessionInfo, conn net.Conn, address string, eventChannel chan<- interface{}) *RTMPSink {
	baseSink := media.NewBaseMediaSink(media.MediaNodeTypeRTMP, address)
	
	sink := &RTMPSink{
		BaseMediaSink: baseSink,
		sessionInfo:   sessionInfo,
		conn:          conn,
		eventChannel:  eventChannel,
		isActive:      true, // 연결 생성시 바로 활성화
	}
	
	// NodeConnected 이벤트 전송
	sink.sendEvent(media.NodeConnected{
		BaseNodeEvent: media.BaseNodeEvent{
			SessionId:  sessionInfo.SessionID,
			StreamName: sessionInfo.AppName + "/" + sessionInfo.StreamName,
			NodeType:   media.MediaNodeTypeRTMP,
		},
		NodeId:  sink.ID(),
		Address: address,
	})
	
	return sink
}

// GetSinkId 싱크 ID 반환 (내부 사용용, MediaSink 인터페이스에서 제거됨)
func (s *RTMPSink) GetSinkId() string {
	return s.sessionInfo.SessionID
}

// GetSinkInfo 싱크 정보 반환 (내부 사용용, MediaSink 인터페이스에서 제거됨)
func (s *RTMPSink) GetSinkInfo() string {
	return fmt.Sprintf("RTMP Player: %s/%s", s.sessionInfo.AppName, s.sessionInfo.StreamName)
}

// SendMediaFrame 미디어 프레임 전송
func (s *RTMPSink) SendMediaFrame(streamId string, frame media.Frame) error {
	if !s.isActive || s.conn == nil {
		return fmt.Errorf("sink not active")
	}
	
	if s.writer == nil {
		return fmt.Errorf("message writer not set")
	}
	
	// pkg/media Frame을 RTMP 메시지로 변환하여 전송
	var err error
	switch frame.Type {
	case media.TypeVideo:
		err = s.sendVideoFrame(frame)
	case media.TypeAudio:
		err = s.sendAudioFrame(frame)
	default:
		slog.Warn("Unknown frame type", "type", frame.Type, "sinkId", s.GetSinkId())
		return nil
	}
	
	if err != nil {
		slog.Error("Failed to send frame to RTMP sink", "sinkId", s.GetSinkId(), "streamId", streamId, "frameType", frame.FrameType, "err", err)
		return err
	}
	
	slog.Debug("Media frame sent to RTMP sink", "sinkId", s.GetSinkId(), "streamId", streamId, "frameType", frame.FrameType, "timestamp", frame.Timestamp, "dataSize", s.calculateDataSize(frame.Data))
	
	return nil
}

// SendMetadata 메타데이터 전송
func (s *RTMPSink) SendMetadata(streamId string, metadata map[string]string) error {
	if !s.isActive || s.conn == nil {
		return fmt.Errorf("sink not active")
	}
	
	if s.writer == nil {
		return fmt.Errorf("message writer not set")
	}
	
	// pkg/media MetadataFrame을 RTMP onMetaData 메시지로 변환하여 전송
	err := s.sendMetadata(metadata)
	if err != nil {
		slog.Error("Failed to send metadata to RTMP sink", "sinkId", s.GetSinkId(), "streamId", streamId, "err", err)
		return err
	}
	
	slog.Info("Metadata sent to RTMP sink", "sinkId", s.GetSinkId(), "streamId", streamId, "metadataKeys", len(metadata))
	
	return nil
}

// Start 싱크 시작 (BaseMediaSink 오버라이드)
func (s *RTMPSink) Start() error {
	if err := s.BaseMediaSink.Start(); err != nil {
		return err
	}
	
	s.isActive = true
	
	// PlayStarted 이벤트 전송
	s.sendEvent(media.PlayStarted{
		BaseNodeEvent: media.BaseNodeEvent{
			SessionId:  s.sessionInfo.SessionID,
			StreamName: s.sessionInfo.AppName + "/" + s.sessionInfo.StreamName,
			NodeType:   media.MediaNodeTypeRTMP,
		},
		NodeId: s.ID(),
	})
	
	slog.Info("RTMP sink started", "sinkId", s.GetSinkId(), "sinkInfo", s.GetSinkInfo())
	
	return nil
}

// Stop 싱크 중지 (BaseMediaSink 오버라이드)
func (s *RTMPSink) Stop() error {
	if err := s.BaseMediaSink.Stop(); err != nil {
		return err
	}
	
	// PlayStopped 이벤트 전송
	s.sendEvent(media.PlayStopped{
		BaseNodeEvent: media.BaseNodeEvent{
			SessionId:  s.sessionInfo.SessionID,
			StreamName: s.sessionInfo.AppName + "/" + s.sessionInfo.StreamName,
			NodeType:   media.MediaNodeTypeRTMP,
		},
		NodeId: s.ID(),
	})
	
	// NodeDisconnected 이벤트 전송
	s.sendEvent(media.NodeDisconnected{
		BaseNodeEvent: media.BaseNodeEvent{
			SessionId:  s.sessionInfo.SessionID,
			StreamName: s.sessionInfo.AppName + "/" + s.sessionInfo.StreamName,
			NodeType:   media.MediaNodeTypeRTMP,
		},
		NodeId: s.ID(),
	})
	
	s.isActive = false
	
	// 연결 종료
	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			slog.Error("Error closing RTMP sink connection", "sinkId", s.GetSinkId(), "err", err)
		}
		s.conn = nil
	}
	
	slog.Info("RTMP sink stopped", "sinkId", s.GetSinkId(), "sinkInfo", s.GetSinkInfo())
	
	return nil
}

// GetSessionInfo 세션 정보 반환
func (s *RTMPSink) GetSessionInfo() RTMPSessionInfo {
	return s.sessionInfo
}

// GetConnection 연결 반환
func (s *RTMPSink) GetConnection() net.Conn {
	return s.conn
}

// SetWriter 메시지 라이터 설정
func (s *RTMPSink) SetWriter(writer *messageWriter) {
	s.writer = writer
}

// GetWriter 메시지 라이터 반환
func (s *RTMPSink) GetWriter() *messageWriter {
	return s.writer
}

// ID BaseMediaSink의 ID() 메서드를 오버라이드하여 고유 ID 제공
func (s *RTMPSink) ID() uintptr {
	return uintptr(unsafe.Pointer(s))
}

// calculateDataSize 데이터 크기 계산 헬퍼 함수
func (s *RTMPSink) calculateDataSize(data [][]byte) int {
	totalSize := 0
	for _, chunk := range data {
		totalSize += len(chunk)
	}
	return totalSize
}



// sendVideoFrame 비디오 프레임을 RTMP 메시지로 전송
func (s *RTMPSink) sendVideoFrame(frame media.Frame) error {
	// video 메시지 생성 및 전송
	message := &Message{
		messageHeader: &messageHeader{
			Timestamp: frame.Timestamp,
			length:    uint32(s.calculateDataSize(frame.Data)),
			typeId:    MSG_TYPE_VIDEO,
			streamId:  s.sessionInfo.StreamID,
		},
		payload: frame.Data, // Zero-copy: 원본 데이터 재사용
	}
	
	return s.writer.writeVideoMessage(s.conn, message)
}

// sendAudioFrame 오디오 프레임을 RTMP 메시지로 전송
func (s *RTMPSink) sendAudioFrame(frame media.Frame) error {
	// audio 메시지 생성 및 전송
	message := &Message{
		messageHeader: &messageHeader{
			Timestamp: frame.Timestamp,
			length:    uint32(s.calculateDataSize(frame.Data)),
			typeId:    MSG_TYPE_AUDIO,
			streamId:  s.sessionInfo.StreamID,
		},
		payload: frame.Data, // Zero-copy: 원본 데이터 재사용
	}
	
	return s.writer.writeAudioMessage(s.conn, message)
}

// sendMetadata 메타데이터를 RTMP onMetaData 메시지로 전송
func (s *RTMPSink) sendMetadata(metadata map[string]string) error {
	// string을 interface{} map으로 변환 (AMF 인코딩을 위해)
	interfaceMetadata := make(map[string]interface{})
	for k, v := range metadata {
		interfaceMetadata[k] = v
	}
	
	// AMF0 onMetaData 시퀀스로 인코딩
	values := []interface{}{"onMetaData", interfaceMetadata}
	
	// AMF0 시퀀스를 바이트로 인코딩
	encodedData, err := amf.EncodeAMF0Sequence(values...)
	if err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}
	
	// script data 메시지로 전송
	payload := [][]byte{encodedData}
	message := &Message{
		messageHeader: &messageHeader{
			Timestamp: 0, // 메타데이터는 타임스탬프 0
			length:    uint32(len(encodedData)),
			typeId:    MSG_TYPE_AMF0_DATA,
			streamId:  s.sessionInfo.StreamID,
		},
		payload: payload,
	}
	
	return s.writer.writeScriptMessage(s.conn, message)
}

// sendEvent 이벤트를 MediaServer로 직접 전송
func (s *RTMPSink) sendEvent(event interface{}) {
	if s.eventChannel == nil {
		return
	}
	
	select {
	case s.eventChannel <- event:
		// 이벤트 전송 성공
	default:
		// 채널이 꽉 찬 경우 이벤트 드롭
		slog.Warn("event channel full, dropping event", "sinkId", s.GetSinkId(), "eventType", fmt.Sprintf("%T", event))
	}
}