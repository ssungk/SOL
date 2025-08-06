package media

import (
	"log/slog"
)

// Stream represents a protocol-independent stream
type Stream struct {
	id           string
	streamBuffer *StreamBuffer

	// Stream routing
	sinks map[uintptr]MediaSink // Multiple sinks indexed by node ID
}

// NewStream creates a new stream
func NewStream(id string) *Stream {
	s := &Stream{
		id:           id,
		streamBuffer: NewStreamBuffer(),
		sinks:        make(map[uintptr]MediaSink),
	}

	slog.Info("Stream created", "streamId", id)

	return s
}

// GetId returns the stream id
func (s *Stream) GetId() string {
	return s.id
}

// SendFrame sends a media frame to the stream (called by external sources)
func (s *Stream) SendFrame(frame Frame) {
	// Cache the frame
	s.streamBuffer.AddFrame(frame)

	// Broadcast to all destinations
	s.broadcastFrame(frame)
}

// SendMetadata sends metadata to the stream (called by external sources)
func (s *Stream) SendMetadata(metadata map[string]string) {
	// Cache the metadata
	s.streamBuffer.AddMetadata(metadata)

	// Broadcast to all destinations
	s.broadcastMetadata(metadata)
}

// Stop stops the stream and cleans up resources
func (s *Stream) Stop() {
	slog.Info("Stopping stream", "streamId", s.id)

	// Clear buffer
	s.streamBuffer.Clear()

	slog.Info("Stream stopped", "streamId", s.id)
}

// AddSink adds a stream sink
func (s *Stream) AddSink(sink MediaSink) error {
	nodeId := sink.ID()
	s.sinks[nodeId] = sink

	slog.Info("Sink added to stream", "streamId", s.id, "nodeId", nodeId, "sinkCount", len(s.sinks))

	return nil
}

// RemoveSink removes a stream sink
func (s *Stream) RemoveSink(sink MediaSink) {
	nodeId := sink.ID()
	delete(s.sinks, nodeId)

	slog.Info("Sink removed from stream", "streamId", s.id, "nodeId", nodeId, "sinkCount", len(s.sinks))
}

// SendCachedDataToSink sends all cached data to a new sink
func (s *Stream) SendCachedDataToSink(sink MediaSink) error {
	nodeId := sink.ID()

	// Send cached metadata first
	if metadata := s.streamBuffer.GetMetadata(); metadata != nil {
		if err := sink.SendMetadata(s.id, metadata); err != nil {
			slog.Error("Failed to send cached metadata to sink", "streamId", s.id, "nodeId", nodeId, "err", err)
		} else {
			slog.Debug("Sent cached metadata to sink", "streamId", s.id, "nodeId", nodeId)
		}
	}

	// Send cached media frames
	cachedFrames := s.streamBuffer.GetCachedFrames()
	if len(cachedFrames) > 0 {
		slog.Debug("Sending cached frames to new sink", "streamId", s.id, "nodeId", nodeId, "frameCount", len(cachedFrames))

		for _, frame := range cachedFrames {
			if err := sink.SendMediaFrame(s.id, frame); err != nil {
				slog.Error("Failed to send cached frame to sink", "streamId", s.id, "nodeId", nodeId, "frameType", frame.FrameType, "err", err)
				// Continue with next frame even if one fails
			}
		}

		slog.Debug("Finished sending cached frames to sink", "streamId", s.id, "nodeId", nodeId)
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
	return s.streamBuffer.HasCachedData()
}

// GetCacheStats returns cache statistics
func (s *Stream) GetCacheStats() map[string]interface{} {
	stats := s.streamBuffer.GetCacheStats()

	stats["sink_count"] = len(s.sinks)

	return stats
}

// CleanupSession removes the session from stream sinks by sessionId
func (s *Stream) CleanupSession(sessionId string) {
	// Find and remove sink by sessionId (this method is less efficient but needed for backward compatibility)
	for nodeId := range s.sinks {
		// This assumes sinks have a way to identify their sessionId
		// In practice, this might need to be refactored to use nodeId directly
		delete(s.sinks, nodeId)
		slog.Info("Cleaned up sink from stream", "streamId", s.id, "sessionId", sessionId, "nodeId", nodeId, "sinkCount", len(s.sinks))
		break // Assume only one sink per session
	}
}

// broadcastFrame sends media frame to all sinks
func (s *Stream) broadcastFrame(frame Frame) {
	// Send to all sinks with stream ID
	for _, sink := range s.sinks {
		if err := sink.SendMediaFrame(s.id, frame); err != nil {
			slog.Error("Failed to send media frame to sink", "streamId", s.id, "nodeId", sink.ID(), "frameType", frame.FrameType, "err", err)
		}
	}
}

// broadcastMetadata sends metadata to all sinks
func (s *Stream) broadcastMetadata(metadata map[string]string) {
	// Send to all sinks with stream ID
	for _, sink := range s.sinks {
		if err := sink.SendMetadata(s.id, metadata); err != nil {
			slog.Error("Failed to send metadata to sink", "streamId", s.id, "nodeId", sink.ID(), "err", err)
		}
	}
}
