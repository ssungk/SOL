package media

import (
	"context"
	"unsafe"
)

// BaseMediaSource provides a basic implementation of the MediaSource interface
type BaseMediaSource struct {
	mediaType MediaType
	address   string
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewBaseMediaSource creates a new base media source
func NewBaseMediaSource(mediaType MediaType, address string) *BaseMediaSource {
	ctx, cancel := context.WithCancel(context.Background())

	return &BaseMediaSource{
		mediaType: mediaType,
		address:   address,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// ID returns the source ID as pointer address
func (s *BaseMediaSource) ID() uintptr {
	return uintptr(unsafe.Pointer(s))
}

// MediaType returns the media type
func (s *BaseMediaSource) MediaType() MediaType {
	return s.mediaType
}

// Address returns the address
func (s *BaseMediaSource) Address() string {
	return s.address
}

// Start starts the source
func (s *BaseMediaSource) Start() error {
	// Implementation-specific logic can be added here
	return nil
}

// Stop stops the source
func (s *BaseMediaSource) Stop() error {
	s.cancel()
	return nil
}

// Context returns the source context (available for concrete implementations)
func (s *BaseMediaSource) Context() context.Context {
	return s.ctx
}

