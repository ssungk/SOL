package srt

import (
	"log/slog"
	"sol/pkg/media"
)

// srtMediaSource SRT MediaSource 구현
type srtMediaSource struct {
	session *Session
}

// ID MediaNode 인터페이스 구현
func (s *srtMediaSource) ID() uintptr {
	return uintptr(s.session.socketID)
}

// NodeType MediaNode 인터페이스 구현
func (s *srtMediaSource) NodeType() media.NodeType {
	return media.NodeTypeSRT
}

// Address MediaNode 인터페이스 구현
func (s *srtMediaSource) Address() string {
	return s.session.remoteAddr.String()
}

// Close MediaNode 인터페이스 구현
func (s *srtMediaSource) Close() error {
	s.session.Close()
	return nil
}

// PublishingStreams MediaSource 인터페이스 구현
func (s *srtMediaSource) PublishingStreams() []*media.Stream {
	// 간단한 구현: 빈 슬라이스 반환
	return []*media.Stream{}
}

// srtMediaSink SRT MediaSink 구현
type srtMediaSink struct {
	session *Session
}

// ID MediaNode 인터페이스 구현
func (s *srtMediaSink) ID() uintptr {
	return uintptr(s.session.socketID)
}

// NodeType MediaNode 인터페이스 구현
func (s *srtMediaSink) NodeType() media.NodeType {
	return media.NodeTypeSRT
}

// Address MediaNode 인터페이스 구현
func (s *srtMediaSink) Address() string {
	return s.session.remoteAddr.String()
}

// Close MediaNode 인터페이스 구현
func (s *srtMediaSink) Close() error {
	s.session.Close()
	return nil
}

// SendMediaFrame MediaSink 인터페이스 구현
func (s *srtMediaSink) SendMediaFrame(streamId string, frame media.Frame) error {
	if s.session.state != StateConnected {
		return nil
	}
	
	// Frame을 SRT 패킷으로 변환하여 전송
	data := s.frameToSRTData(frame)
	if len(data) > 0 {
		return s.session.sendDataPacket(data)
	}
	
	return nil
}

// SendMetadata MediaSink 인터페이스 구현
func (s *srtMediaSink) SendMetadata(streamId string, metadata map[string]string) error {
	if s.session.state != StateConnected {
		return nil
	}
	
	// 간단한 구현: 로그만 출력
	slog.Debug("SRT metadata received", "streamID", streamId, "metadata", metadata)
	return nil
}

// SubscribedStreams MediaSink 인터페이스 구현
func (s *srtMediaSink) SubscribedStreams() []string {
	if s.session.streamID != "" {
		return []string{s.session.streamID}
	}
	return []string{}
}

// frameToSRTData Frame을 SRT 데이터로 변환
func (s *srtMediaSink) frameToSRTData(frame media.Frame) []byte {
	// 프레임 타입에 따라 처리
	if frame.Type == media.TypeVideo {
		if len(frame.Data) > 0 && len(frame.Data[0]) > 0 {
			return frame.Data[0]
		}
	} else if frame.Type == media.TypeAudio {
		if len(frame.Data) > 0 && len(frame.Data[0]) > 0 {
			return frame.Data[0]
		}
	}
	return nil
}


