package media

import "errors"

// Common errors
var (
	ErrInvalidMediaType = errors.New("invalid media type")
	ErrEmptyFrameData   = errors.New("media frame data is empty")
	ErrEmptyFrameType   = errors.New("media frame type is empty")
)

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

// ValidateFrame validates a media frame
func ValidateFrame(frame Frame) error {
	if frame.Type != TypeAudio && frame.Type != TypeVideo {
		return ErrInvalidMediaType
	}
	
	if len(frame.Data) == 0 {
		return ErrEmptyFrameData
	}
	
	if frame.FrameType == "" {
		return ErrEmptyFrameType
	}
	
	return nil
}