package media

// MetadataFrame represents metadata information
type MetadataFrame struct {
	Timestamp uint32         // Timestamp when metadata was created
	Data      map[string]any // Metadata key-value pairs
}

// NewMetadataFrame creates a new metadata frame
func NewMetadataFrame(data map[string]any) MetadataFrame {
	// Make a copy to avoid reference issues
	copied := make(map[string]any)
	for k, v := range data {
		copied[k] = v
	}
	
	return MetadataFrame{
		Timestamp: 0, // Default timestamp
		Data:      copied,
	}
}

// NewMetadataFrameWithTimestamp creates a new metadata frame with timestamp
func NewMetadataFrameWithTimestamp(timestamp uint32, data map[string]any) MetadataFrame {
	// Make a copy to avoid reference issues
	copied := make(map[string]any)
	for k, v := range data {
		copied[k] = v
	}
	
	return MetadataFrame{
		Timestamp: timestamp,
		Data:      copied,
	}
}