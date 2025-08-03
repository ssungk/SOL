package stream

import (
	"context"
	"fmt"
	"log/slog"
)

// Stream represents a protocol-independent stream with event-driven architecture
type Stream struct {
	name         string
	streamBuffer *StreamBuffer
	
	// Stream routing
	src  StreamSrc                 // Single source (1:N)
	dsts map[StreamDst]struct{} // Multiple destinations
	
	// Event-driven channel
	channel chan interface{}
	cancel  context.CancelFunc
}

// NewStream creates a new stream with a source and starts its event loop
func NewStream(name string, src StreamSrc) *Stream {
	ctx, cancel := context.WithCancel(context.Background())
	
	s := &Stream{
		name:         name,
		streamBuffer: NewStreamBuffer(),
		src:          src,
		dsts:         make(map[StreamDst]struct{}),
		channel:      make(chan interface{}, 100), // Buffered channel for events
		cancel:       cancel,
	}
	
	// Setup external channel for the source
	src.SetExternalChannel(s.channel)
	
	// Start the event loop
	go s.eventLoop(ctx)
	
	slog.Info("Stream created with event loop", 
		"streamName", name,
		"sessionId", src.GetSessionId(),
		"protocol", src.GetProtocol(),
		"srcInfo", src.GetSrcInfo())
	
	return s
}

// GetName returns the stream name
func (s *Stream) GetName() string {
	return s.name
}

// eventLoop runs the main event loop for the stream
// This method should only be called from NewStream as a goroutine
func (s *Stream) eventLoop(ctx context.Context) {
	slog.Debug("Stream event loop started", "streamName", s.name)
	
	for {
		select {
		case event := <-s.channel:
			s.handleEvent(event)
		case <-ctx.Done():
			slog.Info("Stream event loop stopped", "streamName", s.name)
			return
		}
	}
}

// handleEvent processes different types of events
func (s *Stream) handleEvent(event interface{}) {
	switch e := event.(type) {
	case MediaDataEvent:
		s.processMediaFrame(e.Frame)
	case MetadataEvent:
		s.processMetadata(e.Metadata)
	case SourceDisconnectedEvent:
		s.handleSourceDisconnected(e)
	case DestinationDisconnectedEvent:
		s.handleDestinationDisconnected(e)
	case DestinationErrorEvent:
		s.handleDestinationError(e)
	// Handle legacy types for backwards compatibility
	case MediaFrame:
		s.processMediaFrame(e)
	case MetadataFrame:
		s.processMetadata(e)
	default:
		slog.Warn("Unknown event type received", 
			"streamName", s.name, 
			"eventType", fmt.Sprintf("%T", event))
	}
}

// Stop stops the stream event loop and cleans up resources
// This will also close the source, which should close the channel
func (s *Stream) Stop() {
	slog.Info("Stopping stream", "streamName", s.name)
	
	// Close the source first (this should close the channel)
	if err := s.src.Close(); err != nil {
		slog.Error("Failed to close stream source", 
			"streamName", s.name, 
			"sessionId", s.src.GetSessionId(), 
			"err", err)
	}
	
	// Cancel the context to stop event loop
	s.cancel()
	
	// Clear buffer
	s.streamBuffer.Clear()
	
	slog.Info("Stream stopped", "streamName", s.name)
}

// handleSourceDisconnected processes source disconnection events
func (s *Stream) handleSourceDisconnected(event SourceDisconnectedEvent) {
	slog.Info("Source disconnected", 
		"streamName", s.name,
		"sessionId", event.SessionId,
		"reason", event.Reason)
	
	// Source disconnected means stream should be destroyed
	// Note: In a real implementation, this would signal the stream manager
	// to destroy this stream. For now, we just stop the stream.
	s.Stop()
}

// handleDestinationDisconnected processes destination disconnection events
func (s *Stream) handleDestinationDisconnected(event DestinationDisconnectedEvent) {
	slog.Info("Destination disconnected", 
		"streamName", s.name,
		"sessionId", event.SessionId,
		"reason", event.Reason)
	
	// Find and remove the destination
	s.removeDstBySessionId(event.SessionId)
}

// handleDestinationError processes destination error events
func (s *Stream) handleDestinationError(event DestinationErrorEvent) {
	slog.Warn("Destination error occurred", 
		"streamName", s.name,
		"sessionId", event.SessionId,
		"error", event.Error)
	
	// For now, treat errors as disconnections
	// In a more sophisticated implementation, you might implement retry logic
	s.removeDstBySessionId(event.SessionId)
}

// removeDstBySessionId removes a destination by session ID
func (s *Stream) removeDstBySessionId(sessionId string) {
	for dst := range s.dsts {
		if dst.GetSessionId() == sessionId {
			s.RemoveDst(dst)
			return
		}
	}
	slog.Debug("Destination not found for removal", 
		"streamName", s.name, 
		"sessionId", sessionId)
}

// AddDst adds a stream destination
func (s *Stream) AddDst(dst StreamDst) error {
	s.dsts[dst] = struct{}{}
	
	// Set external channel for the destination
	dst.SetExternalChannel(s.channel)
	
	slog.Info("Destination added to stream", 
		"streamName", s.name, 
		"sessionId", dst.GetSessionId(), 
		"dstInfo", dst.GetDstInfo(),
		"dstCount", len(s.dsts))

	return nil
}

// RemoveDst removes a stream destination
func (s *Stream) RemoveDst(dst StreamDst) {
	delete(s.dsts, dst)
	
	// Clear external channel for the destination
	dst.SetExternalChannel(nil)
	
	slog.Info("Destination removed from stream", 
		"streamName", s.name, 
		"sessionId", dst.GetSessionId(), 
		"dstCount", len(s.dsts))
}

// processMediaFrame processes incoming media frame from publisher (internal use)
func (s *Stream) processMediaFrame(frame MediaFrame) {
	// Cache the frame
	switch frame.Type {
	case MediaTypeVideo:
		videoFrame := VideoFrame{
			MediaFrame:       frame,
			IsKeyFrame:       isKeyFrame(frame.FrameType),
			IsSequenceHeader: isVideoSequenceHeader(frame.FrameType),
		}
		s.streamBuffer.AddVideoFrame(videoFrame)
	case MediaTypeAudio:
		audioFrame := AudioFrame{
			MediaFrame:       frame,
			IsSequenceHeader: isAudioSequenceHeader(frame.FrameType),
		}
		s.streamBuffer.AddAudioFrame(audioFrame)
	}

	// Broadcast to all destinations
	s.broadcastMediaFrame(frame)
}

// processMetadata processes incoming metadata from publisher (internal use)
func (s *Stream) processMetadata(metadata MetadataFrame) {
	// Cache the metadata
	s.streamBuffer.AddMetadata(metadata)

	// Broadcast to all destinations
	s.broadcastMetadata(metadata)
}

// SendCachedDataToDst sends all cached data to a new destination
func (s *Stream) SendCachedDataToDst(dst StreamDst) error {
	// Send cached metadata first
	if metadata := s.streamBuffer.GetMetadata(); metadata != nil {
		if err := dst.SendMetadata(*metadata); err != nil {
			slog.Error("Failed to send cached metadata to destination", 
				"streamName", s.name, 
				"sessionId", dst.GetSessionId(), 
				"err", err)
		} else {
			slog.Debug("Sent cached metadata to destination", 
				"streamName", s.name, 
				"sessionId", dst.GetSessionId())
		}
	}

	// Send cached media frames
	cachedFrames := s.streamBuffer.GetCachedFrames()
	if len(cachedFrames) > 0 {
		slog.Debug("Sending cached frames to new destination", 
			"streamName", s.name, 
			"sessionId", dst.GetSessionId(), 
			"frameCount", len(cachedFrames))

		for _, frame := range cachedFrames {
			if err := dst.SendMediaFrame(frame); err != nil {
				slog.Error("Failed to send cached frame to destination", 
					"streamName", s.name, 
					"sessionId", dst.GetSessionId(), 
					"frameType", frame.FrameType,
					"err", err)
				// Continue with next frame even if one fails
			}
		}

		slog.Debug("Finished sending cached frames to destination", 
			"streamName", s.name, 
			"sessionId", dst.GetSessionId())
	}

	return nil
}

// GetDstCount returns the number of active destinations
func (s *Stream) GetDstCount() int {
	return len(s.dsts)
}

// GetDsts returns a copy of all active destinations
func (s *Stream) GetDsts() []StreamDst {
	dsts := make([]StreamDst, 0, len(s.dsts))
	for dst := range s.dsts {
		dsts = append(dsts, dst)
	}
	
	return dsts
}

// HasCachedData returns whether the stream has any cached data
func (s *Stream) HasCachedData() bool {
	return s.streamBuffer.HasCachedData()
}

// GetCacheStats returns cache statistics
func (s *Stream) GetCacheStats() map[string]interface{} {
	stats := s.streamBuffer.GetCacheStats()
	
	stats["dst_count"] = len(s.dsts)
	stats["has_src"] = true
	
	return stats
}

// CleanupSession removes the session from stream destinations
// Note: If the source session is being cleaned up, the entire stream should be destroyed
func (s *Stream) CleanupSession(session Session) {
	// Remove from destinations if exists
	for dst := range s.dsts {
		if dst.GetSessionId() == session.GetSessionId() {
			delete(s.dsts, dst)
			slog.Info("Cleaned up destination from stream", 
				"streamName", s.name, 
				"sessionId", session.GetSessionId(), 
				"dstCount", len(s.dsts))
			break
		}
	}

	// Check if this is the source session - if so, the stream should be destroyed by the caller
	if s.src != nil && s.src.GetSessionId() == session.GetSessionId() {
		slog.Warn("Source session being cleaned up - stream should be destroyed", 
			"streamName", s.name, 
			"sessionId", session.GetSessionId())
	}
}

// broadcastMediaFrame sends media frame to all active destinations
func (s *Stream) broadcastMediaFrame(frame MediaFrame) {
	dsts := make([]StreamDst, 0, len(s.dsts))
	for dst := range s.dsts {
		if dst.IsActive() {
			dsts = append(dsts, dst)
		}
	}

	// Send to all active destinations
	for _, dst := range dsts {
		if err := dst.SendMediaFrame(frame); err != nil {
			slog.Error("Failed to send media frame to destination", 
				"streamName", s.name, 
				"srcSessionId", s.src.GetSessionId(),
				"dstSessionId", dst.GetSessionId(), 
				"frameType", frame.FrameType,
				"err", err)
			// Continue with other destinations even if one fails
		}
	}
}

// broadcastMetadata sends metadata to all active destinations
func (s *Stream) broadcastMetadata(metadata MetadataFrame) {
	dsts := make([]StreamDst, 0, len(s.dsts))
	for dst := range s.dsts {
		if dst.IsActive() {
			dsts = append(dsts, dst)
		}
	}

	// Send to all active destinations
	for _, dst := range dsts {
		if err := dst.SendMetadata(metadata); err != nil {
			slog.Error("Failed to send metadata to destination", 
				"streamName", s.name, 
				"srcSessionId", s.src.GetSessionId(),
				"dstSessionId", dst.GetSessionId(), 
				"err", err)
			// Continue with other destinations even if one fails
		}
	}
}

// Helper functions to determine frame types

// isKeyFrame determines if a frame type represents a key frame
func isKeyFrame(frameType string) bool {
	return frameType == "key frame"
}

// isVideoSequenceHeader determines if a frame type represents a video sequence header
func isVideoSequenceHeader(frameType string) bool {
	return frameType == "AVC sequence header" || frameType == "HEVC sequence header"
}

// isAudioSequenceHeader determines if a frame type represents an audio sequence header
func isAudioSequenceHeader(frameType string) bool {
	return frameType == "AAC sequence header"
}
