package media

import (
	"errors"
	"fmt"
	"log/slog"
)

// 에러 정의
var (
	ErrInvalidTrackIndex = errors.New("invalid track index")
)

// Stream represents a protocol-independent stream with multiple tracks
type Stream struct {
	id           string
	tracks       []*Track        // 트랙 배열 (인덱스 기반)
	trackBuffers []*TrackBuffer // 트랙별 개별 버퍼

	// Stream routing
	sinks map[uintptr]MediaSink // Multiple sinks indexed by node ID
}

// NewStream creates a new stream
func NewStream(id string) *Stream {
	s := &Stream{
		id:           id,
		tracks:       make([]*Track, 0),
		trackBuffers: make([]*TrackBuffer, 0),
		sinks:        make(map[uintptr]MediaSink),
	}

	slog.Info("Stream created", "streamId", id)

	return s
}

// ID returns the stream id
func (s *Stream) ID() string {
	return s.id
}

// AddTrack 새로운 트랙을 추가하고 인덱스를 반환
func (s *Stream) AddTrack(codec Codec) int {
	index := len(s.tracks)
	track := NewTrack(index, codec)
	
	s.tracks = append(s.tracks, track)
	s.trackBuffers = append(s.trackBuffers, NewTrackBuffer())
	
	slog.Info("Track added to stream", "streamId", s.id, "trackIndex", index, "codec", codec)
	
	return index
}

// SendFrame 미디어 프레임 전송 (프레임에 포함된 trackIndex 사용)
func (s *Stream) SendFrame(frame Frame) error {
	trackIndex := frame.TrackIndex
	if trackIndex < 0 || trackIndex >= len(s.tracks) {
		return ErrInvalidTrackIndex
	}
	
	// 트랙별 버퍼에 캐시
	s.trackBuffers[trackIndex].AddFrame(frame)
	
	// 모든 sink에 브로드캐스트
	s.broadcastTrackFrame(frame)
	
	return nil
}

// SendMetadata 스트림 메타데이터 전송 (모든 트랙 공통)
func (s *Stream) SendMetadata(metadata map[string]string) {
	// 첫 번째 트랙 버퍼에 메타데이터 저장 (임시)
	if len(s.trackBuffers) > 0 {
		s.trackBuffers[0].AddMetadata(metadata)
	}
	
	// 모든 sink에 브로드캐스트
	s.broadcastMetadata(metadata)
}

// GetTrackCount 트랙 개수 반환
func (s *Stream) GetTrackCount() int {
	return len(s.tracks)
}

// GetTrack 지정된 인덱스의 트랙 반환
func (s *Stream) GetTrack(index int) *Track {
	if index < 0 || index >= len(s.tracks) {
		return nil
	}
	return s.tracks[index]
}

// broadcastTrackFrame 트랙 프레임을 모든 sink에 전송
func (s *Stream) broadcastTrackFrame(frame Frame) {
	// Send to all sinks with stream ID
	for _, sink := range s.sinks {
		// 각 sink의 선호 포맷에 맞게 변환
		convertedFrame := s.convertFrameForSink(frame, sink)

		if err := sink.SendFrame(s.id, convertedFrame); err != nil {
			slog.Error("Failed to send track frame to sink", "streamId", s.id, "trackIndex", frame.TrackIndex, "nodeId", sink.ID(), "codec", frame.Codec, "err", err)
		}
	}
}

// broadcastManagedFrame은 제거됨 - broadcastFrame만 사용

// broadcastMetadata sends metadata to all sinks
func (s *Stream) broadcastMetadata(metadata map[string]string) {
	// Send to all sinks with stream ID
	for _, sink := range s.sinks {
		if err := sink.SendMetadata(s.id, metadata); err != nil {
			slog.Error("Failed to send metadata to sink", "streamId", s.id, "nodeId", sink.ID(), "err", err)
		}
	}
}

// Stop stops the stream and cleans up resources
func (s *Stream) Stop() {
	slog.Info("Stopping stream", "streamId", s.id)

	// Clear all track buffers
	for _, buffer := range s.trackBuffers {
		buffer.Clear()
	}

	slog.Info("Stream stopped", "streamId", s.id)
}

// AddSink adds a stream sink and automatically sends cached data
func (s *Stream) AddSink(sink MediaSink) {
	nodeId := sink.ID()
	s.sinks[nodeId] = sink

	slog.Info("Sink added to stream", "streamId", s.id, "nodeId", nodeId, "sinkCount", len(s.sinks))

	// 자동으로 캐시된 데이터 전송
	if err := s.sendCachedDataToSink(sink); err != nil {
		slog.Error("Failed to send cached data to newly added sink", "streamId", s.id, "nodeId", nodeId, "err", err)
		// sink는 이미 추가되었으므로 에러가 있어도 제거하지 않음
	}
}

// RemoveSink removes a stream sink
func (s *Stream) RemoveSink(sink MediaSink) {
	nodeId := sink.ID()
	delete(s.sinks, nodeId)

	slog.Info("Sink removed from stream", "streamId", s.id, "nodeId", nodeId, "sinkCount", len(s.sinks))
}

// sendCachedDataToSink sends all cached data to a new sink (private)
func (s *Stream) sendCachedDataToSink(sink MediaSink) error {
	nodeId := sink.ID()

	// Send cached metadata first (from first track buffer if exists)
	if len(s.trackBuffers) > 0 {
		if metadata := s.trackBuffers[0].GetMetadata(); metadata != nil {
			if err := sink.SendMetadata(s.id, metadata); err != nil {
				slog.Error("Failed to send cached metadata to sink", "streamId", s.id, "nodeId", nodeId, "err", err)
			} else {
				slog.Debug("Sent cached metadata to sink", "streamId", s.id, "nodeId", nodeId)
			}
		}
	}

	// Send cached frames from all tracks
	for trackIndex, buffer := range s.trackBuffers {
		cachedFrames := buffer.GetCachedFrames()
		if len(cachedFrames) > 0 {
			slog.Debug("Sending cached frames to new sink", "streamId", s.id, "trackIndex", trackIndex, "nodeId", nodeId, "frameCount", len(cachedFrames))

			for _, frame := range cachedFrames {
				// 각 sink의 선호 포맷에 맞게 변환
				convertedFrame := s.convertFrameForSink(frame, sink)

				if err := sink.SendFrame(s.id, convertedFrame); err != nil {
					slog.Error("Failed to send cached track frame to sink", "streamId", s.id, "trackIndex", frame.TrackIndex, "nodeId", nodeId, "codec", frame.Codec, "err", err)
					// Continue with next frame even if one fails
				}
			}

			slog.Debug("Finished sending cached frames to sink", "streamId", s.id, "trackIndex", trackIndex, "nodeId", nodeId)
		}
	}

	return nil
}

// GetSinkCount returns the number of active sinks
func (s *Stream) GetSinkCount() int {
	return len(s.sinks)
}

// GetSinks returns a copy of all active sinks
func (s *Stream) GetSinks() []MediaSink {
	sinks := make([]MediaSink, 0, len(s.sinks))
	for _, sink := range s.sinks {
		sinks = append(sinks, sink)
	}

	return sinks
}

// HasCachedData returns whether the stream has any cached data
func (s *Stream) HasCachedData() bool {
	for _, buffer := range s.trackBuffers {
		if buffer.HasCachedData() {
			return true
		}
	}
	return false
}

// GetCacheStats returns cache statistics
func (s *Stream) GetCacheStats() map[string]any {
	stats := map[string]any{
		"track_count": len(s.tracks),
		"sink_count":  len(s.sinks),
	}

	// 트랙별 통계 추가
	for i, buffer := range s.trackBuffers {
		trackStats := buffer.GetCacheStats()
		stats[fmt.Sprintf("track_%d", i)] = trackStats
	}

	return stats
}

// convertFrameForSink 각 sink의 선호 포맷에 맞게 프레임 변환 (현재는 변환 없음)
func (s *Stream) convertFrameForSink(frame Frame, sink MediaSink) Frame {
	// 아직 포맷 변환이 필요한 프로토콜이 없으므로 원본 그대로 반환
	return frame
}
