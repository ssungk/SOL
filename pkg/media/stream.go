package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sol/pkg/utils"
)

// Stream represents a protocol-independent stream with multiple tracks
type Stream struct {
	id     string
	tracks []*Track // 트랙 배열 (인덱스 기반, 버퍼 포함)

	// Stream routing
	sinks map[uintptr]MediaSink // Multiple sinks indexed by node ID

	// 이벤트 루프
	channel chan any
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewStream creates a new stream
func NewStream(id string) *Stream {
	ctx, cancel := context.WithCancel(context.Background())

	s := &Stream{
		id:     id,
		tracks: make([]*Track, 0),
		sinks:  make(map[uintptr]MediaSink),

		// 이벤트 루프 초기화 (MediaServer/RTMP Session 패턴과 동일)
		channel: make(chan any, DefaultChannelBufferSize),
		ctx:     ctx,
		cancel:  cancel,
	}

	slog.Info("Stream created", "streamID", id)

	// 이벤트 루프 시작 (백그라운드에서)
	go s.eventLoop()

	return s
}

// ID returns the stream id
func (s *Stream) ID() string {
	return s.id
}

// AddTrack 새로운 트랙을 추가하고 인덱스를 반환 - 직접 처리 (이벤트 루프 우회)
func (s *Stream) AddTrack(codec Codec) int {
	index := len(s.tracks)
	track := NewTrack(index, codec)

	s.tracks = append(s.tracks, track)

	slog.Info("Track added to stream", "streamID", s.id, "trackIndex", index, "codec", codec)

	return index
}

// SendFrame 미디어 프레임 전송 (프레임에 포함된 trackIndex 사용) - 이벤트 기반으로 변경
func (s *Stream) SendFrame(frame Frame) error {
	select {
	case s.channel <- sendFrameEvent{frame: frame}:
		return nil
	default:
		// 채널이 가득 차면 드랍 (실시간 스트리밍 특성)
		slog.Warn("Stream channel full, dropping frame", "streamID", s.id, "trackIndex", frame.TrackIndex)
		return errors.New("stream channel full")
	}
}

// SendMetadata 스트림 메타데이터 전송 (모든 트랙 공통) - 이벤트 기반으로 변경
func (s *Stream) SendMetadata(metadata map[string]string) {
	select {
	case s.channel <- sendMetadataEvent{metadata: metadata}:
		// 이벤트 전송 성공
	default:
		slog.Warn("Stream channel full, dropping metadata", "streamID", s.id)
	}
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

// GetTrackCodecs 모든 트랙의 코덱 목록 반환
func (s *Stream) GetTrackCodecs() []Codec {
	codecs := make([]Codec, len(s.tracks))
	for i, track := range s.tracks {
		codecs[i] = track.Codec
	}
	return codecs
}

// Stop stops the stream and cleans up resources
func (s *Stream) Stop() {
	select {
	case s.channel <- stopStreamEvent{}:
		// 이벤트 전송 성공
	default:
		slog.Warn("Failed to send stop event, stopping directly", "streamID", s.id)
		s.cancel() // 직접 종료
	}
}

// eventLoop Stream 이벤트 루프 (MediaServer 패턴과 동일)
func (s *Stream) eventLoop() {
	defer s.shutdown()
	for {
		select {
		case data := <-s.channel:
			s.handleChannel(data)
		case <-s.ctx.Done():
			return
		}
	}
}

// handleChannel 이벤트 타입별 처리 (MediaServer 패턴과 동일)
func (s *Stream) handleChannel(data any) {
	switch v := data.(type) {
	case sendFrameEvent:
		s.handleSendFrame(v)
	case sendMetadataEvent:
		s.handleSendMetadata(v)
	case addSinkEvent:
		s.handleAddSink(v)
	case removeSinkEvent:
		s.handleRemoveSink(v)
	case stopStreamEvent:
		s.handleStop()
	default:
		slog.Warn("Unknown stream event type", "eventType", utils.TypeName(v), "streamID", s.id)
	}
}

// shutdown 정리 작업 수행
func (s *Stream) shutdown() {
	slog.Info("Stream shutdown starting", "streamID", s.id)

	// Clear all track buffers
	for _, track := range s.tracks {
		track.Buffer.Clear()
	}

	slog.Info("Stream shutdown completed", "streamID", s.id)
}

// AddSink adds a stream sink and automatically sends cached data - 이벤트 기반으로 변경
func (s *Stream) AddSink(sink MediaSink) {
	select {
	case s.channel <- addSinkEvent{sink: sink}:
		// 이벤트 전송 성공
	default:
		slog.Warn("Stream channel full, cannot add sink", "streamID", s.id, "nodeId", sink.ID())
	}
}

// RemoveSink removes a stream sink - 이벤트 기반으로 변경
func (s *Stream) RemoveSink(sink MediaSink) {
	select {
	case s.channel <- removeSinkEvent{sink: sink}:
		// 이벤트 전송 성공
	default:
		slog.Warn("Stream channel full, cannot remove sink", "streamID", s.id, "nodeId", sink.ID())
	}
}

// sendCachedDataToSink sends all cached data to a new sink (private)
func (s *Stream) sendCachedDataToSink(sink MediaSink) error {
	nodeId := sink.ID()

	// Send cached metadata first (from first track buffer if exists)
	if len(s.tracks) > 0 {
		if metadata := s.tracks[0].Buffer.GetMetadata(); metadata != nil {
			if err := sink.SendMetadata(s.id, metadata); err != nil {
				slog.Error("Failed to send cached metadata to sink", "streamID", s.id, "nodeId", nodeId, "err", err)
			} else {
				slog.Debug("Sent cached metadata to sink", "streamID", s.id, "nodeId", nodeId)
			}
		}
	}

	// Send cached frames from all tracks
	for trackIndex, track := range s.tracks {
		cachedFrames := track.Buffer.GetCachedFrames()
		if len(cachedFrames) > 0 {
			slog.Debug("Sending cached frames to new sink", "streamID", s.id, "trackIndex", trackIndex, "nodeId", nodeId, "frameCount", len(cachedFrames))

			for _, frame := range cachedFrames {
				// 각 sink의 선호 포맷에 맞게 변환
				convertedFrame := s.convertFrameForSink(frame, sink)

				if err := sink.SendFrame(s.id, convertedFrame); err != nil {
					slog.Error("Failed to send cached track frame to sink", "streamID", s.id, "trackIndex", frame.TrackIndex, "nodeId", nodeId, "codec", frame.Codec, "err", err)
					// Continue with next frame even if one fails
				}
			}

			slog.Debug("Finished sending cached frames to sink", "streamID", s.id, "trackIndex", trackIndex, "nodeId", nodeId)
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
	for _, track := range s.tracks {
		if track.Buffer.HasCachedData() {
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
	for i, track := range s.tracks {
		trackStats := track.Buffer.GetCacheStats()
		stats[fmt.Sprintf("track_%d", i)] = trackStats
	}

	return stats
}

// convertFrameForSink 각 sink의 선호 포맷에 맞게 프레임 변환 (현재는 변환 없음)
func (s *Stream) convertFrameForSink(frame Frame, sink MediaSink) Frame {
	// 아직 포맷 변환이 필요한 프로토콜이 없으므로 원본 그대로 반환
	return frame
}

// 이벤트 핸들러 메서드들

// handleSendFrame 프레임 전송 이벤트 처리
func (s *Stream) handleSendFrame(event sendFrameEvent) {
	frame := event.frame
	trackIndex := frame.TrackIndex

	if trackIndex < 0 || trackIndex >= len(s.tracks) {
		slog.Error("Invalid track index", "streamID", s.id, "trackIndex", trackIndex, "maxTrackIndex", len(s.tracks)-1)
		return
	}

	// 트랙별 버퍼에 캐시
	s.tracks[trackIndex].Buffer.AddFrame(frame)

	// 모든 sink에 브로드캐스트
	for _, sink := range s.sinks {
		// 각 sink의 선호 포맷에 맞게 변환
		convertedFrame := s.convertFrameForSink(frame, sink)

		if err := sink.SendFrame(s.id, convertedFrame); err != nil {
			slog.Error("Failed to send track frame to sink", "streamID", s.id, "trackIndex", frame.TrackIndex, "nodeId", sink.ID(), "codec", frame.Codec, "err", err)
		}
	}
}

// handleSendMetadata 메타데이터 전송 이벤트 처리
func (s *Stream) handleSendMetadata(event sendMetadataEvent) {
	metadata := event.metadata

	// 첫 번째 트랙 버퍼에 메타데이터 저장 (임시)
	if len(s.tracks) > 0 {
		s.tracks[0].Buffer.AddMetadata(metadata)
	}

	// 모든 sink에 브로드캐스트
	for _, sink := range s.sinks {
		if err := sink.SendMetadata(s.id, metadata); err != nil {
			slog.Error("Failed to send metadata to sink", "streamID", s.id, "nodeId", sink.ID(), "err", err)
		}
	}
}

// handleAddSink Sink 추가 이벤트 처리
func (s *Stream) handleAddSink(event addSinkEvent) {
	sink := event.sink
	nodeId := sink.ID()
	s.sinks[nodeId] = sink

	slog.Info("Sink added to stream", "streamID", s.id, "nodeId", nodeId, "sinkCount", len(s.sinks))

	// 자동으로 캐시된 데이터 전송
	if err := s.sendCachedDataToSink(sink); err != nil {
		slog.Error("Failed to send cached data to newly added sink", "streamID", s.id, "nodeId", nodeId, "err", err)
		// 에러가 있어도 sink는 이미 추가되었으므로 계속 진행
	}
}

// handleRemoveSink Sink 제거 이벤트 처리
func (s *Stream) handleRemoveSink(event removeSinkEvent) {
	sink := event.sink
	nodeId := sink.ID()
	delete(s.sinks, nodeId)

	slog.Info("Sink removed from stream", "streamID", s.id, "nodeId", nodeId, "sinkCount", len(s.sinks))
}

// handleStop 스트림 정지 이벤트 처리
func (s *Stream) handleStop() {
	slog.Info("Stopping stream via event", "streamID", s.id)
	s.cancel() // 컨텍스트 종료
}
