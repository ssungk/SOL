package stream

import "errors"

// ConcatChunks combines multiple byte slices into a single byte slice
// This is useful for compatibility with existing code that expects []byte
func ConcatChunks(chunks [][]byte) []byte {
	if len(chunks) == 0 {
		return nil
	}
	
	if len(chunks) == 1 {
		return chunks[0]
	}
	
	totalLen := 0
	for _, chunk := range chunks {
		totalLen += len(chunk)
	}
	
	result := make([]byte, 0, totalLen)
	for _, chunk := range chunks {
		result = append(result, chunk...)
	}
	return result
}

// CopyChunks creates a deep copy of byte slice chunks
// This is useful when you need to store data beyond the original lifetime
func CopyChunks(chunks [][]byte) [][]byte {
	if len(chunks) == 0 {
		return nil
	}
	
	result := make([][]byte, len(chunks))
	for i, chunk := range chunks {
		if len(chunk) > 0 {
			copied := make([]byte, len(chunk))
			copy(copied, chunk)
			result[i] = copied
		}
	}
	return result
}

// GetTotalChunkSize calculates the total size of all chunks
func GetTotalChunkSize(chunks [][]byte) int {
	total := 0
	for _, chunk := range chunks {
		total += len(chunk)
	}
	return total
}

// ValidateMediaFrame validates a media frame
func ValidateMediaFrame(frame MediaFrame) error {
	if frame.Type != MediaTypeAudio && frame.Type != MediaTypeVideo {
		return ErrInvalidMediaType
	}
	
	if len(frame.Data) == 0 {
		return errors.New("media frame data is empty")
	}
	
	if frame.FrameType == "" {
		return errors.New("media frame type is empty")
	}
	
	return nil
}

// CreateVideoFrame creates a video frame with proper type detection
func CreateVideoFrame(frameType string, timestamp uint32, data [][]byte) VideoFrame {
	return VideoFrame{
		MediaFrame: MediaFrame{
			Type:      MediaTypeVideo,
			FrameType: frameType,
			Timestamp: timestamp,
			Data:      data,
		},
		IsKeyFrame:       isKeyFrame(frameType),
		IsSequenceHeader: isVideoSequenceHeader(frameType),
	}
}

// CreateAudioFrame creates an audio frame with proper type detection
func CreateAudioFrame(frameType string, timestamp uint32, data [][]byte) AudioFrame {
	return AudioFrame{
		MediaFrame: MediaFrame{
			Type:      MediaTypeAudio,
			FrameType: frameType,
			Timestamp: timestamp,
			Data:      data,
		},
		IsSequenceHeader: isAudioSequenceHeader(frameType),
	}
}

// CreateMetadataFrame creates a metadata frame
func CreateMetadataFrame(data map[string]any) MetadataFrame {
	// Make a copy to avoid reference issues
	copied := make(map[string]any)
	for k, v := range data {
		copied[k] = v
	}
	
	return MetadataFrame{
		Data: copied,
	}
}
