package media

import (
	"context"
	"unsafe"
)

// BaseMediaSink provides a basic implementation of the MediaSink interface
type BaseMediaSink struct {
	mediaType MediaType
	address   string
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewBaseMediaSink creates a new base media sink
func NewBaseMediaSink(mediaType MediaType, address string) *BaseMediaSink {
	ctx, cancel := context.WithCancel(context.Background())

	return &BaseMediaSink{
		mediaType: mediaType,
		address:   address,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// ID returns the sink ID as pointer address
func (s *BaseMediaSink) ID() uintptr {
	return uintptr(unsafe.Pointer(s))
}

// MediaType returns the media type
func (s *BaseMediaSink) MediaType() MediaType {
	return s.mediaType
}

// Address returns the address
func (s *BaseMediaSink) Address() string {
	return s.address
}

// Start starts the sink
func (s *BaseMediaSink) Start() error {
	// Implementation-specific logic can be added here
	return nil
}

// Stop stops the sink
func (s *BaseMediaSink) Stop() error {
	s.cancel()
	return nil
}

// Context returns the sink context (available for concrete implementations)
func (s *BaseMediaSink) Context() context.Context {
	return s.ctx
}

// SendMediaFrame must be implemented by concrete sink implementations
func (s *BaseMediaSink) SendMediaFrame(streamId string, frame Frame) error {
	panic("SendMediaFrame must be implemented by concrete sink")
}

// SendMetadata must be implemented by concrete sink implementations
func (s *BaseMediaSink) SendMetadata(streamId string, metadata map[string]string) error {
	panic("SendMetadata must be implemented by concrete sink")
}
