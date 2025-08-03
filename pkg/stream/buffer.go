package stream

import (
	"log/slog"
)

// CacheEntry represents a cached frame entry
type CacheEntry struct {
	Frame     MediaFrame
	Timestamp uint32
}

// VideoCache manages video frame caching with key frame support
// Event-driven model: assumes single-threaded event loop access
type VideoCache struct {
	sequenceHeader *VideoFrame    // Video sequence header (e.g., AVC sequence header)
	keyFrames      []VideoFrame   // Key frames and following frames
}

// AudioCache manages audio frame caching
// Event-driven model: assumes single-threaded event loop access
type AudioCache struct {
	sequenceHeader *AudioFrame    // Audio sequence header (e.g., AAC sequence header)
	recentFrames   []AudioFrame   // Recent audio frames
	maxFrames      int            // Maximum number of frames to cache
}

// MetadataCache manages metadata caching
// Event-driven model: assumes single-threaded event loop access
type MetadataCache struct {
	lastMetadata *MetadataFrame
}

// StreamBuffer manages all types of media caching
// Event-driven model: all access should be through event loop to avoid race conditions
type StreamBuffer struct {
	video    VideoCache
	audio    AudioCache
	metadata MetadataCache
}

// NewStreamBuffer creates a new stream buffer
func NewStreamBuffer() *StreamBuffer {
	return &StreamBuffer{
		video: VideoCache{
			keyFrames: make([]VideoFrame, 0),
		},
		audio: AudioCache{
			recentFrames: make([]AudioFrame, 0),
			maxFrames:    10, // Fixed size for now
		},
	}
}

// AddVideoFrame adds a video frame to the cache
// Event-driven: should be called from event loop
func (sb *StreamBuffer) AddVideoFrame(frame VideoFrame) {
	// Handle sequence header separately
	if frame.IsSequenceHeader {
		// Create a copy for cache storage to avoid reference issues
		cached := VideoFrame{
			MediaFrame: MediaFrame{
				Type:      frame.Type,
				FrameType: frame.FrameType,
				Timestamp: frame.Timestamp,
				Data:      frame.Data, // Direct reference for zero-copy
			},
			IsKeyFrame:       frame.IsKeyFrame,
			IsSequenceHeader: frame.IsSequenceHeader,
		}
		sb.video.sequenceHeader = &cached
		slog.Debug("Video sequence header cached", "frameType", frame.FrameType, "timestamp", frame.Timestamp)
		return
	}

	// Handle key frames
	if frame.IsKeyFrame {
		// Start new sequence - clear existing frames
		sb.video.keyFrames = make([]VideoFrame, 0)
		slog.Debug("New key frame sequence started", "timestamp", frame.Timestamp)
	}

	// Add frame to sequence (both key frames and inter frames)
	if frame.IsKeyFrame || len(sb.video.keyFrames) > 0 {
		cached := VideoFrame{
			MediaFrame: MediaFrame{
				Type:      frame.Type,
				FrameType: frame.FrameType,
				Timestamp: frame.Timestamp,
				Data:      frame.Data, // Direct reference for zero-copy
			},
			IsKeyFrame:       frame.IsKeyFrame,
			IsSequenceHeader: frame.IsSequenceHeader,
		}
		sb.video.keyFrames = append(sb.video.keyFrames, cached)

		// Limit cache size to fixed value for now
		if len(sb.video.keyFrames) > 50 {
			sb.video.keyFrames = sb.video.keyFrames[len(sb.video.keyFrames)-50:]
		}
	}
}

// AddAudioFrame adds an audio frame to the cache
// Event-driven: should be called from event loop
func (sb *StreamBuffer) AddAudioFrame(frame AudioFrame) {
	// Handle sequence header separately
	if frame.IsSequenceHeader {
		cached := AudioFrame{
			MediaFrame: MediaFrame{
				Type:      frame.Type,
				FrameType: frame.FrameType,
				Timestamp: frame.Timestamp,
				Data:      frame.Data, // Direct reference for zero-copy
			},
			IsSequenceHeader: frame.IsSequenceHeader,
		}
		sb.audio.sequenceHeader = &cached
		slog.Debug("Audio sequence header cached", "frameType", frame.FrameType, "timestamp", frame.Timestamp)
		return
	}

	// Add to recent frames
	cached := AudioFrame{
		MediaFrame: MediaFrame{
			Type:      frame.Type,
			FrameType: frame.FrameType,
			Timestamp: frame.Timestamp,
			Data:      frame.Data, // Direct reference for zero-copy
		},
		IsSequenceHeader: frame.IsSequenceHeader,
	}
	sb.audio.recentFrames = append(sb.audio.recentFrames, cached)

	// Limit cache size
	if len(sb.audio.recentFrames) > sb.audio.maxFrames {
		sb.audio.recentFrames = sb.audio.recentFrames[len(sb.audio.recentFrames)-sb.audio.maxFrames:]
	}
}

// AddMetadata adds metadata to the cache
// Event-driven: should be called from event loop
func (sb *StreamBuffer) AddMetadata(metadata MetadataFrame) {
	// Make a copy of metadata to avoid reference issues
	cached := MetadataFrame{
		Data: make(map[string]any),
	}
	for k, v := range metadata.Data {
		cached.Data[k] = v
	}
	
	sb.metadata.lastMetadata = &cached
	slog.Debug("Metadata cached")
}

// GetCachedFrames returns all cached frames in proper order for new player
// Event-driven: should be called from event loop
func (sb *StreamBuffer) GetCachedFrames() []MediaFrame {
	frames := make([]MediaFrame, 0)

	// 1. Video sequence header first
	if sb.video.sequenceHeader != nil {
		frames = append(frames, sb.video.sequenceHeader.MediaFrame)
	}

	// 2. Audio sequence header
	if sb.audio.sequenceHeader != nil {
		frames = append(frames, sb.audio.sequenceHeader.MediaFrame)
	}

	// 3. Key frame sequence
	for _, frame := range sb.video.keyFrames {
		frames = append(frames, frame.MediaFrame)
	}

	// 4. Recent audio frames
	for _, frame := range sb.audio.recentFrames {
		frames = append(frames, frame.MediaFrame)
	}

	return frames
}

// GetMetadata returns cached metadata
// Event-driven: should be called from event loop
func (sb *StreamBuffer) GetMetadata() *MetadataFrame {
	if sb.metadata.lastMetadata == nil {
		return nil
	}

	// Return a copy to avoid reference issues
	cached := &MetadataFrame{
		Data: make(map[string]any),
	}
	for k, v := range sb.metadata.lastMetadata.Data {
		cached.Data[k] = v
	}
	
	return cached
}

// Clear clears all cached data
// Event-driven: should be called from event loop
func (sb *StreamBuffer) Clear() {
	sb.video.sequenceHeader = nil
	sb.video.keyFrames = make([]VideoFrame, 0)
	sb.audio.sequenceHeader = nil
	sb.audio.recentFrames = make([]AudioFrame, 0)
	sb.metadata.lastMetadata = nil
	
	slog.Debug("Stream buffer cleared")
}

// HasCachedData returns true if there's any cached data
// Event-driven: should be called from event loop
func (sb *StreamBuffer) HasCachedData() bool {
	hasVideo := sb.video.sequenceHeader != nil || len(sb.video.keyFrames) > 0
	hasAudio := sb.audio.sequenceHeader != nil || len(sb.audio.recentFrames) > 0
	hasMetadata := sb.metadata.lastMetadata != nil

	return hasVideo || hasAudio || hasMetadata
}

// GetCacheStats returns cache statistics
// Event-driven: should be called from event loop
func (sb *StreamBuffer) GetCacheStats() map[string]interface{} {
	return map[string]interface{}{
		"video_sequence_header": sb.video.sequenceHeader != nil,
		"key_frame_count":       len(sb.video.keyFrames),
		"audio_sequence_header": sb.audio.sequenceHeader != nil,
		"audio_frame_count":     len(sb.audio.recentFrames),
		"has_metadata":          sb.metadata.lastMetadata != nil,
	}
}
